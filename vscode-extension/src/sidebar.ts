import mermaid from 'mermaid';
import DOMPurify from 'dompurify';
import hljs from 'highlight.js/lib/common';

declare function acquireVsCodeApi(): { postMessage(value: unknown): void; getState(): any; setState(value: unknown): void };
const api = acquireVsCodeApi();
const main = document.querySelector('main')!;
const navigation = document.querySelector<HTMLElement>('.page-navigation');
if (navigation) {
  // Reserve its actual height, including wrapped labels and editor font scaling.
  new ResizeObserver(() => document.documentElement.style.setProperty('--cw-navigation-height', `${navigation.offsetHeight}px`)).observe(navigation);
}
const pageKey = main.dataset.page ?? '';
const previous = api.getState();
const state = previous?.page === pageKey ? previous : { page: pageKey, timelines: {}, notes: {}, sources: {}, scroll: 0 };
const save = () => api.setState(state);
const send = (action: string, value?: unknown) => api.postMessage({ action, value, token: document.body.dataset.token });
const theme = document.body.dataset.theme;
if (theme === 'light' || theme === 'dark') {
  document.body.classList.remove('vscode-light', 'vscode-dark', 'vscode-high-contrast', 'vscode-high-contrast-light');
  document.body.classList.add('vscode-' + theme);
}

function highlight() {
  const aliases: Record<string, string> = { ts: 'typescript', tsx: 'typescript', js: 'javascript', jsx: 'javascript', cs: 'csharp', py: 'python', rs: 'rust', sh: 'bash', yml: 'yaml', md: 'markdown', h: 'cpp', hpp: 'cpp' };
  for (const pre of document.querySelectorAll<HTMLElement>('pre[data-highlight]')) {
    const rows = [...pre.querySelectorAll<HTMLElement>('.code-text')];
    if (!rows.length) continue;
    const extension = pre.dataset.file?.split('.').pop() ?? '';
    const language = pre.dataset.highlight || aliases[extension] || extension;
    if (!hljs.getLanguage(language)) continue;
    const text = rows.map(row => row.textContent ?? '').join('\n');
    const template = document.createElement('template');
    template.innerHTML = hljs.highlight(text, { language, ignoreIllegals: true }).value;
    rows.forEach(row => row.replaceChildren());
    let line = 0;
    // Split highlighted DOM across gutter rows, carrying nested token classes.
    // This preserves multiline strings and comments without coloring line numbers.
    function visit(node: Node, classes: string[]) {
      if (node.nodeType === Node.TEXT_NODE) {
        (node.textContent ?? '').split('\n').forEach((part, index) => {
          if (index) line++;
          if (!rows[line]) return;
          const span = document.createElement('span');
          span.className = classes.join(' '); span.textContent = part;
          rows[line].append(span);
        });
      } else if (node instanceof Element || node instanceof DocumentFragment) {
        const next = node instanceof Element && node.className ? [...classes, node.className] : classes;
        node.childNodes.forEach(child => visit(child, next));
      }
    }
    visit(template.content, []);
    pre.dataset.highlighted = 'true';
  }
  for (const pre of document.querySelectorAll<HTMLElement>('pre:not([data-highlight]):not(.diagram-source pre) > code')) {
    const language = [...pre.classList].find(c => c.startsWith('language-'))?.slice(9);
    if (language && hljs.getLanguage(language)) pre.innerHTML = hljs.highlight(pre.textContent ?? '', { language, ignoreIllegals: true }).value;
  }
}

