/* The cw/2 step: a list of blocks in the order the author wrote them, as many
   as it takes and in any mix.

   Standard kinds include references to other walkthroughs. The
   one called `extension` is the door out, and it carries the sentence a reader
   who has never heard of it should print instead. This reader has never heard
   of any of them, so that is what it does. */

import {
  HOSTED, el, mdEl, mdInline, mdBlock, baseName, drawMermaid, openDiagram, highlightInto,
  langFor, fileName, openButton, openAt, every, api, toast,
} from "./ui.js";
import { state, doc, parts, setUI, setSticky, go, openStep } from "./state.js";

const ui = () => state.ui;

export default {
  version: "cw/2",

  // cw/2 keeps a repository as a URL rather than as owner/name, because not
  // every forge has an owner and a name. The crumb wants the short form back.
  head(d) {
    const src = d.source || {};
    return {
      repo: shortRepo(src.repositoryUrl),
      number: src.label || src.identifier,
      url: src.url,
      state: src.state,
    };
  },

  partIntro(p) { return { short: p.summary, long: p.description || p.summary }; },
  sectionIntro(s) { return s.summary; },

  stepCode(step) {
    for (const b of step.blocks || []) {
      if (b.type === "code" && b.snippet && b.snippet.source) {
        return { file: b.snippet.source.file, line: b.snippet.source.startLine || 1 };
      }
    }
    return null;
  },

  step(step, host) { into(step.blocks, host, ""); },

  /* The overview and a part page hold blocks of their own: the shape of the
     thing, drawn before anybody is sent into a step. They are the same seven,
     so they are drawn by the same builders, and the key is prefixed because
     the panel state they address is one bag for the whole document. */
  page(list, host, key) { into(list, host, key); },
};

function into(list, host, key) {
  for (const [i, b] of (list || []).entries()) {
    const node = blockNode(b, key + i);
    if (!node) continue;
    // The spacing belongs to being a block rather than to being a diagram or a
    // diff, so it is set here and the builders only say what they are.
    node.classList.add("block");
    if (b.id) node.dataset.block = b.id;
    // And what a comment hangs on. A block without an id is addressed by where
    // it sits, which is enough for as long as the page is open.
    node.dataset.anchor = b.id || "#" + i;
    node.dataset.kind = anchorKind(b.type);
    if (b.id && ui().focus === b.id) {
      node.classList.add("focus");
      // A diagram somewhere else sent the reader here, so put the block it
      // meant in front of them rather than at the top of a page they now have
      // to search.
      requestAnimationFrame(() => node.scrollIntoView({ block: "center", behavior: "smooth" }));
    }
    host.appendChild(node);
  }
}

/* The blocks of something that is not a step. A comment's answer is written
   in the same seven, so it is drawn by the same builders rather than by a
   second set that would drift from these.

   The keys are prefixed because the panel state they address (which note is
   open, which frame a timeline is on) is a document-wide bag, and block 0 of
   an answer is not block 0 of the step behind it. */
export function blocks(list, key) {
  const frag = document.createDocumentFragment();
  for (const [i, b] of (list || []).entries()) {
    const node = blockNode(b, key + i);
    if (!node) continue;
    node.classList.add("block");
    frag.appendChild(node);
  }
  return frag;
}

function blockNode(b, i) {
  switch (b.type) {
    case "markdown": return mdBlock(b.text);
    case "code": return codeBlock(b, i);
    case "callout": return calloutBlock(b);
    case "diagram": return diagramBlock(b, i);
    case "diff": return diffBlock(b);
    case "timeline": return timelineBlock(b, i);
    case "extension": return extensionBlock(b);
    case "reference": return referenceBlock(b);
  }
  // The schema refuses an unknown kind, so this is a document that got here
  // some other way. Say so rather than drawing nothing.
  return el("p", "block-unknown", "This step has a " + b.type + " block, which this reader does not know.");
}

/* Three kinds of thing to select in, and they differ in what can be marked
   back up afterwards. Code is marked by the line, because the highlighter
   owns what is inside a line and wants exactly one span per line. Prose is
   marked by finding the words again. The rest keeps the comment without
   drawing on it: a mermaid drawing and a diff have no words of their own to
   put a mark around. */
