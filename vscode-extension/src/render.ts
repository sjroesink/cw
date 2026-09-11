import MarkdownIt from 'markdown-it';
import { Block, Entry, Snippet, Walkthrough, codeBlocks, sourceRevision, replyBlocks } from './model';
import { sourceLink } from './links';
import type { CommentsView } from './comments';

export const escape = (text: unknown): string => String(text ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]!));
const md = new MarkdownIt({ html: false, linkify: false, typographer: false });
// A walkthrough never fetches images or executes HTML. Ordinary web links are
// opened by the host, after validating the scheme there as well.
md.renderer.rules.image = (tokens, i) => `<span>${escape(tokens[i].content)}</span>`;
md.validateLink = url => /^https?:\/\//i.test(url);
const originalHeading = md.renderer.rules.heading_open;
md.renderer.rules.heading_open = (tokens, i, options, env, self) => {
  tokens[i].tag = 'h' + Math.min(6, Number(tokens[i].tag.slice(1)) + 2);
  return originalHeading ? originalHeading(tokens, i, options, env, self) : self.renderToken(tokens, i, options);
};
md.renderer.rules.heading_close = (tokens, i, options, _env, self) => {
  tokens[i].tag = 'h' + Math.min(6, Number(tokens[i].tag.slice(1)) + 2);
  return self.renderToken(tokens, i, options);
};
export const markdown = (text?: string) => text ? md.render(text) : '';
export interface Reading {
  doc: Walkthrough; entries: Entry[]; index: number; codeIndex: number; done: Set<string>;
  status?: string; root?: string; error?: string; focusBlock?: string; identity?: string;
  view?: 'overview' | 'part' | 'step'; partId?: string;
  publishedUrl?: string; remoteNotice?: string;
  comments?: CommentsView; commentsOpen?: boolean; commentsError?: string;
  snippetChecks?: Record<string, string>;
  commentFocus?: string;
  canReturn?: boolean;
}
function toolbar(r: Reading) {
  return `<nav class="actions reading-tools" aria-label="Reading tools">${r.canReturn ? button('referenceBack', '← Back to previous walkthrough', undefined, false, true) : ''}${button('comments', 'Comments', undefined, false, true)}${button('copyLink', 'Copy page link', undefined, false, true)}${button('theme', 'Theme', undefined, false, true)}${button('settings', 'Settings', undefined, false, true)}</nav>`;
}
function button(action: string, label: string, value?: string | number, disabled = false, secondary = false) {
  return `<button data-action="${action}"${value === undefined ? '' : ` data-value="${escape(value)}"`}${disabled ? ' disabled' : ''}${secondary ? ' class="secondary"' : ''}>${escape(label)}</button>`;
}
function snippetBody(sn: Snippet, blockIndex: number, language?: string) {
  const lines = sn.text.replace(/\r\n/g, '\n').replace(/\n$/, '').split('\n');
  return `<pre class="snippet" data-highlight="${escape(sn.language ?? language ?? '')}" data-file="${escape(sn.source?.file)}"><code>${lines.map((text, i) => {
    const line = i + 1;
    const h = sn.highlights?.find(h => line >= h.lines.start && line <= (h.lines.end ?? h.lines.start));
    const cls = h ? (h.kind === 'added' ? 'added' : 'focus') : '';
    const notes = (sn.annotations ?? []).map((a, index) => ({ a, index })).filter(({ a }) => line >= a.lines.start && line <= (a.lines.end ?? a.lines.start));
    const shown = sn.source?.startLine ? sn.source.startLine + i : line;
    return `<span class="code-line ${cls}">${sn.source ? `<button class="line-number" data-action="line" data-value="${blockIndex}:${line}" title="Open file line ${shown}">${shown}</button>` : `<span class="line-number">${shown}</span>`}<span class="code-text">${escape(text)}</span>${notes.map(({ index }) => `<button class="note-marker" data-note="${blockIndex}:${index}" aria-expanded="false" aria-controls="note-${blockIndex}-${index}" aria-label="Read annotation ${index + 1}" title="Read annotation ${index + 1}">●</button>`).join('')}</span>`;
  }).join('')}</code></pre>${(sn.annotations ?? []).map((a, index) => `<div class="annotation" id="note-${blockIndex}-${index}" data-annotation="${blockIndex}:${index}"><span class="muted">Snippet ${a.lines.start === (a.lines.end ?? a.lines.start) ? 'line' : 'lines'} ${a.lines.start}${a.lines.end && a.lines.end !== a.lines.start ? '–' + a.lines.end : ''}</span>${markdown(a.text)}</div>`).join('')}`;
}
function renderBlock(b: Block, reading: Reading, blockIndex: number, reply = false): string {
  const entry = reading.entries[reading.index];
  switch (b.type) {
    case 'markdown': return markdown(b.text);
    case 'callout': return `<aside class="callout ${['info', 'tip', 'warning', 'danger'].includes(b.severity ?? '') ? b.severity : 'info'}">${b.title ? `<strong>${escape(b.title)}</strong>` : ''}${markdown(b.text)}</aside>`;
    case 'code': {
      const i = codeBlocks(entry.step).indexOf(b);
      const sn = b.snippet;
      const revision = sn.source && sourceRevision(reading.doc, sn.source);
      const link = sn.source && sourceLink(reading.doc, sn.source);
      return `<section id="block-${blockIndex}" data-block-id="${escape(b.id)}" class="code-block ${(i >= 0 && i === reading.codeIndex) || (b.id && b.id === reading.focusBlock) ? 'active-code' : ''}">
        ${sn.label ? `<h3>${escape(sn.label)}</h3>` : ''}
        ${sn.source ? `<div class="file-label">${escape(sn.source.file)}${sn.source.startLine ? ':' + sn.source.startLine : ''}</div>
        ${revision ? `<div class="muted">Written against ${escape(revision)}</div>` : ''}
        ${reading.snippetChecks?.[String(blockIndex)] ? `<p class="snippet-status" role="status">${escape(reading.snippetChecks[String(blockIndex)])}</p>` : ''}
        ${button(reply ? 'replyCode' : 'code', !reply && i === reading.codeIndex ? 'Show active code' : 'Show code', reply ? blockIndex : i, false, true)}` : '<div class="muted">Illustrative example</div>'}
        ${link ? `<p><a href="${escape(link)}">View source ↗</a></p>${button('copySource', 'Copy source link', blockIndex, false, true)}` : ''}
        <div class="actions">${button('copy', 'Copy code', blockIndex, false, true)}</div>
        ${snippetBody(sn, blockIndex, reading.doc.language)}</section>`;
    }
    case 'diagram': return `<figure id="block-${blockIndex}" class="diagram-block" data-diagram="${blockIndex}">
      <div class="actions"><button class="secondary" data-zoom>Full size</button>${button('copy', 'Copy Mermaid', blockIndex, false, true)}</div>
      <div class="diagram-canvas" role="group" aria-label="${escape(b.alt)}"></div>
      <p class="diagram-status" role="status">Drawing diagram…</p>
      <details class="diagram-source"><summary>Mermaid source</summary><pre><code>${escape(b.text)}</code></pre></details>${markdown(b.caption)}<p>${escape(b.alt)}</p>
      <div class="actions diagram-links">${(b.links ?? []).map(l => `<button class="secondary" data-action="block" data-value="${escape(l.blockId)}" data-node="${escape(l.nodeId)}">${escape(l.nodeId)}</button>`).join('')}</div></figure>`;
    case 'diff': return `<figure><div class="file-label">${escape(b.before?.file ?? '(new file)')} → ${escape(b.after?.file ?? '(deleted file)')}</div>${markdown(b.caption)}
      <div class="actions">${b.after ? button('diffLine', 'Open file', `${blockIndex}:1`, false, true) : ''}${button('copy', 'Copy diff', blockIndex, false, true)}</div>
      ${b.hunks.length ? b.hunks.map(h => {
        let old = h.oldStart, current = h.newStart;
        return `<div class="hunk-heading">@@ -${h.oldStart},${h.oldLines} +${h.newStart},${h.newLines} @@ ${escape(h.heading)}</div><pre class="diff" data-highlight="${escape(b.language ?? reading.doc.language ?? '')}" data-file="${escape(b.after?.file ?? b.before?.file)}"><code>` + h.lines.map(l => {
          const oldLine = l.kind === 'add' ? '' : String(old++);
          const newLine = l.kind === 'delete' ? '' : String(current++);
          return `<span class="code-line ${l.kind === 'add' ? 'added' : l.kind === 'delete' ? 'deleted' : ''}"><span class="line-number old" aria-label="Old line ${oldLine}">${oldLine}</span>${newLine && b.after ? `<button class="line-number new" data-action="diffLine" data-value="${blockIndex}:${newLine}" aria-label="New line ${newLine}">${newLine}</button>` : '<span class="line-number new"></span>'}<span class="diff-mark">${l.kind === 'add' ? '+' : l.kind === 'delete' ? '-' : ' '}</span><span class="code-text">${escape(l.text)}</span></span>${l.noNewlineAtEnd ? '<span class="code-line muted">\\ No newline at end of file</span>' : ''}`;
        }).join('') + '</code></pre>';
      }).join('') : '<p class="muted">No lines changed; only file metadata changed.</p>'}</figure>`;
    case 'timeline': return `<figure class="timeline" data-timeline="${blockIndex}">${markdown(b.caption)}
      <div class="actions"><button class="secondary" data-play aria-label="Play timeline">Play</button><button class="secondary" data-frame-back aria-label="Previous frame">←</button><span class="frame-label" aria-live="polite"></span><button class="secondary" data-frame-next aria-label="Next frame">→</button></div>
      <div class="frame-ticks">${b.frames.map((f, i) => `<button class="secondary" data-frame="${i}" aria-label="Frame ${i + 1}: ${escape(f.label)}">${i + 1}</button>`).join('')}</div>
      <div class="timeline-nodes">${b.nodes.map(n => `<div class="timeline-node" data-node-id="${escape(n.id)}"><strong>${escape(n.label)}</strong><span class="node-state"></span><span class="node-detail"></span></div>`).join('')}</div><div class="frame-note"></div>
      <details class="timeline-transcript"><summary>All frames</summary>${b.frames.map(f => `<section class="frame" data-duration="${f.durationMs ?? 2200}" data-label="${escape(f.label)}"><h3>${escape(f.label)}</h3><div class="frame-prose">${markdown(f.note)}</div><ul>${f.states.map(s => `<li data-node-id="${escape(s.nodeId)}" data-state="${escape(s.state)}" data-detail="${escape(s.detail)}"><strong>${escape(b.nodes.find(n => n.id === s.nodeId)?.label)}</strong> — ${escape(s.state)}${s.detail ? ': ' + escape(s.detail) : ''}</li>`).join('')}</ul></section>`).join('')}</details></figure>`;
    case 'reference': return `<aside class="reference"><p class="muted">${escape(['related', 'deep-dive', 'prerequisite', 'next'].includes(b.relation ?? '') ? b.relation : 'related')}</p><h3>${escape(b.title)}</h3>${markdown(b.description)}${button('reference', 'Open walkthrough →', blockIndex)}</aside>`;
    case 'extension': return markdown(b.fallback);
  }
}

