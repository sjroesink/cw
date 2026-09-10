/* The cw/1 step: a body, and up to one of each of four blocks in an order this
   file decides. That is the format's shape and this is the last place that
   knows about it.

   Nothing outside a step is in here. Parts, sections, the rail, the overview
   and the progress bar are the same in both versions of the format, so the
   shell draws those and asks this file only what a step looks like. */

import {
  HOSTED, el, mdEl, mdInline, drawMermaid, openDiagram, highlightInto, langFor,
  fileName, openButton, openAt, every,
} from "./ui.js";
import { state, doc, go, setUI, setSticky } from "./state.js";

const ui = () => state.ui;

export default {
  version: "cw/1",

  // The crumb above the title. cw/1 keeps the repository as owner/name and the
  // pull request as whatever the author wanted it to read as.
  head(d) {
    const src = d.source || {};
    return { repo: src.repo, number: src.number, url: src.url, state: src.state };
  },

  partIntro(p) { return { short: p.desc, long: p.long || p.desc }; },
  sectionIntro(s) { return s.desc; },

  // Where the o key and the editor test land. The shell walks the document and
  // asks this about one step at a time.
  stepCode(step) {
    return step.code ? { file: step.code.file, line: step.code.from || 1 } : null;
  },

  step(step, host) {
    // cw/1 has slots where cw/2 has a list, so the slot's name is what a
    // comment hangs on. Same two attributes, same meaning, and the comment
    // layer above this never learns which version it is reading.
    const slot = (node, name, kind) => {
      node.dataset.anchor = name;
      node.dataset.kind = kind;
      host.appendChild(node);
    };
    slot(mdEl("p", "step-body", step.body), "body", "text");

    if (step.diagram) slot(panelDiagram(step.diagram), "diagram", "block");
    if (step.anim) slot(panelAnim(step.anim), "anim", "block");
    if (step.diff) slot(panelDiff(step.diff), "diff", "block");
    if (step.code) slot(panelCode(step.code), "code", "code");
    if (step.callout) {
      const c = el("div", "callout");
      c.appendChild(el("span", "label", "Watch out"));
      c.appendChild(mdEl("span", "txt", step.callout));
      slot(c, "callout", "text");
    }
  },
};

const lang = (file, explicit) => langFor(file, explicit, state.data && doc().language);

/* ------------------------------------------------------------------ diagram */

function panelDiagram(dg) {
  const panel = el("div", "panel");
  const head = el("div", "panel-head");
  head.appendChild(el("span", "kind", dg.kind || "diagram"));
  const refCount = Object.keys(dg.refs || {}).length;
  if (refCount) head.appendChild(el("span", "hint", "click a block for its code"));
  const big = el("button", "tiny", "full size");
  big.type = "button";
  big.title = "the diagram in a window of its own";
  if (!refCount) big.style.marginLeft = "auto";
  big.addEventListener("click", () => openDiagram(dg.def, dg.kind));
  head.appendChild(big);
  const src = el("button", "tiny", ui().source ? "hide mermaid source" : "view mermaid source");
  src.type = "button";
  src.addEventListener("click", () => setUI({ source: !ui().source }));
  head.appendChild(src);
  panel.appendChild(head);

  const host = el("div", "diagram");
  panel.appendChild(host);
  drawMermaid(host, dg.def).then((svg) => { if (svg) wireRefs(host, dg); });

  const open = (dg.refs || {})[ui().ref];
  if (open) panel.appendChild(refPanel(open));
  if (ui().source) {
    const box = el("div", "source");
    box.appendChild(el("pre", null, dg.def));
    panel.appendChild(box);
  }
  if (dg.caption) panel.appendChild(el("div", "caption", dg.caption));
  return panel;
}

