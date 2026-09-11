import { realpath } from 'node:fs/promises';
import * as path from 'node:path';

export async function containedPath(root: string, file: string): Promise<string> {
  if (!file || file.split('/').some(p => !p || p === '.' || p === '..') || /[\\:\x00-\x1f\x7f]/.test(file))
    throw new Error('Source path must be relative to its repository.');
  const actualRoot = await realpath(root);
  const target = await realpath(path.join(actualRoot, ...file.split('/')));
  const relative = path.relative(actualRoot, target);
  if (!relative || relative === '..' || relative.startsWith('..' + path.sep) || path.isAbsolute(relative))
    throw new Error('Source path resolves outside the selected repository.');
  return target;
}