export function body(reading?: Reading): string {
  if (!reading) return `<main><h1>Code Walkthrough</h1><p>Open a cw/2 JSON file to read its explanation beside your code.</p><div class="actions">${button('open', 'Open walkthrough…')}${button('published', 'Open published…', undefined, false, true)}</div><p class="muted">Choose a step in Contents. The editor follows the code; your place is remembered.</p></main>`;
  const r = reading, e = r.entries[r.index], codes = codeBlocks(e.step);
  if (r.view === 'overview' || r.view === 'part') return overview(r);
  return `<main data-address="${escape(e.key)}" data-focus-comment="${escape(r.commentFocus)}" data-page="${escape((r.identity ?? r.doc.title) + '/' + e.key)}"><header><p class="eyebrow">${escape(r.doc.title)}</p><p class="breadcrumb">${escape(e.part.title)} / ${escape(e.section.title)}</p>
    <nav class="actions">${button('overview', 'Overview', undefined, false, true)}${button('part', e.part.title, e.part.id, false, true)}</nav>
    ${toolbar(r)}${publishedBanner(r)}
    <div class="progress"><span>Step ${r.index + 1} of ${r.entries.length}</span><span>${r.done.size} completed</span></div>
    <progress value="${r.done.size}" max="${r.entries.length}" aria-label="Completed steps"></progress>
    </header>
    ${r.error ? `<div role="alert" class="callout danger">${escape(r.error)}<p>The last valid version is shown.</p></div>` : ''}
    <details class="context"><summary>About this walkthrough and section</summary>${markdown(r.doc.summary)}${markdown(e.part.description ?? e.part.summary)}${markdown(e.section.summary)}</details>
    <h1>${escape(e.step.title)}</h1>
    ${codes.length && r.codeIndex >= 0 ? `<div class="location-nav"><div class="progress"><strong>Code ${r.codeIndex + 1} of ${codes.length}</strong>${button('root', 'Repository…', undefined, false, true)}</div>
      ${r.root ? `<div class="muted root">${escape(r.root)}</div>` : ''}
      ${r.status ? `<p role="status">${escape(r.status)}</p>` : ''}
      ${codes.length > 1 ? `<div class="actions">${button('previousCode', '← Code', undefined, r.codeIndex <= 0, true)}${button('nextCode', 'Code →', undefined, r.codeIndex >= codes.length - 1, true)}</div>` : ''}</div>` : '<p class="muted">This step has no local code location.</p>'}
    <article>${e.step.blocks.map((b, index) => `<div data-comment-block="${index}" data-comment-block-id="${escape(b.id)}">${renderBlock(b, r, index)}</div>`).join('')}</article>
    <footer>${button('complete', r.done.has(e.key) ? '✓ Completed — mark unread' : 'Mark step complete', undefined, false, true)}${r.done.size === r.entries.length ? '<p role="status">Walkthrough complete.</p>' : ''}</footer>${commentsBody(r)}</main>${pageNavigation(r)}`;
}

