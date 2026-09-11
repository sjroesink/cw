import Ajv from 'ajv/dist/2020';
import addFormats from 'ajv-formats';
import { createHash } from 'node:crypto';
import schema from '../../schema/walkthrough.v2.schema.json';

export interface Location { file: string; repositoryUrl?: string; revision?: string; startLine?: number; endLine?: number; url?: string }
export interface Lines { start: number; end?: number }
export interface Snippet {
  text: string; label?: string; language?: string; source?: Location;
  hash?: { algorithm: string; value: string };
  highlights?: { lines: Lines; kind?: string }[];
  annotations?: { lines: Lines; text: string }[];
}
interface BaseBlock { id?: string }
export type CodeBlock = BaseBlock & { type: 'code'; snippet: Snippet };
export type Block = CodeBlock
  | BaseBlock & { type: 'markdown'; text: string }
  | BaseBlock & { type: 'callout'; text: string; title?: string; severity?: string }
  | BaseBlock & { type: 'diagram'; format: string; text: string; alt: string; caption?: string; links?: { nodeId: string; blockId: string }[] }
  | BaseBlock & { type: 'diff'; before?: Location; after?: Location; caption?: string; language?: string; hunks: Hunk[] }
  | BaseBlock & { type: 'timeline'; caption?: string; nodes: { id: string; label: string }[]; frames: { label: string; note?: string; durationMs?: number; states: { nodeId: string; state: string; detail?: string }[] }[] }
  | BaseBlock & { type: 'extension'; name: string; version: number; data: object; fallback: string }
  | BaseBlock & { type: 'reference'; title: string; description?: string; relation?: string; target: { file?: string; url?: string; stepId?: string } };
export interface Hunk { oldStart: number; oldLines: number; newStart: number; newLines: number; heading?: string; lines: { kind: string; text: string; noNewlineAtEnd?: boolean }[] }
export interface Step { id: string; title: string; blocks: Block[] }
export interface Section { id: string; title: string; summary?: string; steps: Step[] }
export interface Part { id: string; title: string; summary?: string; description?: string; files?: string[]; sections: Section[] }
export interface Walkthrough {
  version: 'cw/2'; title: string; summary?: string; language?: string;
  source?: { repositoryUrl?: string; revision?: string; comparison?: { baseRevision: string; headRevision: string }; label?: string; identifier?: string; state?: string; url?: string; changedFiles?: { file: string; status: string; previousFile?: string }[] };
  parts: Part[];
}
export interface Entry { part: Part; section: Section; step: Step; key: string }

// The bundled schema remains strict for authors. Readers allow future optional
// properties and enum values, as promised by cw/2, without accepting new block types.
function readerSchema(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(readerSchema);
  if (value && typeof value === 'object') return Object.fromEntries(Object.entries(value)
    .filter(([key, val]) => key !== 'enum' && !(key === 'additionalProperties' && val === false))
    .map(([key, val]) => [key, readerSchema(val)]));
  return value;
}
const ajv = new Ajv({ allErrors: true, strict: false });
addFormats(ajv);
const validate = ajv.compile(readerSchema(schema) as object);
const validateReplyBlocks = ajv.compile({ $defs: (readerSchema(schema) as any).$defs, type: 'array', items: { $ref: '#/$defs/block' } });
export function replyBlocks(value: unknown): Block[] {
  return validateReplyBlocks(value) ? value as Block[] : [];
}
export const normalize = (text: string) => text.replace(/\r\n/g, '\n');
export const lineCount = (text: string) => normalize(text).replace(/\n$/, '').split('\n').length;
export const hash = (text: string) => createHash('sha256').update(normalize(text), 'utf8').digest('hex');
export const flatten = (doc: Walkthrough): Entry[] => doc.parts.flatMap(part => part.sections.flatMap(section =>
  section.steps.map(step => ({ part, section, step, key: `${part.id}/${section.id}/${step.id}` }))));
export const codeBlocks = (step: Step): CodeBlock[] => step.blocks.filter((b): b is CodeBlock => b.type === 'code' && !!b.snippet.source);

