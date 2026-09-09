/* The parts of the page that know nothing about walkthroughs: elements, the
   fetch wrapper, the toast, inline markdown, the theme, mermaid, highlighting,
   and the one seam for "take me to this line".

   Everything here is used by both renderers and by the shell, and nothing here
   reads the document. That is the whole rule: if a function needs to know what
   a part or a block is, it does not live in this file. */

export const $ = (id) => document.getElementById(id);

/* The page runs in two places. Locally it talks to a server that has the
   reader's working tree and their editor, so a line number opens a file. Hosted
   it has neither, so a line number becomes a link to the code on GitHub and the
   settings live in this browser. */
const CW = window.CW || {};
export const TOKEN = CW.token || "";
export const HOSTED = !!CW.hosted;
export const SOURCE = CW.source || "/api/walkthrough";
export const SLUG = CW.slug || "";
// The build stamp, so a renderer imported at runtime carries the same cache
// key as the script that imported it.
export const STAMP = CW.stamp || "";

export async function api(path, init) {
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
export function toast(msg, bad) {
  const t = $("toast");
  t.textContent = msg;
  t.classList.toggle("bad", !!bad);
  t.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { t.hidden = true; }, bad ? 7000 : 3200);
}

export function el(tag, cls, text) {
  const n = document.createElement(tag);
  if (cls) n.className = cls;
  if (text !== undefined && text !== null) n.textContent = text;
  return n;
}

export function pad2(n) { return String(n).padStart(2, "0"); }
export function plural(n, word) { return n + " " + word + (n === 1 ? "" : "s"); }

// baseName keeps the tail of a path written with either separator.
export function baseName(file) {
  const cut = Math.max(String(file).lastIndexOf("/"), String(file).lastIndexOf("\\"));
  return cut < 0 ? String(file) : String(file).slice(cut + 1);
}

/* A step can put something on a timer, and the page is rebuilt from scratch on
   every move, so whatever was running belongs to a screen that is gone. The
   shell clears these before it draws. */
let timers = [];
export function every(ms, fn) { timers.push(setInterval(fn, ms)); }
export function clearTimers() {
  timers.forEach(clearInterval);
  timers = [];
}

/* ------------------------------------------------------------------ markdown */

/*
Prose about code is written with backticks around the identifiers, and a
walkthrough is mostly prose about code. Printed as text those backticks land on
the screen, so the prose fields are read as markdown.

It builds nodes rather than a string of HTML. The hosted site serves every
walkthrough from one origin, so a document that reached innerHTML would be a way
into everyone else's page, including the cookie that unlocked it. Nothing here
concatenates markup and nothing here should start.
*/

// mdInline turns one run of prose into nodes.
export function mdInline(text) {
  const frag = document.createDocumentFragment();
  let buf = "";
  const flush = () => {
    if (buf) frag.appendChild(document.createTextNode(buf));
    buf = "";
  };
  const wrap = (tag, inner) => {
    flush();
    const n = el(tag);
    n.appendChild(mdInline(inner));
    frag.appendChild(n);
  };
  // A delimiter has to stand against something that is not a word character, so
  // snake_case_names and 3 * 4 come out the way they were written.
  const edge = (i) => i < 0 || i >= text.length || !/\w/.test(text[i]);

  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (c === "\\" && i + 1 < text.length && "`*_[\\".includes(text[i + 1])) {
      buf += text[++i];
      continue;
    }
    // Code first, and it does not nest: a backtick run is taken as written,
    // which is the whole point of putting one around a name.
    if (c === "`") {
      const end = text.indexOf("`", i + 1);
      if (end > i + 1) {
        flush();
        frag.appendChild(el("code", "md", text.slice(i + 1, end)));
        i = end;
        continue;
      }
    }
    if (c === "*" && text[i + 1] === "*") {
      const end = text.indexOf("**", i + 2);
      if (end > i + 2) {
        wrap("strong", text.slice(i + 2, end));
        i = end + 1;
        continue;
      }
    }
    if ((c === "*" || c === "_") && edge(i - 1) && text[i + 1] !== " ") {
      const end = text.indexOf(c, i + 1);
      if (end > i + 1 && text[end - 1] !== " " && edge(end + 1)) {
        wrap("em", text.slice(i + 1, end));
        i = end;
        continue;
      }
    }
    if (c === "[") {
      const m = /^\[([^\]\n]+)\]\(([^)\s]+)\)/.exec(text.slice(i));
      if (m && safeHref(m[2])) {
        flush();
        const a = el("a", null, m[1]);
        a.href = m[2];
        a.target = "_blank";
        a.rel = "noreferrer";
        frag.appendChild(a);
        i += m[0].length - 1;
        continue;
      }
    }
    buf += c;
  }
  flush();
  return frag;
}

