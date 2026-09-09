/* code-walkthrough: the page. It holds no topic of its own. Everything it shows
   comes from one JSON file the server hands over, and everything it can do to
   the machine (open a file, save a setting) goes back through the server. */

const $ = (id) => document.getElementById(id);
const TOKEN = (window.CW && window.CW.token) || "";

const state = {
  data: null,
  view: "overview",
  part: null,
  section: null,
  step: 0,
  done: [],
  note: null,
  ref: null,
  source: false,
  split: false,
  frame: 0,
  playing: true,
  stamp: "",
};

let animTimer = 0;
let drawn = { def: null, theme: null };

/* ------------------------------------------------------------------ plumbing */

async function api(path, init) {
  const opts = Object.assign({ headers: {} }, init || {});
  opts.headers = Object.assign({ "X-Cw-Token": TOKEN }, opts.headers);
  if (opts.body) opts.headers["Content-Type"] = "application/json";
  const res = await fetch(path, opts);
  const ct = res.headers.get("content-type") || "";
  const body = ct.includes("json") ? await res.json() : await res.text();
  if (!res.ok && typeof body === "string") throw new Error(body);
  return body;
}

let toastTimer = 0;
function toast(msg, bad) {
  const t = $("toast");
  t.textContent = msg;
  t.classList.toggle("bad", !!bad);
  t.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { t.hidden = true; }, bad ? 7000 : 3200);
}

function el(tag, cls, text) {
  const n = document.createElement(tag);
  if (cls) n.className = cls;
  if (text !== undefined && text !== null) n.textContent = text;
  return n;
}

function pad2(n) { return String(n).padStart(2, "0"); }
function plural(n, word) { return n + " " + word + (n === 1 ? "" : "s"); }

/* ------------------------------------------------------------------ the data */

const doc = () => state.data.doc;
const parts = () => doc().parts || [];
const partAt = (p) => parts()[p];
const sectionAt = (p, s) => (partAt(p) ? partAt(p).sections[s] : null);
const stepsOf = (p, s) => (sectionAt(p, s) ? sectionAt(p, s).steps : []);

function currentStep() {
  if (state.view !== "step" || state.part === null || state.section === null) return null;
  return stepsOf(state.part, state.section)[state.step] || null;
}

function totalSteps() {
  return parts().reduce((a, p) => a + p.sections.reduce((b, s) => b + s.steps.length, 0), 0);
}
function stepsIn(p) {
  return p.sections.reduce((a, s) => a + s.steps.length, 0);
}

const key = (p, s, i) => p + "." + s + "." + i;
const isDone = (p, s, i) => state.done.indexOf(key(p, s, i)) !== -1;
const sectionComplete = (p, s) => stepsOf(p, s).every((_, i) => isDone(p, s, i));
const partComplete = (p) => partAt(p).sections.every((_, s) => sectionComplete(p, s));

/* ------------------------------------------------------------------ storage */

function storeKey() {
  return "cw:progress:" + ((state.data && state.data.file) || "unknown");
}

function loadProgress() {
  try {
    const raw = localStorage.getItem(storeKey());
    if (!raw) return;
    const saved = JSON.parse(raw);
    state.done = Array.isArray(saved.done) ? saved.done : [];
  } catch { /* a fresh browser is a fresh start, which is fine */ }
}

function saveProgress() {
  try {
    localStorage.setItem(storeKey(), JSON.stringify({ done: state.done }));
  } catch { /* private windows refuse, and nothing here is worth failing over */ }
}

/* ------------------------------------------------------------------ routing */