function annotations() {
  for (const note of document.querySelectorAll<HTMLElement>('[data-annotation]')) {
    const key = note.dataset.annotation!;
    note.hidden = !state.notes[key];
    for (const marker of document.querySelectorAll<HTMLButtonElement>('[data-note]')) {
      if (marker.dataset.note !== key) continue;
      marker.setAttribute('aria-expanded', String(!note.hidden));
      marker.addEventListener('click', () => {
        state.notes[key] = !state.notes[key]; save(); note.hidden = !state.notes[key];
        document.querySelectorAll<HTMLElement>('[data-note]').forEach(m => {
          if (m.dataset.note === key) {
            m.setAttribute('aria-expanded', String(!note.hidden));
            m.closest('.code-line')?.classList.toggle('note-selected', !note.hidden);
          }
        });
        if (!note.hidden) note.scrollIntoView({ block: 'nearest' });
      });
    }
  }
}

function comments() {
  for (const thread of document.querySelectorAll<HTMLElement>('[data-comment-where]')) {
    if (thread.dataset.archived === 'true') continue;
    let where: any;
    try { where = JSON.parse(thread.dataset.commentWhere!); } catch { continue; }
    if (where.step !== main.dataset.address) continue;
    const focus = thread.dataset.thread === main.dataset.focusComment;
    let marked: HTMLElement | undefined;
    if (where.kind === 'code' && where.file && where.lines) {
      for (const pre of document.querySelectorAll<HTMLElement>('article .snippet')) {
        if (pre.dataset.file !== where.file) continue;
        for (const row of pre.querySelectorAll<HTMLElement>('.code-line')) {
          const line = Number(row.querySelector('.line-number')?.textContent);
          if (line >= where.lines.start && line <= where.lines.end) { row.classList.add('comment-anchor'); marked ??= row; }
        }
      }
    } else if (focus && where.quote) {
      const candidates = [...main.querySelectorAll<HTMLElement>('article p, article li, .part-card, .section-card')];
      marked = candidates.find(element => element.textContent?.includes(where.quote));
      marked?.classList.add('comment-anchor');
    }
    if (focus) {
      thread.classList.add('focused-comment');
      if (marked) requestAnimationFrame(() => marked!.scrollIntoView({ block: 'center' }));
    }
  }
  const checkbox = document.querySelector<HTMLInputElement>('[data-show-archived]');
  const filter = () => document.querySelectorAll<HTMLElement>('.comment-thread').forEach(thread => {
    thread.hidden = thread.dataset.archived === 'true' && !checkbox?.checked;
  });
  if (checkbox) {
    checkbox.checked = !!state.showArchived; filter();
    checkbox.addEventListener('change', () => { state.showArchived = checkbox.checked; save(); filter(); });
  }
  let selected: { quote: string; block?: number; first?: number; last?: number } | undefined;
  document.addEventListener('selectionchange', () => {
    const selection = window.getSelection();
    if (!selection?.rangeCount || selection.isCollapsed) return;
    const range = selection.getRangeAt(0);
    const parent = range.commonAncestorContainer instanceof Element ? range.commonAncestorContainer : range.commonAncestorContainer.parentElement;
    if (!parent || !main.contains(parent) || parent.closest('.comments')) return;
    selected = { quote: selection.toString().slice(0, 8000) };
    const block = parent.closest<HTMLElement>('[data-comment-block]');
    if (block) {
      selected.block = Number(block.dataset.commentBlock);
      const rows = [...block.querySelectorAll<HTMLElement>('.snippet .code-text')];
      const touched = rows.map((row, index) => range.intersectsNode(row) ? index + 1 : 0).filter(Boolean);
      if (touched.length) { selected.first = touched[0]; selected.last = touched[touched.length - 1]; }
    }
  });
  const button = document.querySelector<HTMLButtonElement>('[data-comment-selection]');
  button?.addEventListener('pointerdown', event => event.preventDefault());
  button?.addEventListener('click', () => { if (selected?.quote.trim()) send('commentSelection', selected); });
}

