import { runTests } from '@vscode/test-electron';
import { mkdir, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { build } from 'esbuild';
const extensionDevelopmentPath = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const workspace = path.join(extensionDevelopmentPath, '.vscode-test', 'fixture');
await mkdir(path.join(workspace, 'src'), { recursive: true });
await build({ entryPoints: ['test/fixture.ts'], outfile: '.vscode-test/fixture.mjs', bundle: true, platform: 'node', format: 'esm' });
await build({ entryPoints: ['test/rich-fixture.ts'], outfile: '.vscode-test/rich-fixture.mjs', bundle: true, platform: 'node', format: 'esm' });
const { fixture } = await import('../.vscode-test/fixture.mjs');
const { richFixture } = await import('../.vscode-test/rich-fixture.mjs');
await writeFile(path.join(workspace, 'tour.walkthrough.json'), JSON.stringify(fixture(), null, 2));
await writeFile(path.join(workspace, 'rich.walkthrough.json'), JSON.stringify(richFixture, null, 2));
await writeFile(path.join(workspace, 'src', 'a.ts'), '// context\nexport const a = 1;\nexport const b = 2;\n');
await writeFile(path.join(workspace, 'src', 'b.ts'), '// context\n// more context\nexport const moved = true;\n');
await runTests({
  extensionDevelopmentPath: process.env.CW_EXTENSION_UNDER_TEST || extensionDevelopmentPath,
  extensionTestsPath: path.join(extensionDevelopmentPath, 'dist', 'integration.js'),
  extensionTestsEnv: { APPDATA: path.join(extensionDevelopmentPath, '.vscode-test', 'config'), XDG_CONFIG_HOME: path.join(extensionDevelopmentPath, '.vscode-test', 'config') },
  ...(process.env.CW_VSCODE_EXECUTABLE ? { vscodeExecutablePath: process.env.CW_VSCODE_EXECUTABLE } : {}),
  launchArgs: [workspace, '--disable-extensions', '--disable-workspace-trust', '--skip-welcome', '--skip-release-notes',
    '--user-data-dir=' + path.join(extensionDevelopmentPath, '.vscode-test', 'integration-user'),
    '--extensions-dir=' + path.join(extensionDevelopmentPath, '.vscode-test', 'extensions')],
});