function referenceBlock(b) {
  const box = el('aside', 'reference');
  box.appendChild(el('small', null, ['related', 'deep-dive', 'prerequisite', 'next'].includes(b.relation) ? b.relation : 'related'));
  box.appendChild(el('h3', null, b.title));
  if (b.description) box.appendChild(mdBlock(b.description));
  const button = el('button', 'nav', 'Open walkthrough →');
  button.onclick = async () => {
    button.disabled = true;
    try {
      let destination;
      let local = false;
      if (!HOSTED && b.target.file) {
        try { destination = new URL((await api('/api/reference', { method: 'POST', body: JSON.stringify({ file: b.target.file }) })).url, location.href); local = true; }
        catch (error) { if (!b.target.url) throw error; }
      }
      if (!destination && b.target.url) destination = new URL(b.target.url);
      if (!destination) throw new Error('This reference needs its local JSON file. No published URL is provided.');
      if (!/^https?:$/.test(destination.protocol) || destination.username || destination.password) throw new Error('Invalid walkthrough URL.');
      destination.hash = b.target.stepId ? 'step=' + encodeURIComponent(b.target.stepId) : '';
      // Keep the original available even if a hosted target is offline or locked.
      // Same-origin readers can also offer a direct return button.
      if (local && destination.origin === location.origin) {
        try { sessionStorage.setItem('cw:return:' + destination.pathname, location.href); } catch {}
        location.assign(destination.href);
      } else {
        const link = document.createElement('a');
        link.href = destination.href; link.target = '_blank'; link.rel = 'noopener noreferrer'; link.click();
      }
    } catch (error) { toast(error.message || String(error), true); }
    finally { button.disabled = false; }
  };
  box.appendChild(button);
  return box;
}

function anchorKind(type) {
  if (type === "code") return "code";
  if (type === "markdown" || type === "callout" || type === "extension") return "text";
  return "block";
}