function timelines() {
  for (const box of document.querySelectorAll<HTMLElement>('[data-timeline]')) {
    const key = box.dataset.timeline!;
    const frames = [...box.querySelectorAll<HTMLElement>('.frame')];
    const saved = state.timelines[key] ?? { index: 0, playing: !matchMedia('(prefers-reduced-motion: reduce)').matches };
    let index = Math.min(Math.max(0, Number(saved.index) || 0), frames.length - 1);
    let playing = !!saved.playing;
    let timer: number | undefined;
    const play = box.querySelector<HTMLButtonElement>('[data-play]')!;
    const ticks = [...box.querySelectorAll<HTMLButtonElement>('[data-frame]')];
    function paint() {
      window.clearTimeout(timer);
      const frame = frames[index];
      box.dataset.frameIndex = String(index);
      box.querySelector('.frame-label')!.textContent = frame.dataset.label ?? '';
      box.querySelector('.frame-note')!.replaceChildren(...[...frame.querySelector('.frame-prose')!.childNodes].map(n => n.cloneNode(true)));
      for (const cell of box.querySelectorAll<HTMLElement>('.timeline-node')) {
        const entry = [...frame.querySelectorAll<HTMLElement>('[data-node-id]')].find(s => s.dataset.nodeId === cell.dataset.nodeId)!;
        const status = entry.dataset.state ?? 'idle';
        cell.dataset.state = ['idle', 'active', 'done', 'gone', 'alert'].includes(status) ? status : 'idle';
        cell.querySelector('.node-state')!.textContent = status;
        cell.querySelector('.node-detail')!.textContent = entry.dataset.detail ?? '';
      }
      ticks.forEach((tick, i) => tick.setAttribute('aria-pressed', String(i === index)));
      play.textContent = playing ? 'Pause' : 'Play';
      play.setAttribute('aria-label', playing ? 'Pause timeline' : 'Play timeline');
      state.timelines[key] = { index, playing }; save();
      if (playing && !document.hidden) timer = window.setTimeout(() => { index = (index + 1) % frames.length; paint(); }, Math.max(1, Number(frame.dataset.duration) || 2200));
    }
    play.addEventListener('click', () => { playing = !playing; paint(); });
    ticks.forEach((tick, i) => tick.addEventListener('click', () => { index = i; playing = false; paint(); }));
    box.querySelector('[data-frame-back]')!.addEventListener('click', () => { index = (index + frames.length - 1) % frames.length; playing = false; paint(); });
    box.querySelector('[data-frame-next]')!.addEventListener('click', () => { index = (index + 1) % frames.length; playing = false; paint(); });
    document.addEventListener('visibilitychange', paint);
    window.addEventListener('pagehide', () => window.clearTimeout(timer));
    paint();
  }
}

function wireNodes(host: HTMLElement, figure: HTMLElement) {
  const links = new Map([...figure.querySelectorAll<HTMLButtonElement>('[data-node]')].map(b => [b.dataset.node!, b.dataset.value!]));
  const attached = new Set<Element>();
  const register = (node: Element, id: string) => {
    const blockId = links.get(id);
    if (!blockId || attached.has(node)) return;
    attached.add(node);
    node.classList.add('code-link'); node.setAttribute('tabindex', '0'); node.setAttribute('role', 'button'); node.setAttribute('aria-label', `Show code: ${id}`);
    node.addEventListener('click', event => { event.preventDefault(); event.stopPropagation(); send('block', blockId); });
    node.addEventListener('keydown', event => {
      if ((event as KeyboardEvent).key === 'Enter' || (event as KeyboardEvent).key === ' ') { event.preventDefault(); send('block', blockId); }
    });
  };
  host.querySelectorAll('g.node').forEach(node => register(node, node.id.replace(/^.*flowchart-/, '').replace(/-\d+$/, '')));
  host.querySelectorAll('text, tspan').forEach(text => {
    const id = text.textContent?.trim() ?? '';
    const group = text.closest('g');
    if (group) register(group, id);
  });
}

