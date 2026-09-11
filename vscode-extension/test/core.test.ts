import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, mkdir, readFile, rm, symlink, writeFile } from 'node:fs/promises';
import * as path from 'node:path';
import { tmpdir } from 'node:os';
import { codeBlocks, flatten, hash, parseWalkthrough, resolveSnippet, sourceRevision, Snippet } from '../src/model';
import { containedPath } from '../src/paths';
import { body, markdown } from '../src/render';
import { fixture } from './fixture';
import { sourceLink } from '../src/links';
import { createServer } from 'node:http';
import { fetchPublished, fetchStamp, publishedUrl, RemoteError } from '../src/remote';
import { CommentsClient, commentKey } from '../src/comments';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';

test('reference targets validate and render as escaped navigation cards', () => {
  const raw: any = fixture();
  raw.parts[0].sections[0].steps[0].blocks = [{ type: 'reference', title: '<script>bad</script>', description: '**Details**', target: { file: './next.json', stepId: 'next' } }];
  const doc = parseWalkthrough(JSON.stringify(raw));
  const output = body({ doc, entries: flatten(doc), index: 0, codeIndex: 0, done: new Set(), view: 'step', canReturn: true });
  assert.match(output, /data-action="reference"/);
  assert.match(output, /data-action="referenceBack"/);
  assert.match(output, /&lt;script&gt;/);
  for (const target of [{}, { file: '../bad.json' }, { file: './a/../bad.json' }, { url: 'javascript:alert(1)' }]) {
    raw.parts[0].sections[0].steps[0].blocks[0].target = target;
    assert.throws(() => parseWalkthrough(JSON.stringify(raw)));
  }
});

test('comments share the real CLI single-writer server, selection anchors, agent replies and archive state', async () => {
  const directory = await mkdtemp(path.join(tmpdir(), 'cw-comments-test-'));
  const file = path.join(directory, 'review.json');
  const text = JSON.stringify(fixture());
  await writeFile(file, text);
  const env = { ...process.env, APPDATA: directory, XDG_CONFIG_HOME: directory, HOME: directory };
  const settings = path.join(directory, ...(process.platform === 'darwin' ? ['Library', 'Application Support'] : []), 'code-walkthrough');
  const binary = path.resolve('dist/native', process.platform === 'win32' ? 'cw.exe' : 'cw');
  const owner = new CommentsClient(settings, env), other = new CommentsClient(settings, env);
  const probe = new CommentsClient(settings, env), recovered = new CommentsClient(settings, env);
  const key = commentKey(file, 'Review: source_é!');
  assert.equal(key, 'review-source');
  try {
    await owner.connect(binary, file, directory, 'Review: source_é!');
    await owner.add({ step: 'part/section/first', title: 'First', kind: 'code', block: 'a', file: 'src/a.ts', lines: { start: 2, end: 3 }, quote: 'export const a = 1;' }, 'Why this value?');
    await other.connect(binary, file, directory, 'Review: source_é!');
    let view = await other.view();
    assert.equal(view.threads.length, 1);
    assert.deepEqual(view.threads[0].where.lines, { start: 2, end: 3 });
    const cli = (args: string[]) => promisify(execFile)(binary, args, { env, windowsHide: true, timeout: 10000 });
    const claimed = JSON.parse((await cli(['comments', 'watch', key, '--for', '1s', '--json'])).stdout);
    assert.equal(claimed.comment.id, view.threads[0].id);
    assert.equal((await owner.view()).watching, true);
    await cli(['comments', 'reply', view.threads[0].id, '--text', 'It is the initial value.']);
    view = await owner.view();
    assert.equal(view.threads[0].status, 'answered');
    assert.equal(view.threads[0].messages.at(-1)?.from, 'agent');
    await other.reply(view.threads[0].id, 'What about the next call?');
    assert.equal((await owner.view()).threads[0].status, 'open');
    await other.archive(view.threads[0].id, true);
    assert.equal((await owner.view()).threads[0].archived, true);
    await other.archive(view.threads[0].id, false);
    other.dispose();
    assert.equal((await owner.view()).threads[0].archived, undefined, 'Closing a borrower leaves the shared server running');
    assert.equal(await readFile(file, 'utf8'), text, 'Comments never alter the walkthrough');
    const persisted = JSON.parse(await readFile(path.join(settings, 'comments', key + '.json'), 'utf8'));
    assert.equal(persisted.version, 'cw-comments/1');
    assert.equal(persisted.threads[0].messages.length, 3);
    await probe.connect(binary, file, directory, 'Review: source_é!');
    owner.dispose();
    const deadline = Date.now() + 3000;
    while (true) {
      try { await probe.view(); }
      catch { break; }
      assert.ok(Date.now() < deadline, 'Owned helper stops');
      await new Promise(resolve => setTimeout(resolve, 25));
    }
    await recovered.connect(binary, file, directory, 'Review: source_é!');
    const restored = (await recovered.view()).threads[0];
    assert.equal(restored.messages.length, 3);
    assert.deepEqual(restored.where.lines, { start: 2, end: 3 });
    assert.equal(restored.where.quote, 'export const a = 1;');
  } finally {
    recovered.dispose(); probe.dispose(); other.dispose(); owner.dispose();
    await rm(directory, { recursive: true, force: true });
  }
});