function readHash() {
  const h = (location.hash || "").replace(/^#/, "");
  if (!h) { state.view = "overview"; state.part = null; state.section = null; return; }
  const m = h.match(/^(\d+)(?:-(\d+))?(?:-(\d+))?$/);
  if (!m) return;
  const p = Number(m[1]) - 1;
  if (!partAt(p)) return;
  if (m[2] === undefined) { state.view = "part"; state.part = p; state.section = null; return; }
  const s = Number(m[2]) - 1;
  if (!sectionAt(p, s)) return;
  const i = m[3] === undefined ? 0 : Number(m[3]) - 1;
  state.view = "step";
  state.part = p;
  state.section = s;
  state.step = Math.max(0, Math.min(i, stepsOf(p, s).length - 1));
}

function writeHash() {
  let h = "";
  if (state.view === "part") h = "#" + (state.part + 1);
  if (state.view === "step") h = "#" + (state.part + 1) + "-" + (state.section + 1) + "-" + (state.step + 1);
  if (location.hash !== h) history.replaceState(null, "", h || location.pathname + location.search);
}

function go(next) {
  Object.assign(state, next);
  saveProgress();
  writeHash();
  render();
}

function openStep(p, s, i) {
  go({ view: "step", part: p, section: s, step: i, note: null, ref: null, source: false, frame: 0 });
}

function markDone(p, s, i, on) {
  const k = key(p, s, i);
  const done = state.done.filter((x) => x !== k);
  if (on) done.push(k);
  go({ done });
}

// Every screen in one order: the overview, then each part page with its steps
// behind it. Both arrows walk that list and wrap at the ends, so the reader can
// hold one key from the first screen to the last and land back on the overview.
function pages() {
  const out = [{ view: "overview", part: null, section: null, step: 0 }];
  parts().forEach((p, pi) => {
    out.push({ view: "part", part: pi, section: null, step: 0 });
    p.sections.forEach((sec, si) => {
      sec.steps.forEach((_, i) => out.push({ view: "step", part: pi, section: si, step: i }));
    });
  });
  return out;
}

function pageAhead(dir) {
  const list = pages();
  const at = list.findIndex((x) => x.view === state.view && x.part === state.part &&
    (x.view !== "step" || (x.section === state.section && x.step === state.step)));
  return list[((at < 0 ? 0 : at) + dir + list.length) % list.length];
}

// The buttons say where they land, because "next" on the last step of a part
// would otherwise look like it stays inside that part.
function moveLabel(dir) {
  const to = pageAhead(dir);
  let what = dir > 0 ? "Next" : "Previous";
  if (to.view === "overview") what = "Overview";
  else if (to.view === "part") what = "Part " + pad2(to.part + 1);
  else if (state.view === "overview" && dir < 0) what = "Last step";
  return dir > 0 ? what + " →" : "← " + what;
}

function move(dir) {
  if (dir > 0 && state.view === "step") {
    const k = key(state.part, state.section, state.step);
    if (state.done.indexOf(k) === -1) state.done = state.done.concat([k]);
  }
  go(Object.assign({ note: null, ref: null, source: false, frame: 0 }, pageAhead(dir)));
}

/* ------------------------------------------------------------------ theme */

function applyTheme() {
  const s = state.data.settings;
  const root = document.documentElement;
  if (s.theme === "light" || s.theme === "dark") root.dataset.theme = s.theme;
  else delete root.dataset.theme;
  if (s.accent) root.style.setProperty("--accent", s.accent);
}

// The theme lives in settings. This is the shortcut for it, and it saves the
// same way the sheet does, so the next start comes up in the theme you left.
async function toggleTheme() {
  state.data.settings.theme = effectiveDark() ? "light" : "dark";
  applyTheme();
  drawn = { def: null, theme: null };
  render();
  try { await api("/api/settings", { method: "PUT", body: JSON.stringify(state.data.settings) }); }
  catch { /* the page still switched */ }
}

function effectiveDark() {
  const set = document.documentElement.dataset.theme;
  if (set) return set === "dark";
  return window.matchMedia("(prefers-color-scheme: dark)").matches;
}

/* ------------------------------------------------------------------ mermaid */

const MERMAID_LIGHT = {
  fontSize: "13px", background: "#ffffff", primaryColor: "#f6f8f6", primaryBorderColor: "#dde3df",
  primaryTextColor: "#242b27", secondaryColor: "#eceefc", lineColor: "#9aa5a0", textColor: "#3a423d",
  actorBkg: "#f6f8f6", actorBorder: "#dde3df", actorTextColor: "#242b27", signalColor: "#5a6560",
  signalTextColor: "#3a423d", sequenceNumberColor: "#ffffff", clusterBkg: "#fbfcfb", clusterBorder: "#e4e9e6",
  noteBkgColor: "#eceefc", noteBorderColor: "#c3c9f2", mainBkg: "#f6f8f6", nodeTextColor: "#242b27",
  edgeLabelBackground: "#ffffff", labelBackground: "#ffffff",
};

const MERMAID_DARK = {
  fontSize: "13px", background: "#242424", primaryColor: "#2f2f2f", primaryBorderColor: "#464646",
  primaryTextColor: "#ececec", secondaryColor: "#333747", lineColor: "#8b8b8b", textColor: "#d2d2d2",
  actorBkg: "#2f2f2f", actorBorder: "#464646", actorTextColor: "#ececec", signalColor: "#a4a4a4",
  signalTextColor: "#d2d2d2", sequenceNumberColor: "#1f1f1f", clusterBkg: "#2a2a2a", clusterBorder: "#3e3e3e",
  noteBkgColor: "#333747", noteBorderColor: "#565f8e", mainBkg: "#2f2f2f", nodeTextColor: "#ececec",
  edgeLabelBackground: "#242424", labelBackground: "#242424",
};

let mermaidPromise = null;

/* The bundle is served from this origin, cached on disk by the server, so this
   works offline after the first run. When it cannot be had at all the step
   shows the diagram source instead, which is still the thing being described. */
function mermaid() {
  if (mermaidPromise) return mermaidPromise;
  mermaidPromise = new Promise((resolve, reject) => {
    const s = document.createElement("script");
    s.src = "/vendor/mermaid.js";
    s.onload = () => (window.mermaid ? resolve(window.mermaid) : reject(new Error("mermaid did not register")));
    s.onerror = () => reject(new Error("mermaid could not be loaded"));
    document.head.appendChild(s);
  }).then((m) => {
    m.initialize({ startOnLoad: false, securityLevel: "loose", theme: "base", fontFamily: "'JetBrains Mono', monospace" });
    return m;
  });
  return mermaidPromise;
}

/* ------------------------------------------------------------------ highlighting */

/* The bundle is vendored like mermaid, so this works offline after the first
   run. Highlighting happens after the rows are in the DOM: the gutter, the line
   notes and the add markers are the page's own, and only the text is repainted. */
let hljsPromise = null;

function hljs() {
  if (hljsPromise) return hljsPromise;
  hljsPromise = new Promise((resolve) => {
    const s = document.createElement("script");
    s.src = "/vendor/hljs.js";
    s.onload = () => resolve(window.hljs || null);
    s.onerror = () => resolve(null);
    document.head.appendChild(s);
  });
  return hljsPromise;
}

// Only what the bundle actually carries. An unknown extension stays plain text,
// because guessing the language of a snippet is how a comment turns into a string.
const LANGS = {
  ts: "typescript", tsx: "typescript", mts: "typescript", cts: "typescript",
  js: "javascript", jsx: "javascript", mjs: "javascript", cjs: "javascript",
  cs: "csharp", go: "go", py: "python", rb: "ruby", java: "java", kt: "kotlin",
  rs: "rust", php: "php", swift: "swift", m: "objectivec", pl: "perl", lua: "lua",
  c: "c", h: "c", cpp: "cpp", hpp: "cpp", cc: "cpp", cxx: "cpp",
  sql: "sql", json: "json", jsonc: "json", yml: "yaml", yaml: "yaml",
  xml: "xml", html: "xml", htm: "xml", csproj: "xml", props: "xml", xaml: "xml",
  css: "css", scss: "scss", less: "less", sh: "bash", bash: "bash", zsh: "bash",
  md: "markdown", markdown: "markdown", r: "r", vb: "vbnet", ini: "ini",
  makefile: "makefile", graphql: "graphql", gql: "graphql", diff: "diff",
};

function langFor(file, explicit) {
  const want = explicit || (state.data && doc().language) || "";
  if (want) return LANGS[want.toLowerCase()] || want.toLowerCase();
  const name = String(file || "").split("/").pop().toLowerCase();
  if (name === "makefile" || name === "dockerfile") return LANGS[name] || null;
  const ext = name.includes(".") ? name.split(".").pop() : "";
  return LANGS[ext] || null;
}

/* hljs hands back one blob of HTML with nested spans, and the page needs it per
   line. This walks the tree and cuts it at every newline, carrying the classes
   that were open across the cut. */
function tokenLines(lib, text, lang) {
  if (!lib || !lang || !lib.getLanguage(lang)) return null;
  let html;
  try {
    html = lib.highlight(text, { language: lang, ignoreIllegals: true }).value;
  } catch {
    return null;
  }
  const tpl = document.createElement("template");
  tpl.innerHTML = html;
  const lines = [[]];
  const walk = (node, cls) => {
    for (const child of node.childNodes) {
      if (child.nodeType === 3) {
        const parts = child.nodeValue.split("\n");
        parts.forEach((p, i) => {
          if (i) lines.push([]);
          if (p) lines[lines.length - 1].push({ t: p, c: cls });
        });
      } else if (child.nodeType === 1) {
        walk(child, (cls ? cls + " " : "") + (child.getAttribute("class") || ""));
      }
    }
  };
  walk(tpl.content, "");
  return lines;
}

function highlightInto(host, text, file, explicitLang) {
  const lang = langFor(file, explicitLang);
  if (!lang) return;
  hljs().then((lib) => {
    if (!host.isConnected) return;
    const lines = tokenLines(lib, text, lang);
    if (!lines) return;
    const spans = host.querySelectorAll(".t");
    if (spans.length !== lines.length) return;
    lines.forEach((parts, i) => {
      const span = spans[i];
      if (!parts.length) return;
      span.textContent = "";
      for (const part of parts) {
        if (!part.c) { span.appendChild(document.createTextNode(part.t)); continue; }
        const sp = document.createElement("span");
        sp.className = part.c;
        sp.textContent = part.t;
        span.appendChild(sp);
      }
    });
  });
}

let seq = 0;

async function drawDiagram(host, dg) {
  const theme = effectiveDark() ? "dark" : "light";
  drawn = { def: dg.def, theme };
  let m;
  try {
    m = await mermaid();
  } catch {
    const pre = el("pre", "fallback", dg.def);
    host.textContent = "";
    host.appendChild(pre);
    return;
  }
  m.initialize({
    startOnLoad: false, securityLevel: "loose", theme: "base",
    fontFamily: "'JetBrains Mono', monospace",
    themeVariables: theme === "dark" ? MERMAID_DARK : MERMAID_LIGHT,
  });
  try {
    const res = await m.render("mmd-" + ++seq, dg.def);
    if (drawn.def !== dg.def || !host.isConnected) return;
    host.innerHTML = res.svg;
    const svg = host.querySelector("svg");
    if (svg) { svg.style.maxWidth = "100%"; svg.style.height = "auto"; }
    wireRefs(host, dg);
  } catch (err) {
    host.textContent = "";
    host.appendChild(el("pre", "fallback", dg.def));
    toast("The diagram did not render: " + (err && err.message ? err.message : err), true);
  }
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
      go({ ref: state.ref === k ? null : k });
    });
    seen.push({ node, k });
  };
  host.querySelectorAll("g.node").forEach((n) => {
    const k = (n.id || "").replace(/^flowchart-/, "").replace(/-\d+$/, "");
    if (refs[k]) register(n, k);
  });
  host.querySelectorAll("text, tspan").forEach((t) => {
    const label = (t.textContent || "").trim();
    if (!refs[label]) return;
    register(t.closest("g") || t.parentNode, label);
  });
  for (const { node, k } of seen) {
    const on = state.ref === k;
    const shape = node.querySelector("rect, polygon, circle, ellipse, path");
    if (shape) {
      shape.style.stroke = on ? "var(--accent)" : "";
      shape.style.strokeWidth = on ? "2.5px" : "";
    }
    node.style.filter = on ? "drop-shadow(0 2px 6px oklch(0.52 0.14 255 / 0.35))" : "";
  }
}