function overview(r: Reading): string {
  const part = r.doc.parts.find(p => p.id === r.partId) ?? r.doc.parts[0];
  const source = r.doc.source;
  const content = r.view === 'overview'
    ? `<h1>${escape(r.doc.title)}</h1>${markdown(r.doc.summary)}<div class="part-cards">${r.doc.parts.map((p, i) => {
      const entries = r.entries.filter(e => e.part.id === p.id);
      return `<section class="part-card"><p class="muted">Part ${i + 1} · ${p.sections.length} sections · ${entries.length} steps</p><h2>${button('part', p.title, p.id, false, true)}</h2>${markdown(p.summary)}<p class="muted">${entries.filter(e => r.done.has(e.key)).length}/${entries.length} completed</p>${p.files?.length ? `<p class="file-chips">${p.files.map(f => `<code>${escape(f)}</code>`).join(' ')}</p>` : ''}</section>`;
    }).join('')}</div>`
    : `${button('overview', '← Overview', undefined, false, true)}<h1>${escape(part.title)}</h1>${markdown(part.description ?? part.summary)}${part.sections.map(s => `<section class="section-card"><h2>${escape(s.title)}</h2>${markdown(s.summary)}<ol>${r.entries.filter(e => e.section.id === s.id).map(e => `<li>${button('step', `${r.done.has(e.key) ? '✓ ' : ''}${e.step.title}`, e.key, false, true)}</li>`).join('')}</ol></section>`).join('')}`;
  return `<main data-address="${escape(r.view === 'overview' ? '' : part.id)}" data-focus-comment="${escape(r.commentFocus)}" data-page="${escape((r.identity ?? r.doc.title) + '/' + (r.view === 'overview' ? 'overview' : part.id))}">
    <header><p class="eyebrow">Code Walkthrough</p>${source?.url ? `<p><a href="${escape(source.url)}">${escape(source.label ?? source.identifier ?? 'View source')} ↗</a>${source.state ? ' · ' + escape(source.state) : ''}</p>` : ''}<p>${r.done.size} of ${r.entries.length} steps completed</p><progress value="${r.done.size}" max="${r.entries.length}" aria-label="Completed steps"></progress></header>
    ${r.error ? `<div role="alert" class="callout danger">${escape(r.error)}<p>The last valid version is shown.</p></div>` : ''}
    ${toolbar(r)}${publishedBanner(r)}${content}${commentsBody(r)}</main>${pageNavigation(r)}`;
}