// A block named in refs becomes a button. Mermaid gives flowchart nodes an id it
// derives from the source, and everything else only a label, so both are tried.
function wireRefs(host, dg) {
  const refs = dg.refs || {};
  if (!Object.keys(refs).length) return;
  const seen = [];
  const register = (node, k) => {
    if (!node || seen.some((x) => x.node === node)) return;
    node.style.cursor = "pointer";
    node.setAttribute("title", "show the code for " + (refs[k].label || k));
    node.addEventListener("click", (e) => {
      e.stopPropagation();
      setUI({ ref: ui().ref === k ? null : k });
    });
    seen.push({ node, k });
  };
  // Mermaid names a flowchart node after the diagram it is in, so the id reads
  // mmd-3-flowchart-schema-1 and not flowchart-schema-1. Cutting only the
  // documented prefix matched nothing, which is why no shape has ever been
  // clickable despite the hint in the panel head saying so.
  host.querySelectorAll("g.node").forEach((n) => {
    const k = (n.id || "").replace(/^.*flowchart-/, "").replace(/-\d+$/, "");
    if (refs[k]) register(n, k);
  });
  host.querySelectorAll("text, tspan").forEach((t) => {
    const label = (t.textContent || "").trim();
    if (!refs[label]) return;
    register(t.closest("g") || t.parentNode, label);
  });
  for (const { node, k } of seen) {
    const on = ui().ref === k;
    const shape = node.querySelector("rect, polygon, circle, ellipse, path");
    if (shape) {
      shape.style.stroke = on ? "var(--accent)" : "";
      shape.style.strokeWidth = on ? "2.5px" : "";
    }
    node.style.filter = on ? "drop-shadow(0 2px 6px oklch(0.52 0.14 255 / 0.35))" : "";
  }
}

function refPanel(ref) {
  const box = el("div", "refbox");
  const head = el("div", "rhead");
  head.appendChild(el("span", "rlabel", ref.label || ""));
  head.appendChild(fileName(ref.file, ref.from || 1, ref.to, ":" + (ref.from || 1)));
  head.appendChild(el("span", "grow"));
  head.appendChild(openButton(ref.file, ref.from || 1, "open-accent", ref.to));
  const close = el("button", "close-btn", "close");
  close.type = "button";
  close.addEventListener("click", () => setUI({ ref: null }));
  head.appendChild(close);
  box.appendChild(head);

  const lines = el("div", "lines");
  lines.style.borderRadius = "5px";
  lines.style.border = "1px solid var(--code-chrome)";
  lines.style.padding = "12px 0";
  const from = ref.from || 1;
  ref.code.split("\n").forEach((t, i) => {
    const row = el("div", "row");
    const n = el("span", "n link", String(from + i));
    n.title = "open " + ref.file + " at line " + (from + i);
    n.addEventListener("click", () => openAt(ref.file, from + i));
    row.appendChild(n);
    row.appendChild(el("span", "t", t === "" ? " " : t));
    lines.appendChild(row);
  });
  box.appendChild(lines);
  highlightInto(lines, ref.code, lang(ref.file));
  if (ref.note) box.appendChild(mdEl("div", "note", ref.note));
  return box;
}

/* ------------------------------------------------------------------ code */

// Locally this is what the working tree says right now. Hosted it is what the
// publisher's tree said when they published, which is a weaker claim and is
// worded as one.
function checkTag(check) {
  if (!check) return null;
  if (check.state === "ok" || check.state === "unchecked") return null;
  const when = HOSTED ? " when this was published" : " in the tree";
  const label = check.state === "moved" ? "had moved" + when :
    check.state === "gone" ? "was not in the code" + when :
    check.state === "missing-file" ? "file was gone" + when : check.state;
  const tag = el("span", "tag " + (check.state === "moved" ? "moved" : "stale"), label);
  tag.title = check.note || "";
  return tag;
}