/* ------------------------------------------------------------------ chrome */

function renderChrome() {
  const d = doc();
  const src = d.source || {};
  document.title = d.title + " · walkthrough";
  $("title").textContent = d.title;
  $("repo").textContent = src.repo || state.data.rootName || "";
  $("slash").hidden = !(src.repo && src.number);

  const num = $("number");
  num.textContent = src.number || "";
  if (src.url) { num.href = src.url; num.removeAttribute("aria-disabled"); }
  else { num.removeAttribute("href"); }

  $("state").textContent = src.state || "";
  $("state").hidden = !src.state;

  const total = totalSteps();
  $("progressLabel").textContent = state.done.length + " / " + total;
  $("progressFill").style.width = total ? Math.round((state.done.length / total) * 100) + "%" : "0";
}

function problems() {
  const d = state.data;
  const box = $("problems");
  const errs = d.errors || [], warns = d.warnings || [];
  const trouble = [];
  if (d.moved) trouble.push(d.moved + " snippet(s) still exist but have moved to another line");
  if (d.stale) trouble.push(d.stale + " snippet(s) are no longer in the working tree");
  if (!errs.length && !warns.length && !trouble.length) { box.hidden = true; return; }
  const list = [...errs, ...trouble, ...warns].slice(0, 20);
  box.innerHTML = "";
  box.appendChild(el("b", null, errs.length ? "This walkthrough has errors" : "Worth a look"));
  const ul = el("ul");
  for (const line of list) ul.appendChild(el("li", null, line));
  box.appendChild(ul);
  box.hidden = false;
}