// safeHref keeps javascript: and data: out of a link somebody else wrote. A
// link that does not pass stays the text it was.
export function safeHref(url) {
  return /^(https?:\/\/|mailto:|#|\/)/i.test(url);
}

// mdEl is el() for the fields that hold prose rather than a name.
export function mdEl(tag, cls, text) {
  const n = el(tag, cls);
  n.appendChild(mdInline(String(text)));
  return n;
}

/* ------------------------------------------------------------------ theme */

export function applyTheme(settings) {
  const root = document.documentElement;
  if (settings.theme === "light" || settings.theme === "dark") root.dataset.theme = settings.theme;
  else delete root.dataset.theme;
  if (settings.accent) root.style.setProperty("--accent", settings.accent);
}

export function effectiveDark() {
  const set = document.documentElement.dataset.theme;
  if (set) return set === "dark";
  return window.matchMedia("(prefers-color-scheme: dark)").matches;
}

/* Where a setting is kept. Locally it is a file the server owns, so the next
   run comes up the way you left it on this machine. Hosted there is no machine
   to speak of, and the editor settings are meaningless, so the theme and the
   accent live in this browser and nothing leaves it. */
export const settingsStore = {
  local: "cw:settings",

  stored() {
    if (!HOSTED) return null;
    try { return JSON.parse(localStorage.getItem(this.local) || "null"); }
    catch { return null; }
  },

  async save(s) {
    if (!HOSTED) {
      const r = await api("/api/settings", { method: "PUT", body: JSON.stringify(s) });
      return { settings: r.settings, path: r.path };
    }
    try { localStorage.setItem(this.local, JSON.stringify({ theme: s.theme, accent: s.accent })); }
    catch { /* a private window refuses, and the page has already switched */ }
    return { settings: s, path: "this browser" };
  },
};

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

let mermaidSeq = 0;

/* drawMermaid draws one diagram into one element and answers with the svg, so
   the caller can wire up whatever its own format says is clickable.

   The token is per element rather than global, because a cw/2 step may hold
   more than one diagram and rendering is slow enough that they overlap. Without
   it the last one to start would cancel the others. */
export async function drawMermaid(host, def) {
  const token = ++mermaidSeq;
  host._draw = token;

  let m;
  try {
    m = await mermaid();
  } catch {
    host.textContent = "";
    host.appendChild(el("pre", "fallback", def));
    return null;
  }
  m.initialize({
    startOnLoad: false, securityLevel: "loose", theme: "base",
    fontFamily: "'JetBrains Mono', monospace",
    themeVariables: effectiveDark() ? MERMAID_DARK : MERMAID_LIGHT,
  });
  try {
    const res = await m.render("mmd-" + token, def);
    if (host._draw !== token || !host.isConnected) return null;
    host.innerHTML = res.svg;
    const svg = host.querySelector("svg");
    if (svg) { svg.style.maxWidth = "100%"; svg.style.height = "auto"; }
    return svg;
  } catch (err) {
    host.textContent = "";
    host.appendChild(el("pre", "fallback", def));
    toast("The diagram did not render: " + (err && err.message ? err.message : err), true);
    return null;
  }
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

export function langFor(file, explicit, fallback) {
  const want = explicit || fallback || "";
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

// highlightInto repaints the .t spans already in host, in order. Any renderer
// that lays out one .t per line gets highlighting without asking for it.
export function highlightInto(host, text, lang) {
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

/* ------------------------------------------------------------------ opening */

/* One seam for "take me to this line". Locally that is the reader's editor,
   through the server, because only the server can start a process. Hosted it is
   a link to GitHub: into the pull request diff when the file is part of the
   change, and otherwise to the file at the commit the walkthrough was written
   against, which stays right after the branch moves on.

   The document is not read here. What the page knows about links arrives as one
   object the server worked out, and where the editor is arrives as one string,
   so both versions of the format come through the same door. */

let links = null;
let editorName = "your editor";

export function useLinks(github, ide) {
  links = github || null;
  editorName = !ide || ide === "auto" ? "your editor" : ide;
}

export function whereOpens() { return editorName; }

export function githubHref(file, line, to) {
  if (!links) return null;
  if (links.prFiles && links.anchors && links.anchors[file]) {
    return links.prFiles + "#" + links.anchors[file] + "R" + (line || 1);
  }
  if (links.blobBase) {
    const at = "#L" + (line || 1) + (to && to > line ? "-L" + to : "");
    return links.blobBase + file + at;
  }
  return links.prFiles || null;
}

/* A path in a panel head is long, wraps onto a second line and puts the half
   that identifies it last. So what is shown is the base name, the whole path is
   one hover away, and the name itself opens the file: in an editor locally, on
   the pull request or the blob when this is the hosted page. The explicit button
   stays, because it is the one that says where it will open. */
export function fileName(file, line, to, extra) {
  const short = baseName(file) + (extra || "");
  const where = HOSTED ? "opens on GitHub" : "opens in " + whereOpens();

  if (!HOSTED) {
    const b = el("button", "file", short);
    b.type = "button";
    b.title = file + " · " + where;
    b.addEventListener("click", () => openAt(file, line));
    return b;
  }
  const href = githubHref(file, line, to);
  if (!href) {
    const span = el("span", "file plain", short);
    span.title = file;
    return span;
  }
  const a = el("a", "file", short);
  a.href = href;
  a.target = "_blank";
  a.rel = "noreferrer";
  a.title = file + " · " + where;
  return a;
}

export function openButton(file, line, cls, to) {
  if (!HOSTED) {
    const b = el("button", cls, "open in IDE ↗");
    b.type = "button";
    b.title = "open " + file + " at line " + line + " in " + whereOpens();
    b.addEventListener("click", () => openAt(file, line));
    return b;
  }
  const href = githubHref(file, line, to);
  if (!href) {
    const b = el("button", cls, "no link ↗");
    b.type = "button";
    b.title = "this walkthrough names no repository, so there is nothing to link to";
    b.addEventListener("click", () => toast("This walkthrough names no repository to link to", true));
    return b;
  }
  const a = el("a", cls + " open-link", "on GitHub ↗");
  a.href = href;
  a.target = "_blank";
  a.rel = "noreferrer";
  a.title = file + " at line " + line;
  return a;
}

export async function openAt(file, line) {
  if (HOSTED) {
    const href = githubHref(file, line);
    if (href) window.open(href, "_blank", "noreferrer");
    else toast("This walkthrough names no repository to link to", true);
    return;
  }
  try {
    const r = await api("/api/open", { method: "POST", body: JSON.stringify({ file, line, col: 1 }) });
    if (r.ok) toast((r.message ? r.message + ". " : "") + file + ":" + line, !!r.message);
    else toast(r.message || "The editor did not start", true);
  } catch (err) {
    toast(String(err.message || err), true);
  }
}

// copyText prefers the clipboard API and falls back to the old selection trick,
// which is what an http origin or an older browser leaves you.
export async function copyText(text) {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch { /* fall through, the selection still works */ }
  try {
    const t = el("textarea");
    t.value = text;
    t.style.position = "fixed";
    t.style.opacity = "0";
    document.body.appendChild(t);
    t.select();
    const ok = document.execCommand("copy");
    t.remove();
    return ok;
  } catch {
    return false;
  }
}