test('published walkthroughs support protected reads, updates and safe redirects', async () => {
  const requests: string[] = [];
  const server = createServer((req, res) => {
    requests.push(req.url!);
    if (req.url === '/foreign') { res.writeHead(302, { Location: 'http://localhost:1/stolen' }); res.end(); return; }
    if (req.url === '/redirect') { res.writeHead(302, { Location: '/api/v1/walkthroughs/example' }); res.end(); return; }
    if (req.url === '/invalid') { res.end('null'); return; }
    if (req.url === '/large') { res.writeHead(200, { 'Content-Length': String(9 * 1024 * 1024) }); res.end(); return; }
    if (req.url === '/denied') { res.writeHead(403); res.end(); return; }
    if (req.headers['x-cw-password'] !== 'test-password' && req.headers.authorization !== 'Bearer test-key') {
      res.writeHead(401); res.end(); return;
    }
    res.setHeader('Content-Type', 'application/json');
    res.end(JSON.stringify(req.url?.endsWith('/state') ? { stamp: 'second' } : { doc: fixture(), stamp: 'first', meta: { slug: 'example' } }));
  });
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  assert.ok(address && typeof address !== 'string');
  const base = `http://127.0.0.1:${address.port}`;
  try {
    const api = publishedUrl(base + '/w/example#part/section/step');
    assert.equal(api, base + '/api/v1/walkthroughs/example');
    assert.equal(publishedUrl('example', base), api);
    assert.throws(() => publishedUrl('file:///tmp/doc.json'));
    await assert.rejects(fetchPublished(api), (e: unknown) => e instanceof RemoteError && e.status === 401);
    const result = await fetchPublished(api, { kind: 'password', value: 'test-password' });
    assert.equal(result.doc.title, fixture().title);
    assert.equal(result.pageUrl, base + '/w/example');
    assert.equal(result.stamp, 'first');
    assert.equal(await fetchStamp(api, { kind: 'key', value: 'test-key' }), 'second');
    assert.equal((await fetchPublished(base + '/redirect', { kind: 'key', value: 'test-key' })).doc.title, fixture().title);
    await assert.rejects(fetchPublished(base + '/foreign', { kind: 'key', value: 'test-key' }), /another origin/);
    await assert.rejects(fetchPublished(base + '/invalid'), /walkthrough document/);
    await assert.rejects(fetchPublished(base + '/large'), /size limit/);
    await assert.rejects(fetchPublished(base + '/denied'), (e: unknown) => e instanceof RemoteError && e.status === 403);
    assert.ok(!requests.includes('/stolen'));
  } finally {
    server.closeAllConnections();
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
});

test('the real cw/2 tour exercises all seven blocks and passes semantic validation', async () => {
  const doc = parseWalkthrough(await readFile('../examples/cw2-tour.json', 'utf8'));
  assert.equal(new Set(flatten(doc).flatMap(e => e.step.blocks.map(b => b.type))).size, 7);
});
test('versions, duplicate ids, unsupported blocks and invalid anchors are rejected', () => {
  assert.throws(() => parseWalkthrough('{"version":"cw/1"}'), /migrate/);
  assert.throws(() => parseWalkthrough('{"version":"cw/3"}'), /Unsupported/);
  const raw: any = fixture();
  raw.parts[0].id = 'first';
  assert.throws(() => parseWalkthrough(JSON.stringify(raw)), /Duplicate/);
  raw.parts[0].id = 'part';
  raw.parts[0].sections[0].steps[0].blocks.push({ type: 'video', text: 'unknown' });
  assert.throws(() => parseWalkthrough(JSON.stringify(raw)), /Invalid walkthrough/);
  raw.parts[0].sections[0].steps[0].blocks.pop();
  raw.parts[0].sections[0].steps[0].blocks.push({ type: 'diagram', text: 'graph TD; A-->B', format: 'mermaid', alt: 'flow', links: [{ nodeId: 'A', blockId: 'first' }] });
  assert.throws(() => parseWalkthrough(JSON.stringify(raw)), /existing code block/);
});
test('forward-compatible optional fields and enum values are accepted', () => {
  const raw: any = fixture();
  raw.futureOptional = { example: true };
  raw.parts[0].sections[0].steps[0].blocks.push({ type: 'callout', text: 'Future severity', severity: 'notice' });
  assert.equal(parseWalkthrough(JSON.stringify(raw)).title, raw.title);
});
test('semantic ranges and content hashes are checked', () => {
  const raw: any = fixture(), snippet = raw.parts[0].sections[0].steps[0].blocks[1].snippet;
  snippet.highlights[0].lines.start = 3;
  assert.throws(() => parseWalkthrough(JSON.stringify(raw)), /outside/);
  snippet.highlights[0].lines.start = 2;
  snippet.hash = { algorithm: 'sha256', value: hash(snippet.text + 'changed') };
  assert.throws(() => parseWalkthrough(JSON.stringify(raw)), /hash/);
  snippet.hash.value = hash(snippet.text);
  assert.doesNotThrow(() => parseWalkthrough(JSON.stringify(raw)));
  snippet.source.endLine = 4;
  assert.throws(() => parseWalkthrough(JSON.stringify(raw)), /source range/);
});
test('snippet resolution finds moved whole lines and refuses ambiguous or partial matches', () => {
  const sn: Snippet = { text: 'alpha\nbeta\n', source: { file: 'a.ts', startLine: 2 } };
  assert.deepEqual(resolveSnippet('prefix\r\nalpha\r\nbeta\r\n', sn), { state: 'match', startLine: 2, endLine: 3 });
  assert.deepEqual(resolveSnippet('extra\nprefix\nalpha\nbeta\n', sn), { state: 'moved', startLine: 3, endLine: 4 });
  assert.equal(resolveSnippet('alpha\nbeta\nalpha\nbeta\n', sn).state, 'ambiguous');
  assert.equal(resolveSnippet('prefix-alpha\nbeta\n', sn).state, 'different');
  assert.equal(resolveSnippet('prefix\nalpha\nbeta changed\n', sn).state, 'different');
  assert.equal(resolveSnippet('alpha\nbeta', sn).state, 'different');
  assert.equal(resolveSnippet('alpha\nbeta', { text: 'alpha\nbeta' }).state, 'moved');
  assert.notEqual(hash('a\n'), hash('a'));
  assert.equal(hash('a\r\n'), hash('a\n'));
});
test('original exact match wins over duplicate text elsewhere', () => {
  assert.deepEqual(resolveSnippet('x\nx\n', { text: 'x\n', source: { file: 'a', startLine: 2 } }), { state: 'match', startLine: 2, endLine: 2 });
});
test('document order and stable keys survive inserted steps', () => {
  const raw: any = fixture();
  const before = flatten(parseWalkthrough(JSON.stringify(raw)));
  raw.parts[0].sections[0].steps.unshift({ id: 'new', title: 'Inserted', blocks: [{ type: 'markdown', text: 'New content' }] });
  const after = flatten(parseWalkthrough(JSON.stringify(raw)));
  assert.equal(after[1].key, before[0].key);
  assert.equal(codeBlocks(after[1].step).length, 2);
});
test('a different repository does not inherit a revision', () => {
  const doc = parseWalkthrough(JSON.stringify(fixture()));
  doc.source = { repositoryUrl: 'https://github.com/a/b', revision: 'abc' };
  assert.equal(sourceRevision(doc, { file: 'a' }), 'abc');
  assert.equal(sourceRevision(doc, { file: 'a', repositoryUrl: 'https://github.com/c/d' }), undefined);
  assert.equal(sourceRevision(doc, { file: 'a', repositoryUrl: 'https://github.com/c/d', revision: 'def' }), 'def');
});
test('source links keep explicit locations, encode paths and respect repository revision inheritance', () => {
  const doc = parseWalkthrough(JSON.stringify(fixture()));
  doc.source = { repositoryUrl: 'https://github.com/a/b.git', revision: 'feature/a' };
  assert.equal(sourceLink(doc, { file: 'dir with spaces/a.ts', startLine: 3, endLine: 7 }), 'https://github.com/a/b/blob/feature%2Fa/dir%20with%20spaces/a.ts#L3-L7');
  assert.equal(sourceLink(doc, { file: 'a', repositoryUrl: 'https://github.com/c/d' }), 'https://github.com/c/d');
  assert.equal(sourceLink(doc, { file: 'a', url: 'https://example.com/custom' }), 'https://example.com/custom');
  doc.source = { repositoryUrl: 'https://github.com/a/b', url: 'https://github.com/a/b/pull/12', revision: 'head', comparison: { baseRevision: 'base', headRevision: 'head' }, changedFiles: [{ file: 'new.ts', previousFile: 'old.ts', status: 'renamed' }] };
  assert.equal(sourceLink(doc, { file: 'new.ts', startLine: 4 }), `https://github.com/a/b/pull/12/files#diff-${hash('new.ts')}R4`);
  assert.equal(sourceLink(doc, { file: 'old.ts', revision: 'base', startLine: 8 }), `https://github.com/a/b/pull/12/files#diff-${hash('new.ts')}L8`);
  assert.equal(sourceLink(doc, { file: 'new.ts', revision: 'another', startLine: 1 }), 'https://github.com/a/b/blob/another/new.ts#L1');
});
test('raw HTML, scripts, command links and remote images never become executable content', () => {
  const output = markdown('<script>alert(1)</script>\n\n[bad](command:workbench.action.files.save) ![alt](https://example.com/image.png)\n\n[safe](https://example.com)');
  assert.ok(!output.includes('<script>'));
  assert.ok(!output.includes('href="command:'));
  assert.ok(!output.includes('<img'));
  assert.match(output, /href="https:\/\/example.com"/);
  assert.match(output, /&lt;script&gt;/);
});
test('all blocks remain readable, including extension fallback and multi-location navigation', async () => {
  const doc = parseWalkthrough(await readFile('../examples/cw2-tour.json', 'utf8'));
  const entries = flatten(doc);
  for (let index = 0; index < entries.length; index++) {
    const rendered = body({ doc, entries, index, codeIndex: 0, done: new Set() });
    assert.ok(rendered.includes('<article>'));
    assert.ok(!rendered.includes('undefined'));
  }
  const fixtureDoc = parseWalkthrough(JSON.stringify(fixture()));
  const output = body({ doc: fixtureDoc, entries: flatten(fixtureDoc), index: 0, codeIndex: 0, done: new Set() });
  assert.match(output, /Code 1 of 2/);
  assert.match(output, /data-action="code" data-value="1"/);
});
test('source path containment rejects traversal, absolute paths and symlink escapes', async () => {
  const temp = await mkdtemp(path.join(tmpdir(), 'cw-paths-'));
  try {
    const root = path.join(temp, 'repo'), outside = path.join(temp, 'outside');
    await mkdir(root); await mkdir(outside);
    await writeFile(path.join(root, 'ok.ts'), 'ok');
    await writeFile(path.join(outside, 'secret.ts'), 'outside');
    await symlink(outside, path.join(root, 'escape'), process.platform === 'win32' ? 'junction' : 'dir');
    assert.equal(await containedPath(root, 'ok.ts'), path.join(root, 'ok.ts'));
    for (const file of ['../outside/secret.ts', '/outside/secret.ts', 'C:/outside/secret.ts', 'escape/secret.ts', 'a\\b.ts'])
      await assert.rejects(containedPath(root, file));
  } finally {
    if (path.dirname(path.resolve(temp)) !== path.resolve(tmpdir()) || !path.basename(temp).startsWith('cw-paths-'))
      throw new Error('Refusing to remove a path outside the test temporary directory.');
    await rm(temp, { recursive: true, force: true });
  }
});
test('diff counts, overlaps and end-of-file markers are semantically checked', () => {
  const raw: any = fixture();
  const diff: any = { type: 'diff', before: { file: 'a' }, after: { file: 'a' }, hunks: [
    { oldStart: 1, oldLines: 1, newStart: 1, newLines: 1, lines: [{ kind: 'delete', text: 'old' }, { kind: 'add', text: 'new' }] },
  ] };
  raw.parts[0].sections[0].steps[0].blocks.push(diff);
  assert.doesNotThrow(() => parseWalkthrough(JSON.stringify(raw)));
  diff.hunks[0].newLines = 2;
  assert.throws(() => parseWalkthrough(JSON.stringify(raw)), /counts/);
  diff.hunks[0].newLines = 1;
  diff.hunks.push(structuredClone(diff.hunks[0]));
  assert.throws(() => parseWalkthrough(JSON.stringify(raw)), /overlap/);
  diff.hunks[1].oldStart = 3; diff.hunks[1].newStart = 3;
  diff.hunks[0].lines[0].noNewlineAtEnd = true;
  assert.throws(() => parseWalkthrough(JSON.stringify(raw)), /last line/);
});
test('timeline frames must contain every node exactly once', () => {
  const raw: any = fixture();
  const timeline: any = { type: 'timeline', nodes: [{ id: 'node', label: 'A' }], frames: [{ label: 'Start', states: [{ nodeId: 'node', state: 'active' }] }] };
  raw.parts[0].sections[0].steps[0].blocks.push(timeline);
  assert.doesNotThrow(() => parseWalkthrough(JSON.stringify(raw)));
  timeline.frames[0].states.push({ nodeId: 'node', state: 'idle' });
  assert.throws(() => parseWalkthrough(JSON.stringify(raw)), /every node exactly once/);
});