function renderRail() {
  const box = $("railItems");
  box.textContent = "";
  $("btnHome").classList.toggle("on", state.view === "overview");

  parts().forEach((p, pi) => {
    const active = state.part === pi;
    const b = el("button", "part" + (active ? " on" : "") + (active && state.view === "step" ? " instep" : ""));
    b.type = "button";
    b.appendChild(el("span", "badge", pad2(pi + 1)));
    b.appendChild(el("span", "label", p.title));
    if (partComplete(pi)) b.appendChild(el("span", "tick", "✓"));
    b.addEventListener("click", () => go({ view: "part", part: pi, section: null }));
    box.appendChild(b);

    if (!active) return;
    p.sections.forEach((s, si) => {
      const on = state.section === si && state.view === "step";
      const sb = el("button", "section" + (on ? " on" : ""));
      sb.type = "button";
      sb.appendChild(el("span", "label", s.title));
      if (sectionComplete(pi, si)) sb.appendChild(el("span", "tick", "✓"));
      sb.addEventListener("click", () => openStep(pi, si, 0));
      box.appendChild(sb);
    });
  });
}

/* ------------------------------------------------------------------ views */

function render() {
  renderChrome();
  renderRail();
  const stage = $("stage");
  stage.textContent = "";
  if (state.view === "overview") stage.appendChild(viewOverview());
  else if (state.view === "part") stage.appendChild(viewPart());
  else stage.appendChild(viewStep());
  window.scrollTo({ top: 0 });
  armAnim();
}