function pageNavigation(r: Reading): string {
  const entry = r.entries[r.index];
  const previous = r.view === 'overview' ? '← Last step' : '← Previous';
  const next = r.view === 'overview' ? 'First part →' : r.view === 'part' ? 'Start reading →'
    : r.index === r.entries.length - 1 ? 'Finish → Overview'
    : r.entries[r.index + 1].part !== entry.part ? 'Next part →' : 'Next →';
  return `<nav class="page-navigation" aria-label="Walkthrough navigation"><div class="actions">${button('previous', previous, undefined, false, true)}${button('next', next)}</div></nav>`;
}

function publishedBanner(r: Reading) {
  return r.publishedUrl ? `<div class="published"><div class="actions"><a href="${escape(r.publishedUrl)}">Published walkthrough ↗</a>${button('refreshPublished', 'Refresh', undefined, false, true)}</div>${r.remoteNotice ? `<p role="status">${escape(r.remoteNotice)}</p>` : ''}</div>` : '';
}

export function commentContent(r: Reading) {
  let index = r.entries[r.index].step.blocks.length;
  return (r.comments?.threads ?? []).flatMap(thread => thread.messages.flatMap((message, messageIndex) => {
    const prefix = `comment-${thread.id}-${messageIndex}-`;
    return replyBlocks(message.blocks).map(block => ({
      index: index++, thread: thread.id, message: messageIndex,
      block: { ...block, ...(block.id ? { id: prefix + block.id } : {}), ...(block.type === 'diagram' ? { links: block.links?.map(l => ({ ...l, blockId: prefix + l.blockId })) } : {}) } as Block,
    }));
  }));
}

