import { parseWalkthrough, Walkthrough } from './model';

export interface Credential { kind: 'password' | 'key'; value: string }
export interface Published { doc: Walkthrough; text: string; apiUrl: string; pageUrl: string; stamp?: string; slug?: string }
export class RemoteError extends Error {
  constructor(message: string, readonly status?: number) { super(message); }
}
export function publishedUrl(target: string, site = 'https://cw.roesink.dev'): string {
  target = target.trim();
  if (/^[a-z0-9][a-z0-9-]{0,63}$/.test(target)) target = site.replace(/\/$/, '') + '/api/v1/walkthroughs/' + target;
  const url = new URL(target);
  if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password) throw new Error('Use an HTTP(S) walkthrough URL without credentials in the address.');
  url.hash = '';
  const match = url.pathname.match(/^(.*?)\/w\/([a-z0-9][a-z0-9-]{0,63})\/?$/);
  if (match) url.pathname = match[1] + '/api/v1/walkthroughs/' + match[2];
  url.pathname = url.pathname.replace(/\/$/, '');
  return url.toString();
}
async function request(url: string, credential?: Credential): Promise<Response> {
  const origin = new URL(url).origin;
  const headers: Record<string, string> = { Accept: 'application/json' };
  if (credential) headers[credential.kind === 'password' ? 'X-Cw-Password' : 'Authorization'] = credential.kind === 'password' ? credential.value : 'Bearer ' + credential.value;
  const signal = AbortSignal.timeout(15000);
  for (let i = 0; i < 5; i++) {
    const response = await fetch(url, { headers, redirect: 'manual', signal });
    if (response.status >= 300 && response.status < 400 && response.headers.has('location')) {
      const next = new URL(response.headers.get('location')!, url);
      await response.body?.cancel();
      if (next.origin !== origin || next.username || next.password) throw new RemoteError('The site redirected to another origin. Open the destination URL explicitly; credentials were not forwarded.');
      url = next.toString(); continue;
    }
    if (!response.ok) {
      await response.body?.cancel();
      const explanation = response.status === 401 ? 'This walkthrough requires a password or publishing key.'
        : response.status === 403 ? 'This site does not allow your address to read the walkthrough.'
        : response.status === 404 ? 'The walkthrough was not found.' : `The walkthrough site returned HTTP ${response.status}.`;
      throw new RemoteError(explanation, response.status);
    }
    return response;
  }
  throw new RemoteError('The site redirected too many times.');
}
async function json(response: Response): Promise<any> {
  const maximum = 8 * 1024 * 1024;
  if (Number(response.headers.get('content-length')) > maximum) {
    await response.body?.cancel(); throw new RemoteError('The walkthrough exceeds the 8 MB size limit.');
  }
  const reader = response.body?.getReader();
  if (!reader) throw new RemoteError('The site returned no document.');
  let size = 0;
  const buffers: Uint8Array[] = [];
  try {
    while (true) {
      const chunk = await reader.read();
      if (chunk.done) break;
      size += chunk.value.length;
      if (size > maximum) throw new RemoteError('The walkthrough exceeds the 8 MB size limit.');
      buffers.push(chunk.value);
    }
  } finally { await reader.cancel(); }
  try { return JSON.parse(Buffer.concat(buffers).toString('utf8')); }
  catch { throw new RemoteError('The site did not return walkthrough JSON. Paste its /w/name or API URL.'); }
}
export async function fetchPublished(target: string, credential?: Credential): Promise<Published> {
  const apiUrl = publishedUrl(target);
  const payload = await json(await request(apiUrl, credential));
  if (!payload || typeof payload !== 'object') throw new RemoteError('The site did not return a walkthrough document.');
  const text = JSON.stringify(payload.doc ?? payload, null, 2);
  const doc = parseWalkthrough(text);
  const pageUrl = apiUrl.replace(/\/api\/v1\/walkthroughs\/([a-z0-9-]+)(?:\?.*)?$/, '/w/$1');
  return { doc, text, apiUrl, pageUrl, stamp: typeof payload.stamp === 'string' ? payload.stamp : undefined, slug: typeof payload.meta?.slug === 'string' ? payload.meta.slug : undefined };
}
export async function fetchStamp(apiUrl: string, credential?: Credential): Promise<string | undefined> {
  const url = new URL(apiUrl);
  if (!/\/api\/v1\/walkthroughs\/[a-z0-9-]+$/.test(url.pathname)) return undefined;
  url.pathname += '/state';
  const payload = await json(await request(url.toString(), credential));
  return typeof payload?.stamp === 'string' ? payload.stamp : undefined;
}