function viewOverview() {
  const d = doc();
  const v = el("div", "view");
  v.appendChild(el("div", "eyebrow", "This walkthrough has " + plural(parts().length, "part")));
  if (d.summary) v.appendChild(el("p", "lede", d.summary));

  const grid = el("div", "cards");
  parts().forEach((p, pi) => {
    const card = el("button", "card");
    card.type = "button";
    const head = el("div", "head");
    head.appendChild(el("span", "no", "part " + pad2(pi + 1)));
    head.appendChild(el("span", "counts",
      plural(p.sections.length, "section") + " · " + plural(stepsIn(p), "step")));
    card.appendChild(head);
    card.appendChild(el("div", "title", p.title));
    if (p.desc) card.appendChild(el("div", "desc", p.desc));
    if (p.files && p.files.length) {
      const chips = el("div", "chips");
      for (const f of p.files) chips.appendChild(el("span", "chip", f));
      card.appendChild(chips);
    }
    card.addEventListener("click", () => go({ view: "part", part: pi, section: null }));
    grid.appendChild(card);
  });
  v.appendChild(grid);
  v.appendChild(pageFoot());
  return v;
}

function trail(bits) {
  const t = el("div", "trail");
  bits.forEach((b, i) => {
    if (i) t.appendChild(el("span", "sep", "→"));
    if (b.onClick) {
      const btn = el("button", null, b.text);
      btn.type = "button";
      btn.addEventListener("click", b.onClick);
      t.appendChild(btn);
    } else {
      t.appendChild(el("span", null, b.text));
    }
  });
  return t;
}

function viewPart() {
  const p = partAt(state.part);
  const v = el("div", "view");
  v.appendChild(trail([
    { text: "Overview", onClick: () => go({ view: "overview", part: null, section: null }) },
    { text: "part " + pad2(state.part + 1) },
  ]));
  v.appendChild(el("h2", "part-title", p.title));
  if (p.long || p.desc) v.appendChild(el("p", "part-long", p.long || p.desc));

  const list = el("div", "sections");
  p.sections.forEach((s, si) => {
    const row = el("button", "section-row");
    row.type = "button";
    row.appendChild(el("span", "no", pad2(si + 1)));
    const mid = el("span", "mid");
    mid.appendChild(el("span", "t", s.title));
    if (s.desc) mid.appendChild(el("span", "d", s.desc));
    row.appendChild(mid);
    row.appendChild(el("span", "counts", plural(s.steps.length, "step")));
    row.appendChild(el("span", "arrow", "→"));
    row.addEventListener("click", () => openStep(state.part, si, 0));
    list.appendChild(row);
  });
  v.appendChild(list);
  v.appendChild(pageFoot());
  return v;
}

function viewStep() {
  const p = partAt(state.part);
  const sec = sectionAt(state.part, state.section);
  const step = currentStep();
  const v = el("div", "view");

  v.appendChild(trail([
    { text: "Overview", onClick: () => go({ view: "overview", part: null, section: null }) },
    { text: p.title, onClick: () => go({ view: "part", section: null }) },
    { text: sec.title },
  ]));

  const dots = el("div", "dots");
  sec.steps.forEach((_, i) => {
    const b = el("button", "dot-btn" + (i === state.step ? " on" : isDone(state.part, state.section, i) ? " done" : ""), String(i + 1));
    b.type = "button";
    b.title = sec.steps[i].title;
    b.addEventListener("click", () => openStep(state.part, state.section, i));
    dots.appendChild(b);
  });
  dots.appendChild(el("span", "grow"));
  dots.appendChild(el("span", "counter", "step " + (state.step + 1) + " of " + sec.steps.length));
  v.appendChild(dots);

  v.appendChild(el("h2", "step-title", step.title));
  v.appendChild(el("p", "step-body", step.body));

  if (step.diagram) v.appendChild(panelDiagram(step.diagram));
  if (step.anim) v.appendChild(panelAnim(step.anim));
  if (step.diff) v.appendChild(panelDiff(step.diff));
  if (step.code) v.appendChild(panelCode(step.code));
  if (step.callout) {
    const c = el("div", "callout");
    c.appendChild(el("span", "label", "Watch out"));
    c.appendChild(el("span", "txt", step.callout));
    v.appendChild(c);
  }

  v.appendChild(stepFoot());
  return v;
}

function stepFoot() {
  const foot = el("div", "stepfoot");
  const prev = el("button", "nav", moveLabel(-1));
  prev.type = "button";
  prev.addEventListener("click", () => move(-1));
  foot.appendChild(prev);

  const done = isDone(state.part, state.section, state.step);
  const mark = el("button", "nav" + (done ? " done" : ""), done ? "✓ Marked as read" : "Mark as read");
  mark.type = "button";
  mark.addEventListener("click", () => markDone(state.part, state.section, state.step, !done));
  foot.appendChild(mark);

  foot.appendChild(el("span", "keys", "← → keys"));

  const next = el("button", "nav next", moveLabel(1));
  next.type = "button";
  next.addEventListener("click", () => move(1));
  foot.appendChild(next);
  return foot;
}