function shortRepo(url) {
  if (!url) return "";
  const bits = String(url).replace(/^[a-z]+:\/\//i, "").replace(/\.git$/, "").split("/").filter(Boolean);
  return bits.length >= 3 ? bits.slice(-2).join("/") : bits.slice(1).join("/");
}

/* ------------------------------------------------------------------ code */

const docLang = () => (state.data && doc().language) || "";

/* A snippet counts its own lines from 1. What is shown in the gutter is the
   line in the file, when the snippet says which file it came from, because that
   is the number a reader types into their editor. Without a source there is no
   such number and no such file, and the gutter counts from 1 like the format
   does. */
function codeBlock(b, at) {
  const s = b.snippet || {};
  const src = s.source || null;
  const first = src && src.startLine ? src.startLine : 1;
  const shown = (rel) => first + rel - 1;

  const wrap = el("div");
  if (src) wrap.dataset.file = src.file;
  const open = openAnnotation(s, at);

  const box = el("div", "codebox" + (open ? " with-note" : ""));
  const head = el("div", "code-head");
  if (src) head.appendChild(fileName(src.file, first, src.endLine));
  if (s.label) head.appendChild(el("span", "tag", s.label));
  else if (!src) head.appendChild(el("span", "tag", "example"));
  const chip = verifiedTag(s.verification);
  if (chip) head.appendChild(chip);
  head.appendChild(el("span", "grow"));
  if (src) head.appendChild(openButton(src.file, first, "open-dark", src.endLine));
  box.appendChild(head);

  const focus = ranges(s.highlights, (h) => (h.kind || "focus") === "focus");
  const added = ranges(s.highlights, (h) => h.kind === "added");
  const notes = s.annotations || [];

  const lines = el("div", "lines" + (src ? "" : " loose"));
  s.text.replace(/\r\n/g, "\n").replace(/\n$/, "").split("\n").forEach((t, i) => {
    const rel = i + 1;
    const note = notes.findIndex((n) => inRange(n.lines, rel));
    const on = open && open.index === note;
    const row = el("div", "row" + (added.has(rel) ? " add" : "") + (focus.has(rel) ? " hi" : "") +
      (on ? " open" : "") + (note >= 0 ? " clickable" : ""));
    // The number in the file, on the row rather than only in the gutter: a
    // selection dragged across rows becomes a range of lines somebody can
    // open, and a comment already made finds its rows again.
    row.dataset.line = String(shown(rel));

    const num = el("span", "n" + (src ? " link" : ""), String(shown(rel)));
    if (src) {
      num.title = "open " + src.file + " at line " + shown(rel);
      num.addEventListener("click", (e) => { e.stopPropagation(); openAt(src.file, shown(rel)); });
    }
    row.appendChild(num);
    row.appendChild(el("span", "mark", added.has(rel) ? "+" : " "));
    row.appendChild(el("span", "t", t === "" ? " " : t));

    if (note >= 0) {
      // Only the first line of a range carries the marker, so a note about six
      // lines does not look like six notes.
      const start = notes[note].lines.start;
      if (rel === start) {
        const q = el("button", "qmark", on ? "×" : "?");
        q.type = "button";
        q.title = "the note on these lines";
        row.appendChild(q);
      }
      row.addEventListener("click", () => setUI({ note: on ? null : at + ":" + note }));
    }
    lines.appendChild(row);
  });
  box.appendChild(lines);
  highlightInto(lines, s.text.replace(/\n$/, ""), langFor(src && src.file, s.language, docLang()));
  wrap.appendChild(box);

  if (open) {
    const n = notes[open.index];
    const np = el("div", "linenote");
    np.appendChild(el("span", "at", rangeLabel(n.lines, shown)));
    np.appendChild(mdEl("span", "txt", n.text));
    const x = el("button", "x", "×");
    x.type = "button";
    x.addEventListener("click", () => setUI({ note: null }));
    np.appendChild(x);
    wrap.appendChild(np);
  } else if (notes.length) {
    wrap.appendChild(el("div", "notes-hint",
      "click a marked line for the note behind it · " + notes.length + " on this snippet"));
  }
  return wrap;
}

// Which annotation is open is addressed by block and index, because a step can
// hold several snippets and line 2 of one is not line 2 of another.
function openAnnotation(s, at) {
  const want = String(ui().note || "");
  if (!want.startsWith(at + ":")) return null;
  const index = Number(want.slice(String(at).length + 1));
  return (s.annotations || [])[index] ? { index } : null;
}

const inRange = (r, n) => n >= r.start && n <= (r.end || r.start);

function ranges(list, keep) {
  const out = new Set();
  for (const h of list || []) {
    if (!keep(h)) continue;
    for (let n = h.lines.start; n <= (h.lines.end || h.lines.start); n++) out.add(n);
  }
  return out;
}

function rangeLabel(r, shown) {
  const a = shown(r.start), b = shown(r.end || r.start);
  return a === b ? "line " + a : "lines " + a + " to " + b;
}

/* cw/2 says what a snippet was measured against and when, which cw/1 could only
   hint at. Locally the page has just checked it against this machine; hosted it
   is repeating what the publisher found, and both are worded as what they are. */
function verifiedTag(v) {
  if (!v || v.state === "match") return null;
  const when = HOSTED ? " when this was published" : " in the tree";
  const label = {
    moved: "had moved" + when,
    different: "did not match the code" + when,
    "missing-file": "file was gone" + when,
    unavailable: "could not be checked",
  }[v.state] || v.state;
  const tag = el("span", "tag " + (v.state === "moved" ? "moved" : "stale"), label);
  const at = v.resolvedSource && v.resolvedSource.startLine ? ", now at line " + v.resolvedSource.startLine : "";
  tag.title = (v.note || "") + at + " · checked against " + (v.against ? v.against.revision.slice(0, 7) : "?");
  return tag;
}

/* ------------------------------------------------------------------ callout */

const CALLOUT_LABEL = { info: "Note", tip: "Worth knowing", warning: "Watch out", danger: "Careful" };

function calloutBlock(b) {
  const sev = b.severity || "info";
  const c = el("div", "callout sev-" + sev);
  c.appendChild(el("span", "label", b.title || CALLOUT_LABEL[sev] || "Note"));
  c.appendChild(mdEl("span", "txt", b.text));
  return c;
}

/* ------------------------------------------------------------------ diagram */

// Every block in the document that has an id, and where it lives, so a diagram
// can send a reader to a snippet three steps away.
/* Which screen every block with an id is on. A diagram link names a block and
   the reader should not have to know where it lives, and since the overview and
   the part pages hold blocks too, "where" is a screen rather than three
   numbers. */
function blockIndex() {
  const found = new Map();
  const put = (list, where) => (list || []).forEach((b) => { if (b.id) found.set(b.id, where); });
  put(doc().blocks, { view: "overview", part: null, section: null });
  parts().forEach((p, pi) => {
    put(p.blocks, { view: "part", part: pi, section: null });
    p.sections.forEach((s, si) => s.steps.forEach((st, ii) => {
      put(st.blocks, { view: "step", part: pi, section: si, step: ii });
    }));
  });
  return found;
}

// Whether that screen is the one being read.
function onScreen(w) {
  return w.view === state.view && w.part === state.part &&
    (w.view !== "step" || (w.section === state.section && w.step === state.step));
}

function diagramBlock(b, at) {
  const panel = el("div", "panel");
  const head = el("div", "panel-head");
  head.appendChild(el("span", "kind", "diagram"));
  if ((b.links || []).length) head.appendChild(el("span", "hint", "click a shape for its code"));
  const big = el("button", "tiny", "full size");
  big.type = "button";
  big.title = "the diagram in a window of its own";
  if (!(b.links || []).length) big.style.marginLeft = "auto";
  big.addEventListener("click", () => openDiagram(b.text));
  head.appendChild(big);
  const src = el("button", "tiny", ui().source === at ? "hide mermaid source" : "view mermaid source");
  src.type = "button";
  src.addEventListener("click", () => setUI({ source: ui().source === at ? null : at }));
  head.appendChild(src);
  panel.appendChild(head);

  const host = el("div", "diagram");
  host.setAttribute("role", "img");
  host.setAttribute("aria-label", b.alt);
  panel.appendChild(host);
  drawMermaid(host, b.text).then((svg) => { if (svg) wireLinks(host, b); });

  if (ui().source === at) {
    const box = el("div", "source");
    box.appendChild(el("pre", null, b.text));
    panel.appendChild(box);
  }
  if (b.caption) panel.appendChild(mdEl("div", "caption", b.caption));
  // The alt text is the diagram for anyone who cannot see it, so it is on the
  // page rather than only in an attribute.
  panel.appendChild(el("p", "alt", b.alt));
  return panel;
}

/* In cw/1 a diagram carried its own copy of the code, so the same lines were
   pasted twice and went stale separately. In cw/2 it names a block, and the
   block can be anywhere in the document. */
function wireLinks(host, b) {
  const links = b.links || [];
  if (!links.length) return;
  const index = blockIndex();
  const byNode = new Map(links.map((l) => [l.nodeId, l.blockId]));

  const register = (node, nodeId) => {
    const blockId = byNode.get(nodeId);
    const where = index.get(blockId);
    if (!node || !where) return;
    node.style.cursor = "pointer";
    node.setAttribute("title", "show " + blockId);
    node.addEventListener("click", (e) => {
      e.stopPropagation();
      // The block may be on this page or three steps away, and the reader
      // should not have to know which.
      if (onScreen(where)) {
        setUI({ focus: ui().focus === blockId ? null : blockId });
        return;
      }
      go(Object.assign({ step: 0, ui: { focus: blockId } }, where));
    });
  };

  // Mermaid names a flowchart node after the diagram it is in, so the id reads
  // mmd-3-flowchart-version-1 rather than flowchart-version-1.
  host.querySelectorAll("g.node").forEach((n) => {
    register(n, (n.id || "").replace(/^.*flowchart-/, "").replace(/-\d+$/, ""));
  });
  host.querySelectorAll("text, tspan").forEach((t) => {
    const label = (t.textContent || "").trim();
    if (byNode.has(label)) register(t.closest("g") || t.parentNode, label);
  });
}

/* ------------------------------------------------------------------ diff */

/* cw/1 wrote a diff as a flat list of lines with one number for both sides,
   which is enough to draw and not enough to be right. cw/2 carries the
   coordinates a unified diff actually has, so both gutters can say the truth
   and a hunk can start where it starts. */
function diffBlock(b) {
  const wrap = el("div", "diff");
  const head = el("div", "diff-head");
  const before = b.before ? b.before.file : null;
  const after = b.after ? b.after.file : null;
  if (after) head.appendChild(fileName(after, 1));
  else head.appendChild(el("span", "file plain", baseName(before)));
  if (before && after && before !== after) {
    head.appendChild(el("span", "tag", "was " + baseName(before)));
  }
  if (!before) head.appendChild(el("span", "tag", "new file"));
  if (!after) head.appendChild(el("span", "tag stale", "deleted"));
  head.appendChild(el("span", "grow"));
  if (after) head.appendChild(openButton(after, 1, "open-dark"));
  wrap.appendChild(head);

  const body = el("div", "diff-body");
  const lang = langFor(after || before, b.language, docLang());

  if (!(b.hunks || []).length) {
    body.appendChild(el("div", "hunk-head", "no lines changed, only where the file lives"));
  }

  for (const h of b.hunks || []) {
    const label = "@@ -" + h.oldStart + "," + h.oldLines + " +" + h.newStart + "," + h.newLines + " @@";
    body.appendChild(el("div", "hunk-head", h.heading ? label + "  " + h.heading : label));

    let oldAt = h.oldStart, newAt = h.newStart;
    const text = [];
    for (const line of h.lines) {
      const row = el("div", "row " + line.kind);
      row.appendChild(el("span", "n old", line.kind === "add" ? "" : String(oldAt++)));
      row.appendChild(el("span", "n new", line.kind === "delete" ? "" : String(newAt++)));
      row.appendChild(el("span", "mark", line.kind === "add" ? "+" : line.kind === "delete" ? "-" : " "));
      row.appendChild(el("span", "t", line.text === "" ? " " : line.text));
      body.appendChild(row);
      text.push(line.text);
      if (line.noNewlineAtEnd) {
        body.appendChild(el("div", "nonewline", "\\ no newline at end of file"));
      }
    }
    // Highlighting reads the hunk as one piece of code, which is what it is:
    // the marker column and the two gutters are the page's own.
    highlightInto(body, text.join("\n"), lang);
  }
  wrap.appendChild(body);
  if (b.caption) wrap.appendChild(mdEl("div", "caption", b.caption));
  return wrap;
}

/* ------------------------------------------------------------------ timeline */

/* Every frame names every node, so a reader can be dropped into the third one
   without having seen the first two. That is what makes the ticks a scrubber
   rather than a play button with a rewind. */
function timelineBlock(b, at) {
  const playing = state.sticky.playing !== false;
  const box = el("div", "anim");
  const controls = el("div", "controls");
  const play = el("button", "tiny", playing ? "pause" : "play");
  play.type = "button";
  play.addEventListener("click", () => setSticky({ playing: !playing }));
  controls.appendChild(play);

  const start = Number(ui()["frame" + at] || 0) % b.frames.length;
  const ticks = el("div", "ticks");
  b.frames.forEach((_, i) => {
    const t = el("button", "tick" + (i === start ? " on" : ""));
    t.type = "button";
    t.setAttribute("aria-label", "frame " + (i + 1) + ", " + b.frames[i].label);
    t.title = b.frames[i].label;
    t.addEventListener("click", () => go({
      sticky: Object.assign({}, state.sticky, { playing: false }),
      ui: Object.assign({}, state.ui, { ["frame" + at]: i }),
    }));
    ticks.appendChild(t);
  });
  controls.appendChild(ticks);
  const label = el("span", "frame-label");
  controls.appendChild(label);
  box.appendChild(controls);

  const nodes = el("div", "nodes");
  box.appendChild(nodes);
  const note = el("div", "note");
  box.appendChild(note);

  // The nodes are the same nodes throughout, so they are built once and only
  // their state changes. Rebuilding them would throw away the transition that
  // carries the point.
  const cells = new Map();
  for (const n of b.nodes) {
    const cell = el("span", "node");
    cell.appendChild(el("span", "l", n.label));
    cell.appendChild(el("span", "s"));
    nodes.appendChild(cell);
    cells.set(n.id, cell);
  }

  let frame = start;
  const paint = () => {
    const f = b.frames[frame % b.frames.length];
    label.textContent = f.label;
    note.textContent = "";
    note.appendChild(mdInline(f.note || ""));
    for (const s of f.states) {
      const cell = cells.get(s.nodeId);
      if (!cell) continue;
      cell.className = "node " + (s.state || "idle");
      cell.children[1].textContent = s.detail || "";
    }
    [...ticks.children].forEach((t, i) => t.classList.toggle("on", i === frame % b.frames.length));
  };
  paint();

  // durationMs is a hint about this frame rather than a speed for the whole
  // timeline, so the timer ticks often and each frame waits its own time.
  let waited = 0;
  every(200, () => {
    if (!box.isConnected || state.sticky.playing === false) return;
    waited += 200;
    if (waited < (b.frames[frame % b.frames.length].durationMs || 2200)) return;
    waited = 0;
    frame = (frame + 1) % b.frames.length;
    state.ui["frame" + at] = frame;
    paint();
  });
  return box;
}

/* ------------------------------------------------------------------ extension */

/* An extension is content this format has no opinion about, and the deal is
   that it arrives with a sentence a reader who does not know it can print. This
   reader knows none of them, so it prints the sentence and says whose it was.
   An extension without a usable fallback is not an extension, it is a hole. */
function extensionBlock(b) {
  const box = el("div", "ext");
  const head = el("div", "ext-head");
  head.appendChild(el("span", "ext-name", b.name + " v" + b.version));
  head.appendChild(el("span", "ext-why", "this reader does not know this one, so this is what it says instead"));
  box.appendChild(head);
  box.appendChild(mdBlock(b.fallback));
  return box;
}
