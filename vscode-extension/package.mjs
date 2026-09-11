import { createRequire } from 'node:module';
import { spawn } from 'node:child_process';
const require = createRequire(import.meta.url);
const child = spawn(process.execPath, [require.resolve('@vscode/vsce/vsce'), 'package', '--target', `${process.platform}-${process.arch}`, '--allow-missing-repository', '--out', 'cw-walkthrough.vsix'], { stdio: 'inherit', windowsHide: true });
child.on('error', error => { console.error(error.message); process.exitCode = 1; });
child.on('exit', code => { process.exitCode = code ?? 1; });