// The overview and the part pages get the same two buttons, so the arrow keys
// have something visible behind them wherever the reader is.
function pageFoot() {
  const foot = el("div", "stepfoot");
  const prev = el("button", "nav", moveLabel(-1));
  prev.type = "button";
  prev.addEventListener("click", () => move(-1));
  foot.appendChild(prev);
  foot.appendChild(el("span", "keys", "← → keys"));
  const next = el("button", "nav next", moveLabel(1));
  next.type = "button";
  next.addEventListener("click", () => move(1));
  foot.appendChild(next);
  return foot;
}

/* ------------------------------------------------------------------ panels */

function panelDiagram(dg) {
  const panel = el("div", "panel");
  const head = el("div", "panel-head");
  head.appendChild(el("span", "kind", dg.kind || "diagram"));
  const refCount = Object.keys(dg.refs || {}).length;
  if (refCount) head.appendChild(el("span", "hint", "click a block for its code"));
  const src = el("button", "tiny", state.source ? "hide mermaid source" : "view mermaid source");
  src.type = "button";
  if (!refCount) src.style.marginLeft = "auto";
  src.addEventListener("click", () => go({ source: !state.source }));
  head.appendChild(src);
  panel.appendChild(head);

  const host = el("div", "diagram");
  panel.appendChild(host);
  drawDiagram(host, dg);

  const open = (dg.refs || {})[state.ref];
  if (open) panel.appendChild(refPanel(open));
  if (state.source) {
    const box = el("div", "source");
    const pre = el("pre", null, dg.def);
    box.appendChild(pre);
    panel.appendChild(box);
  }
  if (dg.caption) panel.appendChild(el("div", "caption", dg.caption));
  return panel;
}

function refPanel(ref) {
  const box = el("div", "refbox");
  const head = el("div", "rhead");
  head.appendChild(el("span", "rlabel", ref.label || ""));
  head.appendChild(el("span", "where", ref.file + ":" + (ref.from || 1)));
  head.appendChild(el("span", "grow"));
  head.appendChild(openButton(ref.file, ref.from || 1, "open-accent"));
  const close = el("button", "close-btn", "close");
  close.type = "button";
  close.addEventListener("click", () => go({ ref: null }));
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
  highlightInto(lines, ref.code, ref.file);
  if (ref.note) box.appendChild(el("div", "note", ref.note));
  return box;
}

function checkTag(check) {
  if (!check) return null;
  if (check.state === "ok" || check.state === "unchecked") return null;
  const label = check.state === "moved" ? "moved in the tree" :
    check.state === "gone" ? "no longer in the tree" :
    check.state === "missing-file" ? "file is gone" : check.state;
  const tag = el("span", "tag " + (check.state === "moved" ? "moved" : "stale"), label);
  tag.title = check.note || "";
  return tag;
}

function panelCode(code) {
  const wrap = el("div");
  wrap.style.marginBottom = "28px";

  const notes = code.notes || [];
  const openNote = notes.find((n) => n.line === state.note);

  const box = el("div", "codebox" + (openNote ? " with-note" : ""));
  const head = el("div", "code-head");
  head.appendChild(el("span", "file", code.file));
  if (code.note) head.appendChild(el("span", "tag", code.note));
  const tag = checkTag(code.check);
  if (tag) head.appendChild(tag);
  head.appendChild(el("span", "grow"));
  head.appendChild(openButton(code.file, code.from || 1, "open-dark"));
  box.appendChild(head);

  const first = code.from || 1;
  const hi = new Set(code.hi || []);
  const add = new Set(code.add || []);
  const lines = el("div", "lines");
  code.text.split("\n").forEach((t, i) => {
    const n = first + i;
    const note = notes.find((x) => x.line === n);
    const on = state.note === n;
    const row = el("div", "row" + (add.has(n) ? " add" : "") + (hi.has(n) ? " hi" : "") + (on ? " open" : "") + (note ? " clickable" : ""));

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
      row.addEventListener("click", () => go({ note: on ? null : n }));
    }
    lines.appendChild(row);
  });
  box.appendChild(lines);
  highlightInto(lines, code.text, code.file, code.lang);
  wrap.appendChild(box);

  if (openNote) {
    const np = el("div", "linenote");
    np.appendChild(el("span", "at", "line " + openNote.line));
    np.appendChild(el("span", "txt", openNote.text));
    const x = el("button", "x", "×");
    x.type = "button";
    x.addEventListener("click", () => go({ note: null }));
    np.appendChild(x);
    wrap.appendChild(np);
  } else if (notes.length) {
    wrap.appendChild(el("div", "notes-hint",
      "click a marked line for the note behind it · " + notes.length + " on this snippet"));
  }
  return wrap;
}

