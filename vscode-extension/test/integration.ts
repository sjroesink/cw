import * as vscode from 'vscode';
import assert from 'node:assert/strict';
import type { Reading } from '../src/render';
import { createServer } from 'node:http';
import { fixture } from './fixture';
import { CommentsClient } from '../src/comments';
import { richFixture } from './rich-fixture';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { writeFile } from 'node:fs/promises';

export async function run() {
  const extension = vscode.extensions.getExtension<{ reading?: Reading; contentReady: boolean; contentReport?: { diagrams: number; failedDiagrams: number; timelines: number; highlighted: number } }>('sjroesink.cw-walkthrough');
  assert.ok(extension, 'Extension was discovered');
  const reader = await extension.activate();
  const folder = vscode.workspace.workspaceFolders![0].uri;
  const file = vscode.Uri.joinPath(folder, 'tour.walkthrough.json');
  await vscode.commands.executeCommand('cw.open', file);
  await eventually(() => reader.contentReady);
  await vscode.commands.executeCommand('cw.restart');
  assert.equal(reader.reading?.view, 'overview');
  await vscode.commands.executeCommand('cw.next');
  assert.equal(reader.reading?.view, 'part');
  await vscode.commands.executeCommand('cw.next');
  assert.equal(reader.reading?.view, 'step');
  assert.equal(reader.reading?.index, 0);
  assert.ok(Object.values(reader.reading!.snippetChecks ?? {}).some(value => value.includes('Matches local')));
  assert.ok(Object.values(reader.reading!.snippetChecks ?? {}).some(value => value.includes('Moved to lines 3')), 'All snippets are checked before selecting their code');
  await vscode.commands.executeCommand('cw.copyLink');
  const copied = new URL(await vscode.env.clipboard.readText());
  assert.equal(copied.searchParams.get('page'), 'part/section/first');
  assert.equal(copied.searchParams.get('file')?.toLowerCase(), file.fsPath.toLowerCase());
  const config = vscode.workspace.getConfiguration('cw');
  const oldTheme = config.inspect('theme')?.globalValue;
  try {
    await config.update('theme', 'dark', vscode.ConfigurationTarget.Global);
    await vscode.commands.executeCommand('cw.toggleTheme');
    assert.equal(vscode.workspace.getConfiguration('cw').get('theme'), 'light');
  } finally { await config.update('theme', oldTheme, vscode.ConfigurationTarget.Global); }
  let editor = vscode.window.visibleTextEditors.find(e => e.document.uri.path.endsWith('/src/a.ts'));
  assert.ok(editor, 'First source opened');
  assert.equal(editor.selection.active.line, 2, 'Snippet-relative highlight resolves to file line 3');
  await vscode.commands.executeCommand('cw.nextCode');
  await eventually(() => reader.contentReady);
  assert.equal(reader.reading?.codeIndex, 1);
  editor = vscode.window.visibleTextEditors.find(e => e.document.uri.path.endsWith('/src/b.ts'));
  assert.ok(editor, 'Second source opened');
  assert.equal(editor.selection.active.line, 2, 'Moved code opens at its current line');
  assert.match(reader.reading!.status!, /found at lines 3/);

  // Editor changes are rechecked without jumping the cursor while typing.
  const edit = new vscode.WorkspaceEdit();
  edit.insert(editor.document.uri, new vscode.Position(0, 0), '// inserted unsaved\n');
  await vscode.workspace.applyEdit(edit);
  await eventually(() => /found at lines 4/.test(reader.reading?.status ?? ''));
  assert.match(reader.reading!.status!, /unsaved/);
  await editor.document.save();

  await vscode.commands.executeCommand('cw.close');
  await vscode.commands.executeCommand('cw.open', file);
  assert.equal(reader.reading?.codeIndex, 1, 'Block id resumes the selected location');
  await vscode.commands.executeCommand('cw.next');
  assert.equal(reader.reading?.index, 1);
  assert.match(reader.reading!.status!, /Cannot open missing.ts/);
  assert.ok(reader.reading?.done.has('part/section/first'));
  await vscode.commands.executeCommand('cw.next');
  assert.equal(reader.reading?.index, 2);
  assert.equal(reader.reading?.status, undefined, 'Prose-only step has no stale location status');
  await vscode.commands.executeCommand('cw.close');
  await vscode.commands.executeCommand('cw.open', file);
  assert.equal(reader.reading?.index, 2, 'Step is resumed');
  await vscode.commands.executeCommand('cw.next');
  assert.equal(reader.reading?.done.size, 3, 'Finish marks the last step complete');
  assert.equal(reader.reading?.view, 'overview', 'The final step returns to the overview');
  await vscode.commands.executeCommand('cw.previous');
  assert.equal(reader.reading?.view, 'step', 'Previous wraps from overview to the last step');

  const walkthroughDoc = await vscode.workspace.openTextDocument(file);
  const original = walkthroughDoc.getText();
  const invalid = new vscode.WorkspaceEdit();
  invalid.replace(file, new vscode.Range(0, 0, walkthroughDoc.lineCount, 0), '{');
  await vscode.workspace.applyEdit(invalid);
  await eventually(() => !!reader.reading?.error);
  assert.equal(reader.reading?.index, 2, 'Invalid edits retain the last valid walkthrough');
  const repair = new vscode.WorkspaceEdit();
  repair.replace(file, new vscode.Range(0, 0, walkthroughDoc.lineCount, 0), original);
  await vscode.workspace.applyEdit(repair);
  await eventually(() => !reader.reading?.error);
  await walkthroughDoc.save();
  await vscode.commands.executeCommand('cw.restart');
  assert.equal(reader.reading?.index, 0);
  assert.equal(reader.reading?.done.size, 0);
  await vscode.commands.executeCommand('cw.open', vscode.Uri.joinPath(folder, 'rich.walkthrough.json'));
  await vscode.commands.executeCommand('cw.selectStep', 'chapter/features/rich');
  await eventually(() => reader.contentReady);
  assert.equal(reader.contentReport?.diagrams, 1, 'Mermaid renders inside the actual webview CSP');
  assert.equal(reader.contentReport?.failedDiagrams, 0);
  assert.equal(reader.contentReport?.timelines, 1, 'Timeline runtime initialized');
  assert.equal(reader.contentReport?.highlighted, 2, 'Snippet and diff syntax highlighting initialized');
  const action = (action: string, value?: string) => (reader as any).receiveMessage({ action, value, token: (reader as any).token });
  await action('copy', '2');
  assert.equal(await vscode.env.clipboard.readText(), 'export const a = 1;\nexport const b = 2;\n');
  await action('copySource', '2');
  assert.equal(await vscode.env.clipboard.readText(), 'https://github.com/example/project/blob/head/src/a.ts#L2');
  await action('diffLine', '5:3');
  assert.equal(vscode.window.visibleTextEditors.find(e => e.document.uri.path.endsWith('/src/a.ts'))?.selection.active.line, 2, 'Diff after-line opens the requested native line');
  await action('copy', '5');
  assert.match(await vscode.env.clipboard.readText(), /@@ -2,2 \+2,2 @@/);
  const clipboardBefore = await vscode.env.clipboard.readText();
  await (reader as any).receiveMessage({ action: 'copy', value: '2', token: 'stale-page' });
  assert.equal(await vscode.env.clipboard.readText(), clipboardBefore, 'Stale page actions cannot affect the current reading');
  await vscode.commands.executeCommand('cw.comments');
  assert.equal(reader.reading?.commentsOpen, true);
  assert.ok(reader.reading?.comments, reader.reading?.commentsError);
  const client = new CommentsClient();
  const binary = vscode.Uri.joinPath(extension.extensionUri, 'dist', 'native', process.platform === 'win32' ? 'cw.exe' : 'cw').fsPath;
  try {
    await client.connect(binary, vscode.Uri.joinPath(folder, 'rich.walkthrough.json').fsPath, folder.fsPath);
    await client.add({ step: 'chapter/features/rich', title: 'Rich step', quote: 'Handler' }, 'Explain the handler');
    const id = (await client.view()).threads.at(-1)!.id;
    const replyFile = vscode.Uri.joinPath(folder, 'agent-reply.json').fsPath;
    await writeFile(replyFile, JSON.stringify(richFixture.parts[0].sections[0].steps[0].blocks), 'utf8');
    await promisify(execFile)(binary, ['comments', 'reply', id, '--file', replyFile], { windowsHide: true, timeout: 10000 });
    await eventually(() => reader.reading?.comments?.threads.find(t => t.id === id)?.status === 'answered');
    await eventually(() => reader.contentReady);
    assert.ok((reader.contentReport?.diagrams ?? 0) >= 2, 'Agent reply diagrams render in the real webview');
    assert.equal(reader.contentReport?.failedDiagrams, 0);
    await client.archive(id, true);
    await eventually(() => reader.reading?.comments?.threads.find(t => t.id === id)?.archived === true);
  } finally { client.dispose(); }
  const published = fixture();
  let stamp = 'initial';
  const server = createServer((req, res) => {
    res.setHeader('Content-Type', 'application/json');
    res.end(JSON.stringify(req.url?.endsWith('/state') ? { stamp } : { doc: published, stamp, meta: { slug: 'integration' } }));
  });
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  assert.ok(address && typeof address !== 'string');
  const pageUrl = `http://127.0.0.1:${address.port}/w/integration`;
  let cachedUri: vscode.Uri;
  try {
    await vscode.commands.executeCommand('cw.openPublished', pageUrl + '#part/section/first');
    assert.equal(reader.reading?.publishedUrl, pageUrl, (reader as { lastError?: string }).lastError);
    assert.equal(reader.reading?.view, 'step', 'Published deep links select a step');
    await vscode.commands.executeCommand('cw.copyLink');
    assert.equal(await vscode.env.clipboard.readText(), pageUrl + '#part/section/first');
    cachedUri = vscode.Uri.parse(reader.reading!.identity!);
    const fallbackDoc: any = fixture(); fallbackDoc.title = 'Published reference source';
    fallbackDoc.parts[0].sections[0].steps[0].blocks = [{ type: 'reference', title: 'Published fallback', target: { file: './not-present.json', url: pageUrl, stepId: 'third' } }];
    const fallbackFile = vscode.Uri.joinPath(folder, 'published-reference.json');
    await writeFile(fallbackFile.fsPath, JSON.stringify(fallbackDoc));
    await vscode.commands.executeCommand('cw.open', fallbackFile);
    await vscode.commands.executeCommand('cw.selectStep', 'part/section/first');
    await action('reference', '0');
    assert.equal(reader.reading?.publishedUrl, pageUrl, 'Missing file falls back to published URL');
    assert.equal(reader.reading?.entries[reader.reading.index].step.id, 'third');
    await action('referenceBack');
    assert.equal(reader.reading?.doc.title, 'Published reference source');
    await vscode.commands.executeCommand('cw.open', cachedUri);
    await vscode.commands.executeCommand('cw.selectStep', 'part/section/first');
    published.title = 'Updated published walkthrough';
    stamp = 'updated';
    await eventually(() => reader.reading?.doc.title === published.title);
    assert.equal(reader.reading?.entries[reader.reading.index].key, 'part/section/first', 'Updates preserve position');
    await vscode.commands.executeCommand('cw.close');
  } finally {
    server.closeAllConnections();
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
  await vscode.commands.executeCommand('cw.open', cachedUri!);
  assert.equal(reader.reading?.doc.title, 'Updated published walkthrough', 'Downloaded copy works offline');
  await eventually(() => !!reader.reading?.remoteNotice);
  assert.match(reader.reading!.remoteNotice!, /downloaded copy/);
  await vscode.commands.executeCommand('cw.close');
  assert.equal(Boolean(reader.reading), false);
  const refDoc: any = fixture();
  refDoc.title = 'Reference source';
  refDoc.parts[0].sections[0].steps[0].blocks = [
    { type: 'reference', title: 'Target', target: { file: './reference-target.json', stepId: 'third' } },
    { type: 'reference', title: 'Unavailable', target: { file: './not-here.json' } },
  ];
  const refFile = vscode.Uri.joinPath(folder, 'references.json');
  await writeFile(refFile.fsPath, JSON.stringify(refDoc));
  const targetDoc = fixture(); targetDoc.title = 'Reference target';
  await writeFile(vscode.Uri.joinPath(folder, 'reference-target.json').fsPath, JSON.stringify(targetDoc));
  await vscode.commands.executeCommand('cw.open', refFile);
  await vscode.commands.executeCommand('cw.selectStep', 'part/section/first');
  await action('reference', '0');
  assert.equal(reader.reading?.doc.title, 'Reference target');
  assert.equal(reader.reading?.entries[reader.reading.index].step.id, 'third');
  assert.equal(reader.reading?.canReturn, true);
  await action('referenceBack');
  assert.equal(reader.reading?.doc.title, 'Reference source');
  assert.equal(reader.reading?.entries[reader.reading.index].step.id, 'first');
  await action('reference', '1');
  assert.equal(reader.reading?.doc.title, 'Reference source', 'Missing target retains current reading');
  await vscode.commands.executeCommand('cw.close');
  console.log('Integration passed, including reference navigation, return position and unavailable target.');
}
async function eventually(check: () => boolean) {
  const deadline = Date.now() + 15000;
  while (!check()) {
    if (Date.now() > deadline) throw new Error('Timed out waiting for live update');
    await new Promise(resolve => setTimeout(resolve, 50));
  }
}
