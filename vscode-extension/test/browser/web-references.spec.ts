import { test, expect } from '@playwright/test';
import { spawn, execFile } from 'node:child_process';
import { mkdtemp, readFile, writeFile, rm } from 'node:fs/promises';
import { promisify } from 'node:util';
import { tmpdir } from 'node:os';
import * as path from 'node:path';

test('real web reader opens references, returns to position and handles missing targets', async ({ page, context }, testInfo) => {
  const dir = await mkdtemp(path.join(tmpdir(), 'cw-web-reference-'));
  const doc = (title: string, blocks: any[]) => ({ version: 'cw/2', title, parts: [{ id: 'part', title: 'Part', sections: [{ id: 'section', title: 'Section', steps: [{ id: 'read', title: 'Read', blocks }] }] }] });
  await writeFile(path.join(dir, 'target.json'), JSON.stringify(doc('Target walkthrough', [{ type: 'markdown', text: 'This is the destination.' }])));
  await writeFile(path.join(dir, 'parent.json'), JSON.stringify(doc('Parent walkthrough', [
    { type: 'reference', title: 'Read target', target: { file: './target.json', stepId: 'read' } },
    { type: 'reference', title: 'Missing target', target: { file: './missing.json' } },
    { type: 'reference', title: 'Missing step', target: { file: './target.json', stepId: 'no-such-step' } },
    { type: 'reference', title: 'Published alternative', target: { file: './missing.json', url: 'https://example.test/w/target', stepId: 'read' } },
  ])));
  const child = spawn(path.resolve('dist/native', process.platform === 'win32' ? 'cw.exe' : 'cw'), ['serve', path.join(dir, 'parent.json'), '--root', dir, '--port', '0', '--no-open', '--offline'],
    { windowsHide: true, env: { ...process.env, APPDATA: dir, XDG_CONFIG_HOME: dir, HOME: dir } });
  try {
    const url = await new Promise<string>((resolve, reject) => {
      const timeout = setTimeout(() => reject(new Error('Reader did not start')), 10000);
      let output = '';
      child.stdout.on('data', data => { output += data; const match = output.match(/http:\/\/127\.0\.0\.1:\d+\//); if (match) { clearTimeout(timeout); resolve(match[0]); } });
      child.on('error', reject);
      child.on('exit', code => { clearTimeout(timeout); reject(new Error(`Reader exited: ${code}`)); });
    });
    await page.goto(url + '#part/section/read');
    await page.locator('.reference').filter({ hasText: 'Missing target' }).getByRole('button').click();
    await expect(page).toHaveURL(url + '#part/section/read');
    await expect(page.locator('.reference')).toHaveCount(4);
    await context.route('https://example.test/**', route => route.fulfill({ contentType: 'text/html', body: '<p>Published target</p>' }));
    const opened = context.waitForEvent('page');
    await page.locator('.reference').filter({ hasText: 'Published alternative' }).getByRole('button').click();
    const published = await opened;
    await expect(published).toHaveURL('https://example.test/w/target#step=read');
    await expect(page).toHaveURL(url + '#part/section/read');
    await published.close();
    await page.screenshot({ path: testInfo.outputPath('reference-cards.png'), fullPage: true });
    await page.locator('.reference').filter({ hasText: 'Read target' }).getByRole('button').click();
    await expect(page.getByText('This is the destination.', { exact: true })).toBeVisible();
    await expect(page).toHaveURL(/\/r\/[^/]+\/#step=read$/);
    const records = JSON.parse(await readFile(path.join(dir, ...(process.platform === 'darwin' ? ['Library', 'Application Support'] : []), 'code-walkthrough', 'servers.json'), 'utf8'));
    expect(records).toHaveLength(2);
    const linked = records.find((run: any) => run.basePath);
    const comments = await promisify(execFile)(path.resolve('dist/native', process.platform === 'win32' ? 'cw.exe' : 'cw'), ['comments', 'list', linked.key, '--json'],
      { windowsHide: true, env: { ...process.env, APPDATA: dir, XDG_CONFIG_HOME: dir, HOME: dir } });
    expect(JSON.parse(comments.stdout).threads).toEqual([]);
    await page.reload();
    await expect(page.getByText('This is the destination.', { exact: true })).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath('reference-return.png'), fullPage: true });
    await page.getByRole('button', { name: 'Back to previous walkthrough' }).click();
    await expect(page).toHaveURL(url + '#part/section/read');
    await page.locator('.reference').filter({ hasText: 'Missing step' }).getByRole('button').click();
    await expect(page.getByText('The referenced step was not found. Showing the overview.', { exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Back to previous walkthrough' })).toBeVisible();
  } finally {
    child.kill();
    await new Promise<void>(resolve => child.exitCode !== null ? resolve() : child.once('exit', () => resolve()));
    await rm(dir, { recursive: true, force: true });
  }
});