function panelDiff(diff) {
  const wrap = el("div", "diff");
  const head = el("div", "diff-head");
  head.appendChild(el("span", "file", diff.file));
  head.appendChild(el("span", "grow"));
  head.appendChild(openButton(diff.file, diff.from || 1, "open-dark"));
  const toggle = el("button", "open-dark", state.split ? "unified view" : "side-by-side view");
  toggle.type = "button";
  toggle.addEventListener("click", () => go({ split: !state.split }));
  head.appendChild(toggle);
  wrap.appendChild(head);

  const rowFor = (line) => {
    const row = el("div", "row " + line.kind);
    row.appendChild(el("span", "mark", line.kind === "add" ? "+" : line.kind === "del" ? "-" : " "));
    row.appendChild(el("span", "t", line.t === "" ? " " : line.t));
    return row;
  };

  const text = (list) => list.map((l) => l.t).join("\n");
  if (!state.split) {
    const body = el("div", "diff-body");
    for (const l of diff.lines) body.appendChild(rowFor(l));
    wrap.appendChild(body);
    highlightInto(body, text(diff.lines), diff.file);
  } else {
    const grid = el("div", "diff-split");
    for (const [label, keep] of [["before", "add"], ["after", "del"]]) {
      const col = el("div");
      col.appendChild(el("div", "side", label));
      const side = diff.lines.filter((l) => l.kind !== keep);
      for (const l of side) col.appendChild(rowFor(l));
      grid.appendChild(col);
      highlightInto(col, text(side), diff.file);
    }
    wrap.appendChild(grid);
  }
  return wrap;
}