function zoomDiagram(figure: HTMLElement) {
  const host = figure.querySelector<HTMLElement>('.diagram-canvas')!;
  const svg = host.querySelector<SVGSVGElement>('svg');
  if (!svg) return;
  const trigger = figure.querySelector<HTMLButtonElement>('[data-zoom]')!;
  const dialog = document.createElement('dialog'); dialog.className = 'diagram-dialog';
  dialog.setAttribute('aria-label', host.getAttribute('aria-label') ?? 'Full-size diagram');
  dialog.innerHTML = '<div class="zoom-controls"><button data-out aria-label="Zoom out">−</button><button data-in aria-label="Zoom in">+</button><button data-fit>Fit</button><span class="zoom-percent"></span><button data-close>Close</button></div><div class="zoom-viewport"></div>';
  document.body.append(dialog);
  const viewport = dialog.querySelector<HTMLElement>('.zoom-viewport')!;
  const originalStyle = svg.getAttribute('style');
  const originalWidth = svg.getAttribute('width'), originalHeight = svg.getAttribute('height');
  const bounds = svg.viewBox.baseVal;
  const width = bounds.width || svg.getBoundingClientRect().width || 800;
  const height = bounds.height || svg.getBoundingClientRect().height || 600;
  let scale = 1;
  viewport.append(host); dialog.showModal();
  const resize = (next: number) => {
    scale = Math.max(.1, Math.min(8, next));
    svg.style.maxWidth = 'none'; svg.style.width = width * scale + 'px'; svg.style.height = height * scale + 'px';
    dialog.querySelector('.zoom-percent')!.textContent = Math.round(scale * 100) + '%';
  };
  const fit = () => resize(Math.min((viewport.clientWidth - 32) / width, (viewport.clientHeight - 32) / height));
  dialog.querySelector('[data-in]')!.addEventListener('click', () => resize(scale * 1.25));
  dialog.querySelector('[data-out]')!.addEventListener('click', () => resize(scale / 1.25));
  dialog.querySelector('[data-fit]')!.addEventListener('click', fit);
  dialog.querySelector('[data-close]')!.addEventListener('click', () => dialog.close());
  dialog.addEventListener('close', () => {
    figure.insertBefore(host, figure.querySelector('.diagram-status'));
    for (const [name, value] of [['style', originalStyle], ['width', originalWidth], ['height', originalHeight]]) {
      if (value === null) svg.removeAttribute(name!); else svg.setAttribute(name!, value!);
    }
    dialog.remove(); trigger.focus();
  });
  viewport.addEventListener('wheel', event => {
    if (!event.ctrlKey) return;
    event.preventDefault(); resize(scale * (event.deltaY < 0 ? 1.1 : 1 / 1.1));
  }, { passive: false });
  let drag: { x: number; y: number; left: number; top: number } | undefined;
  viewport.addEventListener('pointerdown', event => {
    if (event.button || (event.target as Element).closest('.code-link')) return;
    drag = { x: event.clientX, y: event.clientY, left: viewport.scrollLeft, top: viewport.scrollTop };
    viewport.setPointerCapture(event.pointerId);
  });
  viewport.addEventListener('pointermove', event => {
    if (drag) { viewport.scrollLeft = drag.left - event.clientX + drag.x; viewport.scrollTop = drag.top - event.clientY + drag.y; }
  });
  viewport.addEventListener('pointerup', () => { drag = undefined; });
  viewport.addEventListener('pointercancel', () => { drag = undefined; });
  fit();
}

