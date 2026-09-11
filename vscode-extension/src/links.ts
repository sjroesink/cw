import { Location, Walkthrough, sourceRevision, repositoryKey } from './model';
import { createHash } from 'node:crypto';

export function sourceLink(doc: Walkthrough, location: Location): string | undefined {
  if (location.url && /^https?:\/\//i.test(location.url)) return location.url;
  const repository = location.repositoryUrl ?? doc.source?.repositoryUrl;
  if (!repository) return undefined;
  let url: URL;
  try { url = new URL(repository); } catch { return undefined; }
  const revision = sourceRevision(doc, location);
  const file = location.file.split('/').map(encodeURIComponent).join('/');
  const base = repository.replace(/\/$/, '').replace(/\.git$/, '');
  const source = doc.source;
  const pr = source?.url?.match(/^https:\/\/github\.com\/([^/]+\/[^/]+)\/pull\/(\d+)(?:\/[^?#]*)?$/);
  const sameRepository = repositoryKey(repository) === repositoryKey(source?.repositoryUrl);
  const changed = source?.changedFiles?.find(f => f.file === location.file || f.previousFile === location.file);
  const head = source?.comparison?.headRevision ?? source?.revision;
  const old = !!location.revision && location.revision === source?.comparison?.baseRevision && location.revision !== head;
  if (pr && sameRepository && changed && (!location.revision || location.revision === head || old)) {
    const anchor = createHash('sha256').update(changed.file).digest('hex');
    const side = old || changed.status === 'deleted' ? 'L' : 'R';
    return `https://github.com/${pr[1]}/pull/${pr[2]}/files#diff-${anchor}${location.startLine ? side + location.startLine : ''}`;
  }
  if (!revision) return base;
  const line = location.startLine ? `#L${location.startLine}${location.endLine && location.endLine !== location.startLine ? '-L' + location.endLine : ''}` : '';
  if (url.hostname.toLowerCase() === 'github.com') return `${base}/blob/${encodeURIComponent(revision)}/${file}${line}`;
  if (url.hostname.toLowerCase() === 'gitlab.com') return `${base}/-/blob/${encodeURIComponent(revision)}/${file}${line.replace('-L', '-')}`;
  return base;
}