function panelAnim(anim) {
  const box = el("div", "anim");
  const controls = el("div", "controls");
  const play = el("button", "tiny", state.playing ? "pause" : "play");
  play.type = "button";
  play.addEventListener("click", () => go({ playing: !state.playing }));
  controls.appendChild(play);

  const ticks = el("div", "ticks");
  anim.frames.forEach((_, i) => {
    const t = el("button", "tick" + (i === state.frame % anim.frames.length ? " on" : ""));
    t.type = "button";
    t.setAttribute("aria-label", "frame " + (i + 1));
    t.addEventListener("click", () => go({ frame: i, playing: false }));
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
  box._paint = () => {
    const f = anim.frames[state.frame % anim.frames.length];
    label.textContent = f.label;
    note.textContent = f.note || "";
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
    [...ticks.children].forEach((t, i) => {
      t.classList.toggle("on", i === state.frame % anim.frames.length);
    });
  };
  box._paint();
  return box;
}

function armAnim() {
  clearInterval(animTimer);
  const step = currentStep();
  if (!step || !step.anim) return;
  animTimer = setInterval(() => {
    if (!state.playing) return;
    const box = document.querySelector(".anim");
    if (!box || !box._paint) return;
    state.frame = (state.frame + 1) % step.anim.frames.length;
    box._paint();
  }, 2200);
}

/* ------------------------------------------------------------------ opening */

function openButton(file, line, cls) {
  const b = el("button", cls, "open in IDE ↗");
  b.type = "button";
  b.title = "open " + file + " at line " + line + " in " + whereOpens();
  b.addEventListener("click", () => openAt(file, line));
  return b;
}

function whereOpens() {
  const ide = state.data.settings.ide;
  return !ide || ide === "auto" ? "your editor" : ide;
}

async function openAt(file, line) {
  try {
    const r = await api("/api/open", { method: "POST", body: JSON.stringify({ file, line, col: 1 }) });
    if (r.ok) toast((r.message ? r.message + ". " : "") + file + ":" + line, !!r.message);
    else toast(r.message || "The editor did not start", true);
  } catch (err) {
    toast(String(err.message || err), true);
  }
}

/* ------------------------------------------------------------------ settings */

function openSettings() {
  const s = state.data.settings;
  const sel = $("setIde");
  sel.textContent = "";
  sel.appendChild(new Option("Detect automatically", "auto", false, s.ide === "auto"));
  for (const ide of state.data.ides) {
    const label = ide.name + (ide.available ? "" : " (not found on this machine)") +
      (ide.hinted ? ", this terminal runs inside it" : "");
    sel.appendChild(new Option(label, ide.id, false, s.ide === ide.id));
  }
  $("setIdeCommand").value = s.ideCommand || "";
  $("setIdePath").value = s.idePath || "";
  $("setTheme").value = s.theme || "auto";
  $("settingsPath").textContent = state.data.settingsPath || "";

  const sw = $("swatches");
  sw.textContent = "";
  for (const a of ["oklch(0.52 0.14 255)", "oklch(0.58 0.14 25)", "oklch(0.5 0.11 190)", "oklch(0.48 0.12 300)"]) {
    const b = el("button", "swatch" + (a === s.accent ? " on" : ""));
    b.type = "button";
    b.style.background = a;
    b.setAttribute("aria-label", a);
    b.addEventListener("click", () => {
      state.data.settings.accent = a;
      applyTheme();
      [...sw.children].forEach((c) => c.classList.toggle("on", c === b));
    });
    sw.appendChild(b);
  }
  syncIdeHint();
  $("settings").showModal();
}

function syncIdeHint() {
  const id = $("setIde").value;
  $("wrapIdeCommand").hidden = id !== "custom";
  const ide = state.data.ides.find((x) => x.id === id);
  let hint = "";
  if (id === "auto") hint = "Picks the editor this server was started from, otherwise the first one it finds.";
  else if (ide && !ide.available && id !== "custom") hint = "Not on PATH. Install its shell command or fill in the path below; some editors still answer through their URL handler.";
  else if (ide && ide.noLine) hint = "Opens the file, but has no switch for jumping to a line.";
  else if (ide && ide.path) hint = ide.path;
  $("ideHint").textContent = hint;
}

async function saveSettings() {
  const s = Object.assign({}, state.data.settings, {
    ide: $("setIde").value,
    ideCommand: $("setIdeCommand").value.trim(),
    idePath: $("setIdePath").value.trim(),
    theme: $("setTheme").value,
  });
  try {
    const r = await api("/api/settings", { method: "PUT", body: JSON.stringify(s) });
    state.data.settings = r.settings;
    state.data.settingsPath = r.path;
    applyTheme();
    render();
    toast("Saved to " + r.path);
  } catch (err) {
    toast("Could not save: " + err.message, true);
  }
}

/* ------------------------------------------------------------------ start */

async function load() {
  const data = await api("/api/walkthrough");
  if (data.fatal) {
    $("boot").innerHTML = "";
    const box = el("div");
    box.style.maxWidth = "620px";
    box.style.textAlign = "center";
    box.appendChild(el("b", null, "The walkthrough cannot be read."));
    box.appendChild(el("p", null, data.fatal));
    $("boot").appendChild(box);
    return false;
  }
  state.data = data;
  state.stamp = data.stamp;
  loadProgress();
  readHash();
  applyTheme();

  $("boot").hidden = true;
  document.querySelector(".top").hidden = false;
  document.querySelector(".layout").hidden = false;
  problems();
  render();
  return true;
}

function wire() {
  $("btnHome").addEventListener("click", () => go({ view: "overview", part: null, section: null }));
  $("btnReload").addEventListener("click", () => location.reload());
  $("btnSettings").addEventListener("click", openSettings);
  $("setIde").addEventListener("change", syncIdeHint);
  $("settingsForm").addEventListener("submit", (e) => {
    if (e.submitter && e.submitter.value === "save") saveSettings();
  });
  $("btnTestOpen").addEventListener("click", async () => {
    const first = doc().parts.flatMap((p) => p.sections).flatMap((s) => s.steps).map((s) => s.code).find(Boolean);
    if (!first) { toast("This walkthrough has no code to try", true); return; }
    const s = Object.assign({}, state.data.settings, {
      ide: $("setIde").value, ideCommand: $("setIdeCommand").value.trim(), idePath: $("setIdePath").value.trim(),
    });
    try {
      await api("/api/settings", { method: "PUT", body: JSON.stringify(s) });
      state.data.settings = s;
      await openAt(first.file, first.from || 1);
    } catch (err) { toast(String(err.message || err), true); }
  });

  window.addEventListener("hashchange", () => { readHash(); render(); });

  document.addEventListener("keydown", (e) => {
    if (e.target.matches("input, select, textarea") || $("settings").open) return;
    if (e.metaKey || e.ctrlKey || e.altKey) return;
    switch (e.key) {
      case "ArrowRight": move(1); break;
      case "ArrowLeft": move(-1); break;
      case "Escape":
        if (state.view === "step") go({ view: "part", section: null });
        else if (state.view === "part") go({ view: "overview", part: null, section: null });
        break;
      case "t": toggleTheme(); break;
      case "s": openSettings(); break;
      case "o": {
        const st = currentStep();
        if (st && st.code) openAt(st.code.file, st.code.from || 1);
        break;
      }
      default: return;
    }
  });

  window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => {
    if (!document.documentElement.dataset.theme) { drawn = { def: null, theme: null }; render(); }
  });

  // The walkthrough is read from disk on every request, so an edit to the file
  // or to the code it points at makes this page stale. Offer a reload.
  setInterval(async () => {
    if (document.hidden || !state.data || !$("btnReload").hidden) return;
    try {
      const r = await api("/api/state");
      if (r.stamp && r.stamp !== state.stamp) $("btnReload").hidden = false;
    } catch (err) {
      if (String(err.message || "").includes("token")) {
        $("btnReload").textContent = "the server restarted, reload";
        $("btnReload").hidden = false;
      }
    }
  }, 2500);
}

(async function main() {
  wire();
  try {
    await load();
  } catch (err) {
    $("boot").textContent = "The page could not start: " + (err.message || err);
  }
})();