async function diagrams() {
  const dark = document.body.classList.contains('vscode-dark') || document.body.classList.contains('vscode-high-contrast');
  mermaid.initialize({ startOnLoad: false, securityLevel: 'strict', theme: dark ? 'dark' : 'default',
    htmlLabels: false, fontFamily: 'sans-serif', maxTextSize: 100000, suppressErrorRendering: true,
    secure: ['secure', 'securityLevel', 'startOnLoad', 'maxTextSize', 'suppressErrorRendering', 'htmlLabels', 'flowchart', 'themeCSS', 'dompurifyConfig'],
    flowchart: { htmlLabels: false } });
  for (const figure of document.querySelectorAll<HTMLElement>('[data-diagram]')) {
    const key = figure.dataset.diagram!;
    const source = figure.querySelector<HTMLDetailsElement>('.diagram-source')!;
    source.open = !!state.sources[key];
    source.addEventListener('toggle', () => { state.sources[key] = source.open; save(); });
    const host = figure.querySelector<HTMLElement>('.diagram-canvas')!;
    const status = figure.querySelector<HTMLElement>('.diagram-status')!;
    try {
      const result = await mermaid.render(`cw-diagram-${key}`, source.querySelector('code')!.textContent ?? '');
      // Keep only SVG graphics. No image loads, HTML, embedded objects or URL
      // links from untrusted diagram text; code links come from validated IDs.
      host.innerHTML = DOMPurify.sanitize(result.svg, { USE_PROFILES: { svg: true, svgFilters: true }, FORBID_TAGS: ['foreignObject', 'image', 'script', 'iframe', 'object', 'a'] });
      host.querySelectorAll('*').forEach(node => {
        for (const attr of [...node.attributes]) {
          if ((attr.localName === 'href' || attr.name === 'src') && !attr.value.startsWith('#')) node.removeAttribute(attr.name);
        }
      });
      wireNodes(host, figure);
      status.hidden = true; figure.dataset.rendered = 'true';
      figure.querySelector('[data-zoom]')!.addEventListener('click', () => zoomDiagram(figure));
    } catch {
      status.textContent = 'This diagram could not be drawn. Its source and description are available below.';
      source.open = true;
      figure.querySelector<HTMLButtonElement>('[data-zoom]')!.disabled = true;
    }
  }
}

document.addEventListener('click', event => {
  const target = event.target instanceof Element ? event.target : null;
  const button = target?.closest<HTMLButtonElement>('button[data-action]');
  if (button && !button.disabled) {
    send(button.dataset.action!, button.dataset.value);
    return;
  }
  const link = target?.closest('a[href]');
  if (link) { event.preventDefault(); send('link', link.getAttribute('href') ?? ''); }
});
window.addEventListener('message', event => {
  if (event.data?.type !== 'copied' || event.data.token !== document.body.dataset.token) return;
  const button = [...document.querySelectorAll<HTMLButtonElement>('button[data-action="copy"]')].find(b => b.dataset.value === event.data.value);
  if (button) {
    const original = button.textContent; button.textContent = 'Copied'; window.setTimeout(() => button.textContent = original, 1400);
  }
});
document.addEventListener('keydown', event => {
  if (event.defaultPrevented || event.ctrlKey || event.altKey || event.metaKey || document.querySelector('dialog[open]')) return;
  if ((event.target as Element)?.closest('input, textarea, select, button, a, [contenteditable="true"], [role="button"]')) return;
  const action = ({ ArrowRight: 'next', ArrowLeft: 'previous', Escape: 'up', t: 'theme', s: 'settings', o: 'openCode', c: 'comments' } as Record<string, string>)[event.key];
  if (action) { event.preventDefault(); send(action); }
});
window.addEventListener('scroll', () => { state.scroll = window.scrollY; save(); }, { passive: true });
highlight(); annotations(); timelines();
void diagrams().then(() => {
  window.scrollTo(0, state.scroll ?? 0);
  comments();
  document.body.dataset.ready = 'true';
  api.postMessage({ action: 'ready', token: document.body.dataset.token, report: {
    diagrams: document.querySelectorAll('[data-diagram][data-rendered="true"]').length,
    failedDiagrams: document.querySelectorAll('[data-diagram]:not([data-rendered="true"])').length,
    timelines: document.querySelectorAll('[data-timeline][data-frame-index]').length,
    highlighted: document.querySelectorAll('[data-highlighted="true"]').length,
  } });
});