function commentsBody(r: Reading): string {
  if (!r.commentsOpen) return '';
  const content = commentContent(r);
  return `<section class="comments" aria-label="Comments"><h2>Comments</h2>
    ${r.commentsError ? `<p role="alert">${escape(r.commentsError)}</p>${button('commentsRetry', 'Reconnect comments', undefined, false, true)}` : ''}
    <p class="muted">${r.comments?.watching ? 'An agent is watching for questions.' : 'No agent is watching yet.'}</p>
    <div class="actions">${button('commentPrompt', 'Copy agent prompt', undefined, !r.comments, true)}${button('commentAdd', 'Ask about this page', undefined, !r.comments, true)}${button('comments', 'Hide comments', undefined, false, true)}</div>
    <p class="muted">Select code or prose in the walkthrough, then choose Comment on selection. You can also use CW: Comment on Editor Selection.</p>
    <button class="secondary" data-comment-selection>Comment on selection</button>
    <label class="archive-filter"><input type="checkbox" data-show-archived> Show archived</label>
    <div class="comment-threads">${(r.comments?.threads ?? []).map(t => `<section class="comment-thread" data-thread="${escape(t.id)}" data-comment-where="${escape(JSON.stringify(t.where))}" data-archived="${!!t.archived}">
      <h3>${button('commentLocation', t.where.title || 'Overview', t.id, false, true)} <span class="muted">${escape(t.status)}</span></h3>
      ${t.where.file ? `<p class="file-label">${escape(t.where.file)}${t.where.lines ? ':' + t.where.lines.start + '–' + t.where.lines.end : ''}</p>` : ''}
      ${t.where.quote ? `<blockquote>${escape(t.where.quote)}</blockquote>` : ''}
      ${t.messages.map((m, i) => `<div class="comment-message"><p class="muted">${m.from === 'agent' ? 'Agent' : 'Reader'} · ${escape(m.at)}</p>${markdown(m.text)}${content.filter(b => b.thread === t.id && b.message === i).map(b => renderBlock(b.block, r, b.index, true)).join('')}${m.blocks?.length && !replyBlocks(m.blocks).length ? '<p role="alert">This reply contains invalid blocks.</p>' : ''}</div>`).join('')}
      <div class="actions">${button('commentReply', 'Reply', t.id, false, true)}${button('commentArchive', t.archived ? 'Restore' : 'Archive', t.id, false, true)}</div>
    </section>`).join('') || '<p>No comments yet.</p>'}</div></section>`;
}

export function html(content: string, styleUri: string, scriptUri: string, cspSource: string, nonce: string, token: string, preferences: { theme?: string; accent?: string } = {}): string {
  return `<!doctype html><html lang="en"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src ${cspSource} 'unsafe-inline'; script-src 'nonce-${nonce}';"><link rel="stylesheet" href="${escape(styleUri)}"><title>Code Walkthrough</title></head><body data-token="${escape(token)}" data-theme="${escape(preferences.theme ?? 'auto')}" data-accent="${escape(preferences.accent ?? 'auto')}">${content}<script nonce="${nonce}" src="${escape(scriptUri)}"></script></body></html>`;
}

export function diffText(b: Extract<Block, { type: 'diff' }>): string {
  return `--- ${b.before ? 'a/' + b.before.file : '/dev/null'}\n+++ ${b.after ? 'b/' + b.after.file : '/dev/null'}\n` + b.hunks.map(h =>
    `@@ -${h.oldStart},${h.oldLines} +${h.newStart},${h.newLines} @@${h.heading ? ' ' + h.heading : ''}\n` + h.lines.map(l =>
      `${l.kind === 'add' ? '+' : l.kind === 'delete' ? '-' : ' '}${l.text}\n${l.noNewlineAtEnd ? '\\ No newline at end of file\n' : ''}`).join('')).join('');
}
