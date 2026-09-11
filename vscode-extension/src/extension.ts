import * as vscode from 'vscode';
import * as path from 'node:path';
import { randomBytes, createHash } from 'node:crypto';
import { body, html, Reading, diffText, commentContent } from './render';
import { CommentsClient, CommentWhere } from './comments';
import { sourceLink } from './links';
import { mkdtemp, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { CodeBlock, Entry, Location, Part, Section, Walkthrough, codeBlocks, flatten, parseWalkthrough, repositoryKey, resolveSnippet, lineCount } from './model';
import { containedPath } from './paths';
import { Credential, Published, RemoteError, fetchPublished, fetchStamp, publishedUrl } from './remote';

interface RemoteRecord { apiUrl: string; pageUrl: string; stamp?: string; slug?: string }

type OutlineNode = { kind: 'overview' | 'part' | 'section' | 'step'; id: string; title: string; key?: string; parent?: OutlineNode; children: OutlineNode[] };
interface Progress { step?: string; block?: string; done: string[]; view?: 'overview' | 'part' | 'step'; partId?: string }
class Outline implements vscode.TreeDataProvider<OutlineNode> {
  readonly change = new vscode.EventEmitter<OutlineNode | undefined>();
  readonly onDidChangeTreeData = this.change.event;
  nodes: OutlineNode[] = [];
  reading?: Reading;
  set(reading?: Reading) {
    this.reading = reading;
    const make = (part: Part, section?: Section, entry?: Entry, parent?: OutlineNode): OutlineNode => {
      const item = entry?.step ?? section ?? part;
      const node: OutlineNode = { kind: entry ? 'step' : section ? 'section' : 'part', id: item.id, title: item.title, key: entry?.key, parent, children: [] };
      node.children = entry ? [] : section
        ? reading!.entries.filter(e => e.section === section).map(e => make(part, section, e, node))
        : part.sections.map(s => make(part, s, undefined, node));
      return node;
    };
    this.nodes = reading?.doc.parts.map(p => make(p)) ?? [];
    if (reading) this.nodes.unshift({ kind: 'overview', id: '__overview__', title: 'Overview', children: [] });
    this.change.fire(undefined);
  }
  getChildren(node?: OutlineNode) { return node?.children ?? this.nodes; }
  getParent(node: OutlineNode) { return node.parent; }
  getTreeItem(node: OutlineNode) {
    const item = new vscode.TreeItem(node.title, node.kind === 'step' || node.kind === 'overview' ? vscode.TreeItemCollapsibleState.None : vscode.TreeItemCollapsibleState.Expanded);
    item.id = node.id;
    if (node.key) {
      item.command = { command: 'cw.selectStep', title: 'Read step', arguments: [node.key] };
      item.iconPath = new vscode.ThemeIcon(this.reading?.done.has(node.key) ? 'pass-filled' : 'circle-outline');
      if (this.reading?.view === 'step' && this.reading.entries[this.reading.index].key === node.key) item.description = 'Reading';
    } else item.iconPath = new vscode.ThemeIcon(node.kind === 'part' ? 'book' : 'list-unordered');
    if (node.kind === 'overview') item.command = { command: 'cw.overview', title: 'Overview' };
    if (node.kind === 'part') item.command = { command: 'cw.part', title: 'Read part', arguments: [node.id] };
    if (node.kind === 'section') item.command = { command: 'cw.selectStep', title: 'Read section', arguments: [node.children[0]?.key] };
    return item;
  }
}

class Reader implements vscode.WebviewViewProvider, vscode.Disposable {
  reading?: Reading;
  private uri?: vscode.Uri;
  private referenceHistory: { uri: vscode.Uri; address: string; codeIndex: number; focusBlock?: string }[] = [];
  private view?: vscode.WebviewView;
  private token = '';
  private renderedToken = '';
  contentReport?: { diagrams: number; failedDiagrams: number; timelines: number; highlighted: number };
  lastError?: string;
  get contentReady() { return !!this.token && this.renderedToken === this.token; }
  private queue: Promise<unknown> = Promise.resolve();
  private readonly outline = new Outline();
  private readonly subscriptions: vscode.Disposable[] = [];
  private watched?: vscode.FileSystemWatcher;
  private timer?: ReturnType<typeof setTimeout>;
  private pendingReload = false;
  private activeSource?: string;
  private remote?: RemoteRecord;
  private remoteTimer?: ReturnType<typeof setTimeout>;
  private commentsClient?: CommentsClient;
  private commentTimer?: ReturnType<typeof setTimeout>;
  private commentSnapshot?: string;
  private activeReply?: CodeBlock;
  private checkedSources = new Set<string>();
  private readonly editors = new Set<vscode.TextEditor>();
  private readonly focus = vscode.window.createTextEditorDecorationType({ isWholeLine: true, backgroundColor: new vscode.ThemeColor('editor.findMatchHighlightBackground') });
  private readonly added = vscode.window.createTextEditorDecorationType({ isWholeLine: true, backgroundColor: new vscode.ThemeColor('diffEditor.insertedTextBackground') });
  private readonly notes = vscode.window.createTextEditorDecorationType({ isWholeLine: true, borderWidth: '0 0 0 2px', borderStyle: 'solid', borderColor: new vscode.ThemeColor('textLink.foreground') });

  constructor(private readonly context: vscode.ExtensionContext) {
    this.subscriptions.push(this.outline.change, this.focus, this.added, this.notes,
      vscode.window.createTreeView('cw.outline', { treeDataProvider: this.outline, showCollapseAll: true }),
      vscode.window.registerWebviewViewProvider('cw.content', this));
    const commands: Record<string, (...args: any[]) => unknown> = {
      'cw.open': (uri?: vscode.Uri) => this.open(uri),
      'cw.openPublished': (target?: string) => this.openPublished(target),
      'cw.refreshPublished': () => this.refreshPublished(),
      'cw.selectStep': (key: string) => this.select(key),
      'cw.overview': () => this.showPage('overview'), 'cw.part': (id: string) => this.showPage('part', id),
      'cw.next': () => this.move(1), 'cw.previous': () => this.move(-1),
      'cw.nextCode': () => this.moveCode(1), 'cw.previousCode': () => this.moveCode(-1),
      'cw.chooseRoot': () => this.chooseRoot(), 'cw.restart': () => this.restart(),
      'cw.resume': () => this.resume(), 'cw.close': () => this.close(),
      'cw.settings': () => vscode.commands.executeCommand('workbench.action.openSettings', '@ext:sjroesink.cw-walkthrough'),
      'cw.copyLink': () => this.copyLink(), 'cw.toggleTheme': () => this.toggleTheme(),
      'cw.comments': () => this.toggleComments(), 'cw.commentSelection': () => this.commentEditorSelection(),
    };
    for (const [name, action] of Object.entries(commands))
      this.subscriptions.push(vscode.commands.registerCommand(name, (...args) => this.run(() => action(...args))));
    this.subscriptions.push(vscode.workspace.onDidChangeTextDocument(e => {
      if (e.document.uri.toString() !== this.uri?.toString() && e.document.uri.toString() !== this.activeSource && !this.checkedSources.has(e.document.uri.toString())) return;
      // Highlights must not claim a stale position while the user is typing.
      if (e.document.uri.toString() === this.activeSource) this.clearDecorations();
      this.scheduleRefresh(e.document.uri.toString() === this.uri?.toString());
    }));
    this.subscriptions.push(vscode.window.onDidChangeActiveColorTheme(() => this.render()));
    this.subscriptions.push(vscode.workspace.onDidChangeConfiguration(e => { if (e.affectsConfiguration('cw.theme') || e.affectsConfiguration('cw.accent')) this.render(); }));
    this.subscriptions.push(vscode.window.registerUriHandler({ handleUri: uri => this.run(async () => {
      if (uri.path !== '/open') return;
      const query = new URLSearchParams(uri.query), file = query.get('file'), url = query.get('url');
      if (url) await this.openPublished(url);
      else if (file) await this.open(vscode.Uri.file(file));
      else return;
      await this.goToAddress(query.get('page') ?? '');
    }).then(() => undefined) }));
  }

  run(action: () => unknown): Promise<unknown> {
    const pending = this.queue.then(() => { this.lastError = undefined; return action(); });
    this.queue = pending.catch(err => { this.lastError = message(err); void vscode.window.showErrorMessage(`CW: ${this.lastError}`); });
    return this.queue;
  }

  async restore() {
    const last = this.context.workspaceState.get<string>('cw.last');
    if (last) {
      try { await this.load(vscode.Uri.parse(last), false); }
      catch { /* A deleted or unavailable file should not interrupt startup. */ }
    }
  }

  resolveWebviewView(view: vscode.WebviewView) {
    this.view = view;
    view.webview.options = { enableScripts: true, localResourceRoots: [vscode.Uri.joinPath(this.context.extensionUri, 'media'), vscode.Uri.joinPath(this.context.extensionUri, 'dist', 'webview')] };
    const messages = view.webview.onDidReceiveMessage(msg => this.receiveMessage(msg));
    const disposed = view.onDidDispose(() => { messages.dispose(); disposed.dispose(); if (this.view === view) this.view = undefined; });
    this.render();
  }

  private receiveMessage(msg: any) {
      if (!msg || typeof msg.action !== 'string' || msg.token !== this.token) return;
      if (msg.action === 'ready') {
        this.renderedToken = msg.token;
        if (msg.report && ['diagrams', 'failedDiagrams', 'timelines', 'highlighted'].every(k => Number.isInteger(msg.report[k]) && msg.report[k] >= 0)) this.contentReport = msg.report;
        return;
      }
      return this.run(async () => {
        if (msg.token !== this.token) return;
        switch (msg.action) {
          case 'open': await this.open(); break;
          case 'published': await this.openPublished(); break;
          case 'reference': await this.openReference(Number(msg.value)); break;
          case 'referenceBack': await this.referenceBack(); break;
          case 'refreshPublished': await this.refreshPublished(); break;
          case 'settings': await vscode.commands.executeCommand('workbench.action.openSettings', '@ext:sjroesink.cw-walkthrough'); break;
          case 'theme': await this.toggleTheme(); break;
          case 'copyLink': await this.copyLink(); break;
          case 'up': if (this.reading?.view === 'step') await this.showPage('part', this.reading.entries[this.reading.index].part.id); else await this.showPage('overview'); break;
          case 'openCode': await this.navigate(); break;
          case 'comments': await this.toggleComments(); break;
          case 'commentsRetry': if (this.reading) { await this.stopComments(); this.reading.commentsOpen = false; await this.toggleComments(); } break;
          case 'commentAdd': await this.addComment(); break;
          case 'commentSelection': await this.addComment(msg.value); break;
          case 'commentReply': await this.replyComment(msg.value); break;
          case 'commentArchive': await this.archiveComment(msg.value); break;
          case 'commentLocation': {
            const thread = this.reading?.comments?.threads.find(t => t.id === msg.value);
            if (thread && this.reading) { this.reading.commentFocus = thread.id; await this.goToAddress(thread.where.step); }
            break;
          }
          case 'commentPrompt': if (this.reading?.comments?.prompt) {
            const binary = vscode.Uri.joinPath(this.context.extensionUri, 'dist', 'native', process.platform === 'win32' ? 'cw.exe' : 'cw').fsPath;
            await vscode.env.clipboard.writeText(`Use the cw executable at ${binary} for the commands below.\n\n` + this.reading.comments.prompt);
          } break;
          case 'replyCode': {
            const block = this.payloadBlock(Number(msg.value));
            if (block?.type === 'code' && block.snippet.source) { this.activeReply = block; await this.navigate(); }
            break;
          }
          case 'overview': await this.showPage('overview'); break;
          case 'part': if (typeof msg.value === 'string') await this.showPage('part', msg.value); break;
          case 'step': if (typeof msg.value === 'string') await this.select(msg.value); break;
          case 'next': await this.move(1); break;
          case 'previous': await this.move(-1); break;
          case 'nextCode': await this.moveCode(1); break;
          case 'previousCode': await this.moveCode(-1); break;
          case 'root': await this.chooseRoot(); break;
          case 'complete': await this.complete(); break;
          case 'code': {
            const i = Number(msg.value), r = this.reading;
            if (r && Number.isInteger(i) && i >= 0 && i < codeBlocks(r.entries[r.index].step).length) {
              this.activeReply = undefined; r.codeIndex = i; await this.navigate(); await this.save();
            }
            break;
          }
          case 'block': if (typeof msg.value === 'string') await this.block(msg.value); break;
          case 'copy': {
            const b = this.payloadBlock(Number(msg.value));
            const text = b?.type === 'code' ? b.snippet.text : b?.type === 'diagram' ? b.text : b?.type === 'diff' ? diffText(b) : undefined;
            if (text !== undefined) {
              await vscode.env.clipboard.writeText(text);
              await this.view?.webview.postMessage({ type: 'copied', token: this.token, value: msg.value });
            }
            break;
          }
          case 'copySource': {
            const block = this.payloadBlock(Number(msg.value));
            if (this.reading && block?.type === 'code' && block.snippet.source) {
              const link = sourceLink(this.reading.doc, block.snippet.source);
              if (link) { await vscode.env.clipboard.writeText(link); void vscode.window.showInformationMessage('CW: source link copied.'); }
            }
            break;
          }
          case 'line': case 'diffLine': {
            if (typeof msg.value !== 'string' || !/^\d+:\d+$/.test(msg.value) || !this.reading) break;
            const [index, line] = msg.value.split(':').map(Number), r = this.reading;
            const b = this.payloadBlock(index);
            if (msg.action === 'line' && b?.type === 'code' && b.snippet.source && line >= 1 && line <= lineCount(b.snippet.text)) {
              const codeIndex = codeBlocks(r.entries[r.index].step).indexOf(b);
              this.activeReply = codeIndex < 0 ? b : undefined;
              if (codeIndex >= 0) r.codeIndex = codeIndex;
              await this.navigate(true, line); await this.save();
            }
            if (msg.action === 'diffLine' && b?.type === 'diff' && b.after && (line === 1 || b.hunks.some(h => line >= h.newStart && line < h.newStart + h.newLines))) {
              const root = await this.rootFor(b.after);
              if (root) {
                const file = await containedPath(root.fsPath, b.after.file);
                const document = await vscode.workspace.openTextDocument(vscode.Uri.file(file));
                if (line > document.lineCount) { await vscode.window.showWarningMessage('CW: this diff line is no longer present in the local file.'); break; }
                this.clearDecorations();
                await vscode.window.showTextDocument(document, { preserveFocus: true, preview: true, viewColumn: vscode.ViewColumn.One, selection: new vscode.Range(line - 1, 0, line - 1, 0) });
              }
            }
            break;
          }
          case 'link': if (typeof msg.value === 'string' && /^https?:\/\//i.test(msg.value)) await vscode.env.openExternal(vscode.Uri.parse(msg.value)); break;
        }
      });
  }

  private render() {
    if (this.reading) this.reading.canReturn = this.referenceHistory.length > 0;
    this.outline.set(this.reading);
    if (!this.view) return;
    this.token = randomBytes(16).toString('hex');
    this.contentReport = undefined;
    const webview = this.view.webview;
    const configuration = vscode.workspace.getConfiguration('cw');
    const preferences = { theme: configuration.get<string>('theme', 'auto'), accent: configuration.get<string>('accent', 'auto') };
    webview.html = html(body(this.reading), webview.asWebviewUri(vscode.Uri.joinPath(this.context.extensionUri, 'media', 'sidebar.css')).toString(),
      webview.asWebviewUri(vscode.Uri.joinPath(this.context.extensionUri, 'dist', 'webview', 'sidebar.js')).toString(), webview.cspSource, randomBytes(16).toString('hex'), this.token, preferences);
    this.view.description = this.reading ? `${this.reading.index + 1}/${this.reading.entries.length}` : undefined;
  }

  private async open(uri?: vscode.Uri) {
    if (!uri || typeof uri.scheme !== 'string' || typeof uri.fsPath !== 'string') {
      const selected = await vscode.window.showOpenDialog({ canSelectMany: false, filters: { 'Walkthrough JSON': ['json'] }, title: 'Open a cw/2 walkthrough' });
      if (!selected?.[0]) return;
      uri = selected[0];
    }
    await this.load(uri, true);
  }

  private address() {
    const r = this.reading;
    return !r || r.view === 'overview' ? '' : r.view === 'part' ? r.partId ?? '' : r.entries[r.index].key;
  }

  private async openReference(index: number) {
    const b = this.payloadBlock(index);
    if (b?.type !== 'reference' || !this.uri || !this.reading) return;
    const previous = { uri: this.uri, address: this.address(), codeIndex: this.reading.codeIndex, focusBlock: this.reading.focusBlock };
    await this.save();
    let local: vscode.Uri | undefined;
    if (b.target.file && !this.remote) {
      try {
        local = vscode.Uri.file(await containedPath(path.dirname(this.uri.fsPath), b.target.file.replace(/^\.\//, '')));
        parseWalkthrough(Buffer.from(await vscode.workspace.fs.readFile(local)).toString('utf8'));
      } catch (error) { if (!b.target.url) throw error; local = undefined; }
    }
    if (local) await this.load(local, false);
    else if (b.target.url) {
      const opened = await this.openPublished(b.target.url);
      if (!opened) return;
    } else throw new Error('This reference needs its local JSON file. No published URL is provided.');
    this.referenceHistory.push(previous);
    const entry = b.target.stepId ? this.reading!.entries.find(e => e.step.id === b.target.stepId) : undefined;
    await this.goToAddress(entry?.key ?? '');
    if (b.target.stepId && !entry) void vscode.window.showWarningMessage(`CW: step ${b.target.stepId} was not found. Showing the overview.`);
  }

  private async referenceBack() {
    const previous = this.referenceHistory.at(-1);
    if (!previous) return;
    await this.save();
    await this.load(previous.uri, false);
    this.referenceHistory.pop();
    await this.goToAddress(previous.address);
    if (this.reading) { this.reading.codeIndex = previous.codeIndex; this.reading.focusBlock = previous.focusBlock; await this.navigate(false); this.render(); }
  }

  private async goToAddress(address: string) {
    if (!this.reading) return;
    if (address.startsWith('step=')) {
      const id = address.slice(5);
      const entry = this.reading.entries.find(e => e.step.id === id);
      if (!entry) void vscode.window.showWarningMessage(`CW: step ${id} was not found. Showing the overview.`);
      address = entry?.key ?? '';
    }
    if (!address) await this.showPage('overview');
    else if (this.reading.doc.parts.some(p => p.id === address)) await this.showPage('part', address);
    else await this.select(address);
  }

  private async copyLink() {
    if (!this.reading || !this.uri) return;
    const page = this.address();
    const link = this.remote ? this.remote.pageUrl + (page ? '#' + page.split('/').map(encodeURIComponent).join('/') : '')
      : `${vscode.env.uriScheme}://sjroesink.cw-walkthrough/open?` + new URLSearchParams({ file: this.uri.fsPath, page });
    await vscode.env.clipboard.writeText(link);
    void vscode.window.showInformationMessage('CW: page link copied.');
  }

  private async toggleTheme() {
    const config = vscode.workspace.getConfiguration('cw');
    const current = config.get<string>('theme', 'auto');
    const dark = current === 'dark' || (current === 'auto' && [vscode.ColorThemeKind.Dark, vscode.ColorThemeKind.HighContrast].includes(vscode.window.activeColorTheme.kind));
    await config.update('theme', dark ? 'light' : 'dark', vscode.ConfigurationTarget.Global);
  }

  private progressKey() { return `cw.progress:${this.uri!.toString()}`; }
  private rootsKey() { return `cw.roots:${this.uri!.toString()}`; }
  private async load(uri: vscode.Uri, navigate: boolean) {
    const record = this.context.workspaceState.get<Record<string, RemoteRecord>>('cw.remoteFiles', {})[uri.toString()];
    if (uri.scheme !== 'file' && !record) throw new Error('Open a walkthrough from a local folder or a folder on the remote extension host.');
    const text = record ? Buffer.from(await vscode.workspace.fs.readFile(uri)).toString('utf8') : (await vscode.workspace.openTextDocument(uri)).getText();
    const doc = parseWalkthrough(text);
    const previousComments = this.uri?.toString() === uri.toString() ? this.reading : undefined;
    if (!previousComments) await this.stopComments();
    if (this.remoteTimer) clearTimeout(this.remoteTimer);
    this.remote = record;
    if (this.timer) clearTimeout(this.timer);
    this.pendingReload = false;
    this.clearDecorations();
    this.activeSource = undefined;
    this.uri = uri;
    const entries = flatten(doc), saved = this.context.workspaceState.get<Progress>(this.progressKey());
    const index = Math.max(0, entries.findIndex(e => e.key === saved?.step));
    this.reading = {
      doc, entries, index, identity: uri.toString(), codeIndex: Math.max(0, codeBlocks(entries[index].step).findIndex(b => b.id === saved?.block && !!b.id)),
      done: new Set((saved?.done ?? []).filter(key => entries.some(e => e.key === key))),
      view: saved?.view ?? (saved?.step ? 'step' : 'overview'), partId: doc.parts.find(p => p.id === saved?.partId)?.id ?? entries[index].part.id,
      publishedUrl: record?.pageUrl,
      comments: previousComments?.comments, commentsOpen: previousComments?.commentsOpen, commentsError: previousComments?.commentsError,
    };
    if (saved?.block && entries[index].step.blocks.some(b => b.id === saved.block && b.type === 'code' && !b.snippet.source)) {
      this.reading.codeIndex = -1; this.reading.focusBlock = saved.block;
    }
    this.watched?.dispose();
    this.watched = undefined;
    if (!record) {
      this.watched = vscode.workspace.createFileSystemWatcher(new vscode.RelativePattern(vscode.Uri.file(path.dirname(uri.fsPath)), path.basename(uri.fsPath)));
      this.watched.onDidChange(() => this.scheduleRefresh(true));
      this.watched.onDidDelete(() => this.scheduleRefresh(true));
    }
    await vscode.commands.executeCommand('setContext', 'cw.loaded', true);
    await this.context.workspaceState.update('cw.last', uri.toString());
    this.render();
    if (navigate) {
      await vscode.commands.executeCommand('cw.content.focus');
      await this.navigate();
    }
    await this.save();
    if (this.commentSnapshot) await writeFile(path.join(this.commentSnapshot, 'walkthrough.json'), JSON.stringify(doc), 'utf8');
    this.pollPublished();
  }

  private payloadBlock(index: number) {
    const r = this.reading;
    if (!r || !Number.isInteger(index) || index < 0) return undefined;
    return r.entries[r.index].step.blocks[index] ?? commentContent(r).find(b => b.index === index)?.block;
  }

  private async toggleComments() {
    const r = this.reading;
    if (!r || !this.uri) return;
    r.commentsOpen = !r.commentsOpen;
    if (r.commentsOpen && !this.commentsClient) {
      const client = new CommentsClient();
      try {
        let file = this.uri.fsPath;
        if (this.remote) {
          this.commentSnapshot = await mkdtemp(path.join(tmpdir(), 'cw-vscode-'));
          file = path.join(this.commentSnapshot, 'walkthrough.json');
          await writeFile(file, JSON.stringify(r.doc), 'utf8');
        }
        const root = r.root ?? (await this.rootFor({ file: '', repositoryUrl: r.doc.source?.repositoryUrl }, false, false))?.fsPath ?? path.dirname(file);
        await client.connect(vscode.Uri.joinPath(this.context.extensionUri, 'dist', 'native', process.platform === 'win32' ? 'cw.exe' : 'cw').fsPath, file, root,
          this.remote ? this.remote.slug ?? 'remote-' + createHash('sha256').update(this.remote.apiUrl).digest('hex').slice(0, 32) : undefined);
        this.commentsClient = client;
        r.comments = await client.view(); r.commentsError = undefined;
        this.pollComments();
      } catch (error) {
        client.dispose(); r.commentsError = message(error);
      }
    }
    this.render();
  }

  private pollComments() {
    if (this.commentTimer) clearTimeout(this.commentTimer);
    const client = this.commentsClient;
    if (!client) return;
    this.commentTimer = setTimeout(async () => {
      try {
        const view = await client.view();
        await this.run(() => {
          const r = this.reading;
          if (!r || this.commentsClient !== client) return;
          const changed = view.rev !== r.comments?.rev || view.watching !== r.comments?.watching || r.commentsError;
          r.comments = view; r.commentsError = undefined;
          if (changed && r.commentsOpen) this.render();
        });
      } catch {
        await this.run(() => {
          if (this.commentsClient !== client || !this.reading || this.reading.commentsError) return;
          this.reading.commentsError = 'The local comment service is unavailable. Existing comments remain on disk.';
          this.render();
        });
      } finally { if (this.commentsClient === client) this.pollComments(); }
    }, 1500);
  }

  private async stopComments() {
    if (this.commentTimer) clearTimeout(this.commentTimer);
    this.commentsClient?.dispose(); this.commentsClient = undefined; this.activeReply = undefined;
    if (this.commentSnapshot) { const directory = this.commentSnapshot; this.commentSnapshot = undefined; await rm(directory, { recursive: true, force: true }); }
  }

  private async addComment(selection?: unknown) {
    const r = this.reading;
    if (!r || !this.commentsClient) return;
    const entry = r.entries[r.index];
    const where: CommentWhere = { step: this.address(), title: r.view === 'overview' ? r.doc.title : r.view === 'part' ? r.doc.parts.find(p => p.id === r.partId)?.title : entry.step.title };
    if (selection && typeof selection === 'object') {
      const selected = selection as { block?: number; quote?: string; first?: number; last?: number };
      if (typeof selected.quote === 'string') where.quote = selected.quote.slice(0, 8000);
      where.kind = 'text';
      const block = Number.isInteger(selected.block) ? entry.step.blocks[selected.block!] : undefined;
      if (r.view === 'step' && block) {
        where.block = block.id;
        if (block.type === 'code' && block.snippet.source && Number.isInteger(selected.first) && Number.isInteger(selected.last) && selected.first! >= 1 && selected.last! >= selected.first! && selected.last! <= lineCount(block.snippet.text)) {
          where.kind = 'code'; where.file = block.snippet.source.file;
          where.lines = { start: (block.snippet.source.startLine ?? 1) + selected.first! - 1, end: (block.snippet.source.startLine ?? 1) + selected.last! - 1 };
        }
      }
    }
    const text = await vscode.window.showInputBox({ title: 'Ask about ' + (where.title || 'this page'), prompt: where.quote ? where.quote.slice(0, 160) : 'Question for the agent watching this walkthrough', ignoreFocusOut: true });
    if (!text?.trim()) return;
    await this.commentsClient.add(where, text);
    r.comments = await this.commentsClient.view(); this.render();
  }

  private async commentEditorSelection() {
    const r = this.reading, editor = vscode.window.activeTextEditor;
    if (!r || !editor || editor.selection.isEmpty) return;
    if (!this.commentsClient) { r.commentsOpen = false; await this.toggleComments(); }
    if (!this.commentsClient) return;
    const root = await this.rootFor({ file: '', repositoryUrl: r.doc.source?.repositoryUrl }, false, false);
    if (!root) throw new Error('Choose the repository folder before commenting on editor code.');
    const relative = path.relative(root.fsPath, editor.document.uri.fsPath).split(path.sep).join('/');
    await containedPath(root.fsPath, relative);
    const text = await vscode.window.showInputBox({ title: 'Comment on selected code', ignoreFocusOut: true });
    if (!text?.trim()) return;
    const selection = editor.selection;
    await this.commentsClient.add({ step: this.address(), title: r.entries[r.index].step.title, kind: 'code', file: relative,
      lines: { start: selection.start.line + 1, end: selection.end.line + (selection.end.character === 0 ? 0 : 1) }, quote: editor.document.getText(selection).slice(0, 8000) }, text);
    r.commentsOpen = true; r.comments = await this.commentsClient.view(); this.render();
  }

  private async replyComment(id: unknown) {
    if (typeof id !== 'string' || !this.commentsClient || !this.reading?.comments?.threads.some(t => t.id === id)) return;
    const text = await vscode.window.showInputBox({ title: 'Reply to comment', ignoreFocusOut: true });
    if (!text?.trim()) return;
    await this.commentsClient.reply(id, text); this.reading.comments = await this.commentsClient.view(); this.render();
  }

  private async archiveComment(id: unknown) {
    if (typeof id !== 'string' || !this.commentsClient || !this.reading) return;
    const thread = this.reading.comments?.threads.find(t => t.id === id);
    if (!thread) return;
    await this.commentsClient.archive(id, !thread.archived); this.reading.comments = await this.commentsClient.view(); this.render();
  }

  private async credential(apiUrl: string): Promise<Credential | undefined> {
    const raw = await this.context.secrets.get('cw.credential:' + apiUrl);
    if (!raw) return undefined;
    try {
      const value = JSON.parse(raw);
      if (['password', 'key'].includes(value.kind) && typeof value.value === 'string') return value;
    } catch { /* old or invalid secret */ }
    return undefined;
  }

  private async openPublished(target?: string) {
    if (!target) target = await vscode.window.showInputBox({ title: 'Open published walkthrough', prompt: 'Paste its page URL, API URL or name on cw.roesink.dev', ignoreFocusOut: true });
    if (!target) return;
    const url = publishedUrl(target, vscode.workspace.getConfiguration('cw').get('site', 'https://cw.roesink.dev'));
    let credential = await this.credential(url);
    while (true) {
      try {
        const result = await vscode.window.withProgress({ location: vscode.ProgressLocation.Notification, title: 'Loading walkthrough' }, () => fetchPublished(url, credential));
        if (credential) await this.context.secrets.store('cw.credential:' + url, JSON.stringify(credential));
        await this.storePublished(result, true);
        if (target.includes('#')) {
          let fragment = target.slice(target.indexOf('#') + 1);
          try { fragment = decodeURIComponent(fragment); } catch { /* malformed escapes cannot select a valid ID */ }
          await this.goToAddress(fragment);
        }
        return true;
      } catch (error) {
        if (!(error instanceof RemoteError) || error.status !== 401) throw error;
        const kind = await vscode.window.showQuickPick([{ label: 'Password', credentialKind: 'password' as const }, { label: 'Publishing key', credentialKind: 'key' as const }], { title: `Unlock walkthrough on ${new URL(url).host}` });
        if (!kind) return;
        const value = await vscode.window.showInputBox({ title: kind.label, prompt: `For ${new URL(url).host}. Stored securely in VS Code after a successful unlock.`, password: true, ignoreFocusOut: true });
        if (value === undefined) return;
        credential = { kind: kind.credentialKind, value };
      }
    }
  }

  private async storePublished(result: Published, navigate: boolean) {
    const directory = vscode.Uri.joinPath(this.context.globalStorageUri, 'walkthroughs');
    await vscode.workspace.fs.createDirectory(directory);
    const uri = vscode.Uri.joinPath(directory, createHash('sha256').update(result.apiUrl).digest('hex') + '.json');
    const records = this.context.workspaceState.get<Record<string, RemoteRecord>>('cw.remoteFiles', {});
    records[uri.toString()] = { apiUrl: result.apiUrl, pageUrl: result.pageUrl, stamp: result.stamp, slug: result.slug };
    await this.context.workspaceState.update('cw.remoteFiles', records);
    await vscode.workspace.fs.writeFile(uri, Buffer.from(result.text, 'utf8'));
    await this.load(uri, navigate);
  }

  private async refreshPublished() {
    if (!this.remote || !this.uri) return;
    const result = await fetchPublished(this.remote.apiUrl, await this.credential(this.remote.apiUrl));
    await this.save(); await this.storePublished(result, false);
    await this.navigate(false);
  }

  private pollPublished() {
    if (!this.remote || !this.uri) return;
    if (this.remoteTimer) clearTimeout(this.remoteTimer);
    const current = this.remote, uri = this.uri.toString();
    this.remoteTimer = setTimeout(async () => {
      try {
        const credential = await this.credential(current.apiUrl);
        const stamp = await fetchStamp(current.apiUrl, credential);
        const result = stamp && stamp !== current.stamp ? await fetchPublished(current.apiUrl, credential) : undefined;
        await this.run(async () => {
          if (this.uri?.toString() !== uri || this.remote !== current) return;
          if (result) { await this.save(); await this.storePublished(result, false); await this.navigate(false); }
          else if (this.reading?.remoteNotice) { this.reading.remoteNotice = undefined; this.render(); }
        });
      } catch (error) {
        await this.run(() => {
          if (this.uri?.toString() !== uri || this.remote !== current || !this.reading) return;
          const notice = error instanceof RemoteError ? error.message + ' The downloaded copy remains available.' : 'The site is unavailable. Reading the downloaded copy.';
          if (this.reading.remoteNotice !== notice) { this.reading.remoteNotice = notice; this.render(); }
        });
      } finally {
        if (this.uri?.toString() === uri && this.remote === current) this.pollPublished();
      }
    }, 5000);
  }

  private async save() {
    if (!this.reading || !this.uri) return;
    const r = this.reading;
    await this.context.workspaceState.update(this.progressKey(), {
      step: r.entries[r.index].key, block: codeBlocks(r.entries[r.index].step)[r.codeIndex]?.id ?? r.focusBlock, done: [...r.done],
      view: r.view, partId: r.partId,
    } satisfies Progress);
  }

  private async select(key: string) {
    const r = this.reading;
    if (!r) return;
    const index = r.entries.findIndex(e => e.key === key);
    if (index < 0) return;
    this.activeReply = undefined;
    r.index = index; r.codeIndex = 0; r.focusBlock = undefined; r.view = 'step'; r.partId = r.entries[index].part.id;
    await vscode.commands.executeCommand('cw.content.focus');
    await this.navigate(); await this.save();
  }

  private async move(delta: number) {
    this.activeReply = undefined;
    const r = this.reading;
    if (!r) return;
    if (delta > 0 && r.view === 'step') r.done.add(r.entries[r.index].key);
    const pages: { view: 'overview' | 'part' | 'step'; partId?: string; index?: number }[] = [{ view: 'overview' }];
    for (const p of r.doc.parts) {
      pages.push({ view: 'part', partId: p.id });
      r.entries.forEach((e, index) => { if (e.part.id === p.id) pages.push({ view: 'step', partId: p.id, index }); });
    }
    const at = pages.findIndex(p => p.view === r.view && (p.view === 'overview' || p.partId === r.partId) && (p.view !== 'step' || p.index === r.index));
    const next = pages[(Math.max(0, at) + delta + pages.length) % pages.length];
    r.view = next.view; r.partId = next.partId;
    if (next.index !== undefined) r.index = next.index;
    r.codeIndex = 0; r.focusBlock = undefined;
    await this.navigate();
    await this.save();
  }

  private async showPage(view: 'overview' | 'part', partId?: string) {
    this.activeReply = undefined;
    const r = this.reading;
    if (!r || (view === 'part' && !r.doc.parts.some(p => p.id === partId))) return;
    r.view = view; r.partId = partId;
    await vscode.commands.executeCommand('cw.content.focus');
    await this.navigate(); await this.save();
  }

  private async complete() {
    const r = this.reading;
    if (!r || r.view !== 'step') return;
    const key = r.entries[r.index].key;
    if (r.done.has(key)) r.done.delete(key); else r.done.add(key);
    this.render(); await this.save();
  }

  private async moveCode(delta: number) {
    this.activeReply = undefined;
    const r = this.reading;
    if (!r || r.view !== 'step') return;
    const codes = codeBlocks(r.entries[r.index].step);
    if (!codes.length) return;
    r.codeIndex = Math.max(0, Math.min(codes.length - 1, r.codeIndex + delta));
    await this.navigate(); await this.save();
  }

  private async block(id: string) {
    const r = this.reading;
    if (!r) return;
    const index = r.entries.findIndex(e => e.step.blocks.some(b => b.id === id && b.type === 'code'));
    if (index < 0) {
      const reply = commentContent(r).find(b => b.block.id === id)?.block;
      if (reply?.type === 'code' && reply.snippet.source) { this.activeReply = reply; await this.navigate(); }
      else if (reply?.type === 'code') { r.focusBlock = reply.id; this.render(); }
      return;
    }
    this.activeReply = undefined;
    r.index = index;
    r.view = 'step'; r.partId = r.entries[index].part.id;
    r.codeIndex = codeBlocks(r.entries[index].step).findIndex(b => b.id === id);
    r.focusBlock = id;
    await this.navigate(); await this.save();
  }

  private async rootFor(location: Location, choose = false, interactive = true): Promise<vscode.Uri | undefined> {
    const r = this.reading!;
    const key = repositoryKey(location.repositoryUrl ?? r.doc.source?.repositoryUrl);
    const roots = this.context.workspaceState.get<Record<string, string>>(this.rootsKey(), {});
    if (!choose && roots[key]) return vscode.Uri.parse(roots[key]);
    const folders = vscode.workspace.workspaceFolders ?? [];
    const mainRepo = key === repositoryKey(r.doc.source?.repositoryUrl);
    let root: vscode.Uri | undefined;
    if (!choose && mainRepo) {
      // Prefer the repository containing the walkthrough. For downloaded files,
      // a single open workspace is the natural default. More roots need a choice.
      root = vscode.workspace.getWorkspaceFolder(this.uri!)?.uri;
      if (!root && folders.length === 1) root = folders[0].uri;
    }
    if (!root && interactive) {
      const options = folders.filter(f => f.uri.scheme === 'file').map(f => ({ label: f.name, description: f.uri.fsPath, uri: f.uri }));
      const picked = await vscode.window.showQuickPick([...options, { label: 'Browse for a repository folder…', description: '', uri: undefined }], {
        title: 'CW: choose repository folder', placeHolder: key || 'The folder source paths are relative to',
      });
      if (!picked) return undefined;
      root = picked.uri;
      if (!root) root = (await vscode.window.showOpenDialog({ canSelectFolders: true, canSelectFiles: false, canSelectMany: false, title: `Repository folder: ${key || r.doc.title}` }))?.[0];
    }
    if (root?.scheme !== 'file') return undefined;
    roots[key] = root.toString();
    await this.context.workspaceState.update(this.rootsKey(), roots);
    return root;
  }

  private async chooseRoot() {
    const r = this.reading;
    if (!r) return;
    const location = codeBlocks(r.entries[r.index].step)[r.codeIndex]?.snippet.source ?? { file: '', repositoryUrl: r.doc.source?.repositoryUrl };
    if (await this.rootFor(location, true)) await this.navigate();
  }

  private async navigate(reveal = true, relativeLine?: number) {
    const r = this.reading;
    if (!r) return;
    this.clearDecorations(); this.activeSource = undefined;
    r.status = undefined; r.root = undefined;
    await this.checkSnippets();
    if (r.view !== 'step' && !this.activeReply) { this.render(); return; }
    const block = this.activeReply ?? codeBlocks(r.entries[r.index].step)[r.codeIndex];
    if (!block) { this.render(); return; }
    r.focusBlock = block.id;
    const location = block.snippet.source!;
    try {
      const root = await this.rootFor(location, false, reveal);
      if (!root) { r.status = 'Choose a repository folder to open this code. The saved snippet is shown below.'; return; }
      r.root = root.fsPath;
      const filename = await containedPath(root.fsPath, location.file);
      const document = await vscode.workspace.openTextDocument(vscode.Uri.file(filename));
      this.activeSource = document.uri.toString();
      const result = resolveSnippet(document.getText(), block.snippet);
      const dirty = document.isDirty ? ' Includes unsaved editor changes.' : '';
      if (result.state === 'different') { r.status = 'The local code differs from the saved snippet. Showing the original snippet below.' + dirty; return; }
      if (result.state === 'ambiguous') { r.status = 'This snippet occurs in more than one place. Its new location is ambiguous; showing the original below.'; return; }
      r.status = (result.state === 'moved' ? `Code found at lines ${result.startLine}–${result.endLine}; its saved location has moved.` : 'Code matches the local file.') + dirty;
      const start = result.startLine! - 1, end = result.endLine! - 1;
      const editor = reveal ? await vscode.window.showTextDocument(document, { preview: true, preserveFocus: true, viewColumn: vscode.ViewColumn.One })
        : vscode.window.visibleTextEditors.find(e => e.document.uri.toString() === document.uri.toString());
      if (!editor) return;
      this.decorate(editor, block, start, end);
      if (reveal) {
        const first = relativeLine ?? block.snippet.highlights?.[0]?.lines.start ?? 1;
        editor.selection = new vscode.Selection(start + first - 1, 0, start + first - 1, 0);
        editor.revealRange(new vscode.Range(start, 0, end, document.lineAt(end).text.length), vscode.TextEditorRevealType.InCenterIfOutsideViewport);
      }
    } catch (err) {
      r.status = `Cannot open ${location.file}: ${message(err)} The saved snippet remains available below.`;
    } finally { this.render(); }
  }

  private async checkSnippets() {
    const r = this.reading;
    if (!r) return;
    r.snippetChecks = {};
    this.checkedSources.clear();
    const blocks = r.entries[r.index].step.blocks.map((block, index) => ({ block, index })).concat(commentContent(r).map(({ block, index }) => ({ block, index })));
    await Promise.all(blocks.map(async ({ block, index }) => {
      if (block.type !== 'code' || !block.snippet.source) return;
      const source = block.snippet.source;
      try {
        const root = await this.rootFor(source, false, false);
        if (!root) { r.snippetChecks![index] = 'No local repository selected.'; return; }
        const file = await containedPath(root.fsPath, source.file);
        this.checkedSources.add(vscode.Uri.file(file).toString());
        const document = await vscode.workspace.openTextDocument(vscode.Uri.file(file));
        const result = resolveSnippet(document.getText(), block.snippet);
        r.snippetChecks![index] = (result.state === 'match' ? 'Matches local code.' : result.state === 'moved' ? `Moved to lines ${result.startLine}–${result.endLine}.`
          : result.state === 'ambiguous' ? 'Several matches; location is ambiguous.' : 'Local code differs from this saved snippet.') + (document.isDirty ? ' Includes unsaved changes.' : '');
      } catch { r.snippetChecks![index] = 'Local source is missing or unavailable.'; }
    }));
  }

  private decorate(editor: vscode.TextEditor, block: CodeBlock, start: number, end: number) {
    this.editors.add(editor);
    const ranges = (kind: string) => (block.snippet.highlights ?? []).filter(h => (h.kind ?? 'focus') === kind).map(h =>
      new vscode.Range(start + h.lines.start - 1, 0, start + (h.lines.end ?? h.lines.start) - 1, 0));
    const focus = ranges('focus');
    editor.setDecorations(this.focus, block.snippet.highlights?.length ? focus : [new vscode.Range(start, 0, end, 0)]);
    editor.setDecorations(this.added, ranges('added'));
    editor.setDecorations(this.notes, (block.snippet.annotations ?? []).map(a => {
      const hoverMessage = new vscode.MarkdownString(a.text);
      hoverMessage.isTrusted = false; hoverMessage.supportHtml = false;
      return { range: new vscode.Range(start + a.lines.start - 1, 0, start + (a.lines.end ?? a.lines.start) - 1, 0), hoverMessage };
    }));
  }

  private clearDecorations() {
    for (const editor of this.editors) {
      try { editor.setDecorations(this.focus, []); editor.setDecorations(this.added, []); editor.setDecorations(this.notes, []); } catch { /* disposed editor */ }
    }
    this.editors.clear();
  }

  private scheduleRefresh(walkthrough: boolean) {
    this.pendingReload ||= walkthrough;
    if (this.timer) clearTimeout(this.timer);
    this.timer = setTimeout(() => void this.run(async () => {
      if (!this.reading || !this.uri) return;
      const reload = this.pendingReload;
      this.pendingReload = false;
      if (reload) {
        try {
          await vscode.workspace.fs.stat(this.uri);
          const doc = parseWalkthrough((await vscode.workspace.openTextDocument(this.uri)).getText());
          const current = this.reading.entries[this.reading.index].key;
          const blockId = codeBlocks(this.reading.entries[this.reading.index].step)[this.reading.codeIndex]?.id ?? this.reading.focusBlock;
          const entries = flatten(doc), index = Math.max(0, entries.findIndex(e => e.key === current));
          this.reading = { ...this.reading, doc, entries, index, error: undefined,
            codeIndex: Math.max(0, codeBlocks(entries[index].step).findIndex(b => !!b.id && b.id === blockId)),
            done: new Set([...this.reading.done].filter(key => entries.some(e => e.key === key))),
          };
          if (blockId && entries[index].step.blocks.some(b => b.id === blockId && b.type === 'code' && !b.snippet.source)) {
            this.reading.codeIndex = -1; this.reading.focusBlock = blockId;
          } else if (!blockId || !entries[index].step.blocks.some(b => b.id === blockId)) this.reading.focusBlock = undefined;
        } catch (err) { this.reading.error = message(err); this.render(); return; }
      }
      await this.navigate(false); await this.save();
    }), 350);
  }

  private async restart() {
    this.activeReply = undefined;
    if (!this.reading) return;
    this.reading.index = 0; this.reading.codeIndex = 0; this.reading.focusBlock = undefined; this.reading.done.clear(); this.reading.view = 'overview'; this.reading.partId = undefined;
    await this.navigate(); await this.save();
  }
  private async resume() {
    if (!this.reading) await this.restore();
    await vscode.commands.executeCommand('cw.content.focus');
    if (this.reading) await this.navigate(); else await this.open();
  }
  private async close() {
    this.referenceHistory = [];
    await this.stopComments();
    await this.save();
    if (this.timer) clearTimeout(this.timer);
    this.pendingReload = false;
    if (this.remoteTimer) clearTimeout(this.remoteTimer);
    this.remote = undefined;
    this.watched?.dispose(); this.watched = undefined;
    this.clearDecorations(); this.activeSource = undefined; this.reading = undefined; this.uri = undefined;
    await this.context.workspaceState.update('cw.last', undefined);
    await vscode.commands.executeCommand('setContext', 'cw.loaded', false);
    this.render();
  }
  dispose() {
    void this.stopComments();
    if (this.timer) clearTimeout(this.timer);
    if (this.remoteTimer) clearTimeout(this.remoteTimer);
    this.remote = undefined;
    this.watched?.dispose(); this.clearDecorations();
    for (const disposable of this.subscriptions) disposable.dispose();
  }
}
function message(err: unknown) { return err instanceof Error ? err.message : String(err); }
export async function activate(context: vscode.ExtensionContext) {
  const reader = new Reader(context);
  context.subscriptions.push(reader);
  await reader.restore();
  return reader;
}