function panelCode(code) {
  const wrap = el("div");
  wrap.dataset.file = code.file;
  wrap.style.marginBottom = "28px";

  const notes = code.notes || [];
  const openNote = notes.find((n) => n.line === ui().note);

  const box = el("div", "codebox" + (openNote ? " with-note" : ""));
  const head = el("div", "code-head");
  head.appendChild(fileName(code.file, code.from || 1, code.to));
  if (code.note) head.appendChild(el("span", "tag", code.note));
  const tag = checkTag(code.check);
  if (tag) head.appendChild(tag);
  head.appendChild(el("span", "grow"));
  head.appendChild(openButton(code.file, code.from || 1, "open-dark", code.to));
  box.appendChild(head);

  const first = code.from || 1;
  const hi = new Set(code.hi || []);
  const add = new Set(code.add || []);
  const lines = el("div", "lines");
  code.text.split("\n").forEach((t, i) => {
    const n = first + i;
    const note = notes.find((x) => x.line === n);
    const on = ui().note === n;
    const row = el("div", "row" + (add.has(n) ? " add" : "") + (hi.has(n) ? " hi" : "") + (on ? " open" : "") + (note ? " clickable" : ""));
    row.dataset.line = String(n);

    const num = el("span", "n link", String(n));
    num.title = "open " + code.file + " at line " + n;
    num.addEventListener("click", (e) => { e.stopPropagation(); openAt(code.file, n); });
    row.appendChild(num);

    row.appendChild(el("span", "mark", add.has(n) ? "+" : " "));
    row.appendChild(el("span", "t", t === "" ? " " : t));

    if (note) {
      const q = el("button", "qmark", on ? "×" : "?");
      q.type = "button";
      q.title = "the note on this line";
      row.appendChild(q);
      row.addEventListener("click", () => setUI({ note: on ? null : n }));
    }
    lines.appendChild(row);
  });
  box.appendChild(lines);
  highlightInto(lines, code.text, lang(code.file, code.lang));
  wrap.appendChild(box);

  if (openNote) {
    const np = el("div", "linenote");
    np.appendChild(el("span", "at", "line " + openNote.line));
    np.appendChild(mdEl("span", "txt", openNote.text));
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

/* ------------------------------------------------------------------ diff */

function panelDiff(diff) {
  const split = !!state.sticky.split;
  const wrap = el("div", "diff");
  const head = el("div", "diff-head");
  head.appendChild(fileName(diff.file, diff.from || 1));
  head.appendChild(el("span", "grow"));
  head.appendChild(openButton(diff.file, diff.from || 1, "open-dark"));
  const toggle = el("button", "open-dark", split ? "unified view" : "side-by-side view");
  toggle.type = "button";
  toggle.addEventListener("click", () => setSticky({ split: !split }));
  head.appendChild(toggle);
  wrap.appendChild(head);

  const rowFor = (line) => {
    const row = el("div", "row " + line.kind);
    row.appendChild(el("span", "mark", line.kind === "add" ? "+" : line.kind === "del" ? "-" : " "));
    row.appendChild(el("span", "t", line.t === "" ? " " : line.t));
    return row;
  };

  const text = (list) => list.map((l) => l.t).join("\n");
  if (!split) {
    const body = el("div", "diff-body");
    for (const l of diff.lines) body.appendChild(rowFor(l));
    wrap.appendChild(body);
    highlightInto(body, text(diff.lines), lang(diff.file));
  } else {
    const grid = el("div", "diff-split");
    for (const [label, keep] of [["before", "add"], ["after", "del"]]) {
      const col = el("div");
      col.appendChild(el("div", "side", label));
      const side = diff.lines.filter((l) => l.kind !== keep);
      for (const l of side) col.appendChild(rowFor(l));
      grid.appendChild(col);
      highlightInto(col, text(side), lang(diff.file));
    }
    wrap.appendChild(grid);
  }
  return wrap;
}

/* ------------------------------------------------------------------ anim */

function panelAnim(anim) {
  const playing = state.sticky.playing !== false;
  const box = el("div", "anim");
  const controls = el("div", "controls");
  const play = el("button", "tiny", playing ? "pause" : "play");
  play.type = "button";
  play.addEventListener("click", () => setSticky({ playing: !playing }));
  controls.appendChild(play);

  const frame = () => (ui().frame || 0) % anim.frames.length;
  const ticks = el("div", "ticks");
  anim.frames.forEach((_, i) => {
    const t = el("button", "tick" + (i === frame() ? " on" : ""));
    t.type = "button";
    t.setAttribute("aria-label", "frame " + (i + 1));
    // One move, one repaint: pausing and jumping are the same click.
    t.addEventListener("click", () => go({
      sticky: Object.assign({}, state.sticky, { playing: false }),
      ui: Object.assign({}, state.ui, { frame: i }),
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

  // The frames are the same nodes changing state, so they are patched in place.
  // Rebuilding them would throw away the transition that carries the point.
  let at = frame();
  const paint = () => {
    const f = anim.frames[at % anim.frames.length];
    label.textContent = f.label;
    note.textContent = "";
    note.appendChild(mdInline(f.note || ""));
    f.nodes.forEach((n, i) => {
      let node = nodes.children[i];
      if (!node) {
        node = el("span", "node");
        node.appendChild(el("span", "l"));
        node.appendChild(el("span", "s"));
        nodes.appendChild(node);
      }
      node.className = "node " + (n.state || "idle");
      node.children[0].textContent = n.label;
      node.children[1].textContent = n.sub || "";
    });
    while (nodes.children.length > f.nodes.length) nodes.lastChild.remove();
    [...ticks.children].forEach((t, i) => t.classList.toggle("on", i === at % anim.frames.length));
  };
  paint();

  // The timer belongs to this box. The shell clears it before it draws the next
  // screen, so nothing keeps running for a step nobody is looking at.
  every(2200, () => {
    if (!box.isConnected || state.sticky.playing === false) return;
    at = (at + 1) % anim.frames.length;
    // Written back without a repaint, so pausing freezes the frame on screen
    // rather than starting the run again from the first one.
    state.ui.frame = at;
    paint();
  });
  return box;
}
