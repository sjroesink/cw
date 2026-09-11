import { readFile } from 'node:fs/promises';
import { spawn, ChildProcess } from 'node:child_process';
import { homedir } from 'node:os';
import * as path from 'node:path';
import { createHash } from 'node:crypto';
import type { Block } from './model';

export interface CommentWhere { step: string; title?: string; block?: string; kind?: string; file?: string; lines?: { start: number; end: number }; quote?: string; start?: number }
export interface CommentThread { id: string; at: string; status: string; archived?: boolean; where: CommentWhere; messages: { from: string; at: string; text?: string; blocks?: Block[] }[] }
export interface CommentsView { rev: number; watching: boolean; prompt?: string; threads: CommentThread[] }
interface Run { pid: number; port: number; token: string; key: string; file: string; basePath?: string }

export function settingsDirectory(env = process.env): string {
  const config = process.platform === 'win32' ? env.APPDATA
    : process.platform === 'darwin' ? path.join(homedir(), 'Library', 'Application Support') : env.XDG_CONFIG_HOME;
  return path.join(config || path.join(homedir(), '.config'), 'code-walkthrough');
}
function slug(text: string): string {
  // Match the CLI's ASCII slug normalization for persisted comment identity.
  return text.trim().toLowerCase().replace(/[\s_/.\-]/gu, '-').replace(/[^a-z0-9-]/g, '').replace(/-+/g, '-').replace(/^-|-$/g, '').slice(0, 48).replace(/^-|-$/g, '') || 'x';
}
export function commentKey(file: string, publishedSlug?: string): string {
  if (publishedSlug) return slug(publishedSlug);
  const clean = path.normalize(file);
  const normalized = process.platform === 'win32' ? clean.toLowerCase() : clean;
  return slug(path.basename(file, path.extname(file))) + '-' + createHash('sha256').update(normalized).digest('hex').slice(0, 8);
}

/** Uses the CLI server as the single writer. Never writes the comments JSON. */
export class CommentsClient {
  private run?: Run;
  private child?: ChildProcess;
  private closed = false;
  constructor(private readonly directory = settingsDirectory(), private readonly env = process.env) {}

  private async records(): Promise<Run[]> {
    try {
      const value = JSON.parse(await readFile(path.join(this.directory, 'servers.json'), 'utf8'));
      return Array.isArray(value) ? value.filter(r => r && Number.isInteger(r.pid) && Number.isInteger(r.port) && r.port > 0 && r.port < 65536 && typeof r.token === 'string' && typeof r.key === 'string') : [];
    } catch { return []; }
  }

  async connect(binary: string, file: string, root: string, publishedSlug?: string) {
    const key = commentKey(file, publishedSlug);
    for (const run of await this.records()) {
      if (run.key !== key) continue;
      try { await this.request(run, '/api/comments'); this.run = run; return; } catch { /* stale registration */ }
    }
    if (this.closed) throw new Error('Comments were closed.');
    const args = ['serve', file, '--root', root, '--port', '0', '--no-open', '--offline'];
    if (publishedSlug) args.push('--comment-key', publishedSlug);
    const child = spawn(binary, args, { windowsHide: true, shell: false, stdio: 'ignore', env: this.env });
    this.child = child;
    let failed: Error | undefined;
    child.on('error', error => { failed = error; });
    const deadline = Date.now() + 10000;
    while (Date.now() < deadline && !this.closed) {
      if (failed) throw new Error('Cannot start the bundled comment service: ' + failed.message);
      if (child.exitCode !== null) throw new Error('The comment service stopped before it was ready.');
      const run = (await this.records()).find(r => r.pid === child.pid && r.key === key);
      if (run) {
        try { await this.request(run, '/api/comments'); this.run = run; return; } catch { /* startup still pending */ }
      }
      await new Promise(resolve => setTimeout(resolve, 100));
    }
    this.dispose();
    throw new Error('The comment service did not become available.');
  }

  private async request(run: Run, endpoint: string, data?: unknown): Promise<any> {
    const basePath = typeof run.basePath === 'string' && /^\/r\/[a-zA-Z0-9_-]+$/.test(run.basePath) ? run.basePath : '';
    const response = await fetch(`http://127.0.0.1:${run.port}${basePath}${endpoint}`, {
      method: data === undefined ? 'GET' : 'POST', redirect: 'error', signal: AbortSignal.timeout(3000),
      headers: { 'X-Cw-Token': run.token, ...(data === undefined ? {} : { 'Content-Type': 'application/json' }) },
      body: data === undefined ? undefined : JSON.stringify(data),
    });
    if (!response.ok) { await response.body?.cancel(); throw new Error(`Comment service returned HTTP ${response.status}.`); }
    return response.json();
  }

  async view(): Promise<CommentsView> {
    if (!this.run) throw new Error('Comments are not connected.');
    return this.request(this.run, '/api/comments');
  }
  async add(where: CommentWhere, text: string) {
    if (!this.run) throw new Error('Comments are not connected.');
    await this.request(this.run, '/api/comments', { where, text });
  }
  async reply(id: string, text: string) {
    if (!this.run || !/^\d+$/.test(id)) throw new Error('Unknown comment.');
    await this.request(this.run, '/api/comments/' + id, { from: 'reader', text });
  }
  async archive(id: string, on: boolean) {
    if (!this.run || !/^\d+$/.test(id)) throw new Error('Unknown comment.');
    await this.request(this.run, '/api/comments/' + id + '/archive', { on });
  }
  dispose() {
    this.closed = true;
    this.child?.kill(); this.child = undefined; this.run = undefined;
  }
}
