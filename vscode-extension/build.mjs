import { build } from 'esbuild';
import { mkdir, copyFile, readFile, readdir, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
await mkdir('dist', { recursive: true });
await mkdir('dist/native', { recursive: true });
await promisify(execFile)('go', ['build', '-trimpath', '-ldflags=-s -w', '-o', path.resolve('dist/native', process.platform === 'win32' ? 'cw.exe' : 'cw'), '.'], { cwd: '..', windowsHide: true });
const result = await build({
  entryPoints: { extension: 'src/extension.ts', 'core.test': 'test/core.test.ts', integration: 'test/integration.ts', 'browser-server': 'test/browser-server.ts' },
  outdir: 'dist', bundle: true, platform: 'node', target: 'node20',
  format: 'cjs', external: ['vscode'], sourcemap: true, metafile: true,
});
const browser = await build({
  entryPoints: ['src/sidebar.ts'], outfile: 'dist/webview/sidebar.js', bundle: true,
  platform: 'browser', target: 'chrome128', format: 'iife', minify: true,
  metafile: true, legalComments: 'linked',
});
await copyFile('../schema/walkthrough.v2.schema.json', 'dist/walkthrough.v2.schema.json');
const packages = new Set();
for (const filename of [...Object.keys(result.metafile.inputs), ...Object.keys(browser.metafile.inputs)]) {
  const match = filename.match(/^(.*node_modules\/(?:@[^/]+\/)?[^/]+)/);
  if (match) packages.add(match[1]);
}
const notices = [];
for (const directory of [...packages].sort()) {
  const pkg = JSON.parse(await readFile(path.join(directory, 'package.json'), 'utf8'));
  for (const filename of await readdir(directory)) {
    if (!/^(licen[sc]e|copying|notice)(\.|$|-)/i.test(filename)) continue;
    notices.push(`${pkg.name} ${pkg.version} — ${filename}\n\n${await readFile(path.join(directory, filename), 'utf8')}`);
  }
}
await writeFile('dist/THIRD_PARTY_NOTICES.txt', notices.join('\n\n' + '='.repeat(72) + '\n\n'));