export function parseWalkthrough(text: string): Walkthrough {
  let raw: unknown;
  try { raw = JSON.parse(text.replace(/^\uFEFF/, '')); } catch { throw new Error('This file is not valid JSON.'); }
  const version = (raw as { version?: unknown } | null)?.version;
  if (version !== 'cw/2') throw new Error(version === 'cw/1'
    ? 'This extension reads cw/2. Convert a copy with: cw migrate <file> --to cw/2'
    : `Unsupported walkthrough version: ${String(version ?? '(missing)')}. Expected cw/2.`);
  if (!validate(raw)) {
    // Select the relevant union branch to avoid burying the useful error.
    const errors = (validate.errors ?? []).filter(e => !['const', 'oneOf'].includes(e.keyword));
    throw new Error('Invalid walkthrough: ' + (errors.length ? errors : validate.errors!).slice(0, 5)
      .map(e => `${e.instancePath || '/'} ${e.message}`).join('; '));
  }
  const doc = raw as Walkthrough;
  const ids = new Set<string>();
  const blocks = new Map<string, Block>();
  const claim = (id: string | undefined) => {
    if (!id) return;
    if (ids.has(id)) throw new Error(`Duplicate id: ${id}`);
    ids.add(id);
  };
  for (const p of doc.parts) {
    claim(p.id);
    for (const s of p.sections) {
      claim(s.id);
      for (const step of s.steps) {
        claim(step.id);
        for (const b of step.blocks) {
          claim(b.id);
          if (b.id) blocks.set(b.id, b);
          if (b.type === 'code') {
            const sn = b.snippet, n = lineCount(sn.text), src = sn.source;
            if (sn.hash && sn.hash.value !== hash(sn.text)) throw new Error(`Step ${step.id}: snippet hash does not match its text.`);
            if (src?.endLine && src.endLine - src.startLine! + 1 !== n) throw new Error(`Step ${step.id}: source range does not match snippet length.`);
            for (const item of [...sn.highlights ?? [], ...sn.annotations ?? []]) {
              if (item.lines.start < 1 || (item.lines.end ?? item.lines.start) < item.lines.start || (item.lines.end ?? item.lines.start) > n)
                throw new Error(`Step ${step.id}: highlight or annotation is outside its snippet.`);
            }
          }
          if (b.type === 'diff') checkHunks(b.hunks);
          if (b.type === 'timeline') {
            const nodes = new Set(b.nodes.map(n => n.id));
            if (nodes.size !== b.nodes.length) throw new Error(`Step ${step.id}: duplicate timeline node.`);
            for (const frame of b.frames) {
              const states = new Set(frame.states.map(s => s.nodeId));
              if (states.size !== nodes.size || states.size !== frame.states.length || [...states].some(id => !nodes.has(id)))
                throw new Error(`Step ${step.id}: each timeline frame must contain every node exactly once.`);
            }
          }
        }
      }
    }
  }
  for (const { step } of flatten(doc)) for (const b of step.blocks) {
    if (b.type === 'diagram') for (const link of b.links ?? []) {
      if (blocks.get(link.blockId)?.type !== 'code') throw new Error(`Diagram link ${link.blockId} must point to an existing code block.`);
    }
  }
  return doc;
}

function checkHunks(hunks: Hunk[]) {
  let oldEnd = 0, newEnd = 0;
  const all = hunks.flatMap(h => h.lines);
  const lastOld = all.findLastIndex(l => l.kind !== 'add');
  const lastNew = all.findLastIndex(l => l.kind !== 'delete');
  for (const [i, l] of all.entries()) if (l.noNewlineAtEnd &&
    ((l.kind !== 'add' && i !== lastOld) || (l.kind !== 'delete' && i !== lastNew))) throw new Error('Diff newline marker must be on the last line of its side.');
  for (const h of hunks) {
    if (h.oldLines !== h.lines.filter(l => l.kind !== 'add').length || h.newLines !== h.lines.filter(l => l.kind !== 'delete').length)
      throw new Error('Diff hunk counts do not match its lines.');
    if ((h.oldLines ? h.oldStart <= oldEnd : h.oldStart < oldEnd) || (h.newLines ? h.newStart <= newEnd : h.newStart < newEnd))
      throw new Error('Diff hunks overlap or are out of order.');
    oldEnd = h.oldStart + Math.max(0, h.oldLines - 1);
    newEnd = h.newStart + Math.max(0, h.newLines - 1);
  }
}

export interface Resolution { state: 'match' | 'moved' | 'different' | 'ambiguous'; startLine?: number; endLine?: number }
export function resolveSnippet(fileText: string, snippet: Snippet): Resolution {
  const text = normalize(fileText), wanted = normalize(snippet.text), n = lineCount(wanted);
  const starts = [0];
  for (let i = 0; i < text.length; i++) if (text[i] === '\n' && i + 1 < text.length) starts.push(i + 1);
  const matches = (i: number) => {
    const start = starts[i];
    if (start === undefined || i + n > starts.length) return false;
    const end = starts[i + n] ?? text.length;
    return text.slice(start, end) === wanted;
  };
  const original = snippet.source?.startLine;
  if (original && matches(original - 1)) return { state: 'match', startLine: original, endLine: original + n - 1 };
  let found: number | undefined;
  for (let i = 0; i < starts.length; i++) {
    if (!matches(i)) continue;
    if (found !== undefined) return { state: 'ambiguous' };
    found = i + 1;
  }
  return found === undefined ? { state: 'different' } : { state: 'moved', startLine: found, endLine: found + n - 1 };
}

export function repositoryKey(url?: string) { return (url ?? '').replace(/\/$/, '').replace(/\.git$/, ''); }
export function sourceRevision(doc: Walkthrough, location: Location): string | undefined {
  if (location.revision) return location.revision;
  if (location.repositoryUrl && repositoryKey(location.repositoryUrl) !== repositoryKey(doc.source?.repositoryUrl)) return undefined;
  return doc.source?.revision ?? doc.source?.comparison?.headRevision;
}
