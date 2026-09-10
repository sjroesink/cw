/* What a reader asks about the words in front of them, and what came back.

   A comment hangs on a block, which both renderers name the same way with
   data-anchor and data-kind, so nothing in here knows cw/1 from cw/2. It hangs
   on a selection inside that block too, and the two kinds of selection are put
   back differently: in code by the line, because the highlighter owns what is
   inside a line, and in prose by finding the words again.

   None of this exists on the site. There is nobody in a terminal there to
   answer, and the routes are not registered. */

import { HOSTED, STAMP, $, el, api, toast, mdBlock, copyText } from "./ui.js";
import { state, parts, stepsOf, key as routeOf, currentStep, go } from "./state.js";

let view = { rev: -1, watching: false, prompt: "", threads: [] };
let draft = null; // the selection being written about, until it is sent or dropped
let opened = null; // which thread is expanded
let showArchived = false;

// Whether the column is open, and how wide. Before anybody says otherwise
// that is "open on a step that has been asked about", at the width a card
// reads well in, and both are theirs to change and keep.
let pin = null;
let width = 340;
let answered = new Set(); // what had an answer last time we looked

/* An answer may be written in the seven blocks a step is written in, and then
   it is drawn by the renderer that draws a step. That renderer is loaded per
   version by the shell, so this asks for it the first time an answer needs
   it: a cw/1 walkthrough whose comments are all prose never loads it. */
let drawBlocks = null;
let loading = false;

function needBlocks() {
  if (drawBlocks || loading) return;
  loading = true;
  import("./render2.js" + (STAMP ? "?v=" + STAMP : "")).then((mod) => {
    drawBlocks = mod.blocks;
    refresh();
  }).catch(() => { /* the answer stays the sentence under it */ });
}

// The composer holds what somebody is halfway through typing, so it is built
// once and never rebuilt from under them.
let box = null; // the column
let composer = null;
let asking = null; // the button that floats by a selection

export const busy = () => !!draft;

try {
  const saved = localStorage.getItem("cw:comments");
  if (saved === "open" || saved === "closed") pin = saved === "open";
  const w = Number(localStorage.getItem("cw:comments-width"));
  if (w) width = w;
  showArchived = localStorage.getItem("cw:comments-archived") === "yes";
} catch { /* a private window is a fresh start, which is fine */ }

const keep = (k, v) => {
  try {
    localStorage.setItem(k, String(v));
  } catch { /* nothing here is worth failing over */ }
};

// The column opens by itself on a step that has been asked about, and the
// button always does the opposite of what is on the screen. Asked for, it
// opens on any screen and with nothing in it: that is where it says whether
// anybody is listening, and how to leave the first one.
function shown() {
  if (draft) return true;
  if (pin !== null) return pin;
  return mine().length > 0;
}

export function toggle() {
  pin = !shown();
  keep("cw:comments", pin ? "open" : "closed");
  refresh();
}

/* ------------------------------------------------------------------ the data */

export function seed(payload) {
  if (HOSTED || !payload || !payload.comments) return;
  view = payload.comments;
  answered = answeredIn(view.threads);
}

// fold takes what the poll already asks for. The page learns that something
// changed from one number, and only then asks for the threads themselves.
export async function fold(st) {
  if (HOSTED || !st || !st.comments) return;
  const was = view.watching;
  view.watching = !!st.comments.watching;
  if (st.comments.rev === view.rev) {
    if (was !== view.watching) drawColumn();
    return;
  }
  await reload();
}

async function reload() {
  try {
    view = await api("/api/comments");
  } catch (err) {
    return; // a server that stopped is already saying so through the reload pill
  }
  const now = answeredIn(view.threads);
  for (const t of view.threads) {
    if (now.has(t.id) && !answered.has(t.id) && !onThisStep(t)) {
      toast("an answer came back on " + (t.where.title || "another step"));
      break;
    }
  }
  answered = now;
  refresh();
}

const answeredIn = (threads) => new Set((threads || []).filter((t) => t.status === "answered").map((t) => t.id));
const onThisStep = (t) => state.view === "step" && t.where.step === here();
const here = () => routeOf(state.part, state.section, state.step);
const live = (t) => showArchived || !t.archived;
const mine = () => view.threads.filter((t) => onThisStep(t) && live(t));

/* ------------------------------------------------------------------ the page */

// mount runs after every render, because the stage it decorates is thrown away
// and built again on every one of them.
export function mount(stage) {
  if (HOSTED) return;
  build();
  if (draft && draft.step !== here()) close();
  markStage(stage);
  drawColumn();
}

function refresh() {
  const stage = $("stage");
  unmark(stage);
  markStage(stage);
  drawColumn();
}

function build() {
  if (box) return;
  box = el("aside", "threads");
  box.appendChild(grip());
  const inner = el("div", "threads-inner");
  inner.appendChild(el("div", "threads-head"));
  inner.appendChild(el("div", "threads-list"));
  composer = buildComposer();
  inner.appendChild(composer);
  box.appendChild(inner);
  document.querySelector(".layout").appendChild(box);
  setWidth(width);
  if ($("btnComments")) $("btnComments").addEventListener("click", toggle);
  window.addEventListener("resize", () => setWidth(width));

  document.addEventListener("selectionchange", () => {
    // Selecting is a drag, and this fires for every pixel of it. The button
    // shows up once the reader has stopped moving.
    clearTimeout(build.timer);
    build.timer = setTimeout(offerToComment, 220);
  });
  document.addEventListener("scroll", () => { if (asking) asking.hidden = true; }, true);

  // The stage survives every render, its contents do not, so the marks are
  // listened to from here rather than one listener per line. A line that is
  // already a note's line keeps answering to the note.
  $("stage").addEventListener("click", (e) => {
    const hit = e.target.closest("mark.asked, .pin, .row.asked");
    if (!hit || (hit.classList.contains("clickable") && !hit.classList.contains("pin"))) return;
    const id = hit.dataset.thread;
    if (id) reveal(id);
  });
}

/* The edge between the reading and the asking. A column somebody has read
   three answers in is too narrow for the fourth, and how wide it should be
   is not something this can work out on their behalf. */
function grip() {
  const g = el("div", "grip");
  g.title = "drag to make this wider";
  let from = 0;
  let start = 0;
  const move = (e) => setWidth(start + (from - e.clientX));
  const stop = () => {
    document.removeEventListener("pointermove", move);
    document.removeEventListener("pointerup", stop);
    document.body.classList.remove("resizing");
    keep("cw:comments-width", width);
  };
  g.addEventListener("pointerdown", (e) => {
    e.preventDefault();
    from = e.clientX;
    start = box.getBoundingClientRect().width;
    document.body.classList.add("resizing");
    document.addEventListener("pointermove", move);
    document.addEventListener("pointerup", stop);
  });
  return g;
}

// Wide enough for a card, and never so wide that the code it is about has
// nowhere left to be.
function setWidth(px) {
  const room = Math.max(300, window.innerWidth - 620);
  width = Math.round(Math.max(260, Math.min(px, room)));
  document.querySelector(".layout").style.setProperty("--threads", width + "px");
}

function drawColumn() {
  if (!box) return;
  const show = shown();
  box.hidden = !show;
  document.querySelector(".layout").classList.toggle("withcomments", show);
  drawToggle();
  if (!show) return;

  const head = box.querySelector(".threads-head");
  head.textContent = "";
  head.appendChild(el("b", null, "comments"));
  head.appendChild(watcherLine());
  if (view.threads.some((t) => t.archived)) head.appendChild(archiveBox());

  const list = box.querySelector(".threads-list");
  list.textContent = "";
  const here = mine();
  for (const t of here) list.appendChild(card(t));

  const others = view.threads.filter((t) => !onThisStep(t) && live(t));
  if (!here.length && !others.length) {
    list.appendChild(el("p", "empty", state.view === "step"
      ? "Select a few lines of code or half a sentence, and press comment."
      : "Open a step to leave a comment on what is in it."));
    return;
  }
  if (!others.length) return;
  list.appendChild(el("div", "elsewhere",
    state.view === "step" ? "asked on another step" : "asked in this walkthrough"));
  for (const t of others) list.appendChild(rowFor(t));
}

// The button stays. It is the way to the column, and the column is the only
// place that says whether anybody is listening, which is worth knowing most
// when there is nothing there yet.
function drawToggle() {
  const b = $("btnComments");
  if (!b) return;
  b.hidden = false;
  b.classList.toggle("on", shown());
  b.title = (shown() ? "Hide" : "Show") + " the comments (c)";
  b.setAttribute("aria-pressed", shown() ? "true" : "false");
  const n = $("commentCount");
  if (!n) return;
  const count = view.threads.filter(live).length;
  n.textContent = count ? String(count) : "";
  n.hidden = !count;
}

// One line for a thread on another step. Clicking it goes there, which is a
// move like any other and takes the same road.
function rowFor(t) {
  const row = el("button", "thread-row");
  row.type = "button";
  row.appendChild(el("span", "at", t.where.title || t.where.step));
  row.appendChild(el("span", "state " + t.status, t.status));
  row.appendChild(el("span", "msg-one", t.messages[0] ? t.messages[0].text : ""));
  row.addEventListener("click", () => {
    const at = stepAt(t.where.step);
    opened = t.id;
    if (at) go(Object.assign({ view: "step", ui: {} }, at));
  });
  return row;
}

// Whether anybody is listening is the difference between a question that gets
// answered and one that sits there, so the page says which it is.
function watcherLine() {
  const line = el("div", "watcher" + (view.watching ? " on" : ""));
  if (view.watching) {
    line.appendChild(el("span", "dot"));
    line.appendChild(el("span", null, "an agent is watching these"));
    return line;
  }
  line.appendChild(el("span", null, "nobody is watching these yet"));
  const b = el("button", "tiny", "copy the prompt");
  b.type = "button";
  b.title = "paste it into an agent, and it answers what you ask here";
  b.addEventListener("click", async () => {
    await copyText(view.prompt || "");
    toast("copied. Paste it into an agent and leave it running");
  });
  line.appendChild(b);
  return line;
}

// Whether what has been put away is on the screen. Only offered once there is
// something put away, because a checkbox that filters nothing is furniture.
function archiveBox() {
  const row = el("label", "archived-toggle");
  const box = el("input");
  box.type = "checkbox";
  box.checked = showArchived;
  box.addEventListener("change", () => {
    showArchived = box.checked;
    keep("cw:comments-archived", showArchived ? "yes" : "no");
    refresh();
  });
  row.appendChild(box);
  row.appendChild(el("span", null, "show archived"));
  return row;
}

function card(t) {
  const c = el("div", "thread" + (opened === t.id ? " on" : "") + (t.archived ? " archived" : ""));
  c.dataset.thread = t.id;

  const head = el("div", "thread-head");
  head.appendChild(el("span", "at", place(t)));
  head.appendChild(el("span", "grow"));
  const put = el("button", "tiny", t.archived ? "unarchive" : "archive");
  put.type = "button";
  put.title = t.archived ? "put this back in the column" : "put this away, without losing it";
  put.addEventListener("click", async (e) => {
    e.stopPropagation();
    try {
      await api("/api/comments/" + t.id + "/archive",
        { method: "POST", body: JSON.stringify({ on: !t.archived }) });
    } catch (err) {
      toast(String(err.message || err), true);
      return;
    }
    await reload();
  });
  head.appendChild(put);
  c.appendChild(head);

  if (t.where.quote) c.appendChild(el("div", "quote", t.where.quote));

  for (const [i, m] of t.messages.entries()) {
    const msg = el("div", "msg " + (m.from === "agent" ? "agent" : "reader"));
    msg.appendChild(el("span", "who", m.from === "agent" ? "the agent" : "you"));
    if (m.blocks && m.blocks.length) {
      if (drawBlocks) msg.appendChild(drawBlocks(m.blocks, "c" + t.id + "." + i + ":"));
      else {
        needBlocks();
        msg.appendChild(el("div", "thinking", "drawing the answer"));
      }
    } else {
      msg.appendChild(mdBlock(m.text || ""));
    }
    c.appendChild(msg);
  }

  if (t.status === "thinking") {
    const wait = el("div", "thinking");
    wait.appendChild(el("span", "spin"));
    wait.appendChild(el("span", null, "the agent is answering"));
    c.appendChild(wait);
  } else if (t.status !== "answered") {
    c.appendChild(el("div", "thinking",
      view.watching ? "waiting for the agent" : "nobody has picked this up"));
  } else {
    c.appendChild(again(t));
  }

  c.addEventListener("click", () => {
    opened = opened === t.id ? null : t.id;
    refresh();
    const first = document.querySelector(".asked[data-thread='" + t.id + "']");
    if (first && opened === t.id) first.scrollIntoView({ block: "center", behavior: "smooth" });
  });
  c.addEventListener("mouseenter", () => light(t.id, true));
  c.addEventListener("mouseleave", () => light(t.id, false));
  return c;
}

// Asking again under an answer is a second question about the same thing, so it
// goes in the same thread rather than needing the same words selected twice.
function again(t) {
  const row = el("div", "more");
  // Everything in here is typing, and the card underneath closes on a click.
  row.addEventListener("click", (e) => e.stopPropagation());
  const b = el("button", "tiny", "ask something else about this");
  b.type = "button";
  b.addEventListener("click", (e) => {
    e.stopPropagation();
    const area = el("textarea");
    area.rows = 3;
    area.placeholder = "what else about this?";
    const send = el("button", "nav next", "ask");
    send.type = "button";
    const post = async () => {
      const text = area.value.trim();
      if (!text) return;
      try {
        await api("/api/comments/" + t.id, { method: "POST", body: JSON.stringify({ from: "reader", text }) });
      } catch (err) {
        toast(String(err.message || err), true);
        return;
      }
      await reload();
    };
    send.addEventListener("click", (ev) => { ev.stopPropagation(); post(); });
    area.addEventListener("keydown", (ev) => {
      ev.stopPropagation();
      if (ev.key === "Enter" && (ev.ctrlKey || ev.metaKey)) post();
    });
    row.textContent = "";
    row.appendChild(area);
    row.appendChild(send);
    area.focus();
  });
  row.appendChild(b);
  return row;
}

function place(t) {
  if (t.where.file) {
    const l = t.where.lines;
    if (l && l.end > l.start) return t.where.file + " " + l.start + "-" + l.end;
    if (l) return t.where.file + " " + l.start;
    return t.where.file;
  }
  return t.where.title || "this step";
}

/* -------------------------------------------------------------- the marks */

// markStage puts every comment back beside the words it is about. It runs on a
// stage that was built a moment ago, so there is never anything to clean up.
function markStage(stage) {
  if (!stage) return;
  for (const t of mine()) {
    const host = stage.querySelector("[data-anchor='" + cssName(t.where.block) + "']");
    if (!host) continue;
    if (host.dataset.kind === "code" && t.where.lines) markLines(host, t);
    else if (host.dataset.kind === "text") markQuote(host, t);
    else markBlock(host, t);
  }
}

function unmark(stage) {
  if (!stage) return;
  for (const m of stage.querySelectorAll("mark.asked")) {
    const parent = m.parentNode;
    while (m.firstChild) parent.insertBefore(m.firstChild, m);
    parent.removeChild(m);
  }
  for (const n of stage.querySelectorAll(".asked")) n.classList.remove("asked", "lit");
  for (const n of stage.querySelectorAll(".asked-block")) n.classList.remove("asked-block");
  for (const n of stage.querySelectorAll(".pin")) n.remove();
  // Text taken out of a mark leaves the run of words in pieces, and the next
  // search for a quote reads them as written.
  stage.normalize();
}

function markLines(host, t) {
  let first = null;
  for (const row of host.querySelectorAll(".row[data-line]")) {
    const n = Number(row.dataset.line);
    if (n < t.where.lines.start || n > (t.where.lines.end || t.where.lines.start)) continue;
    row.classList.add("asked");
    row.dataset.thread = t.id;
    if (!first) first = row;
  }
  if (!first) markBlock(host, t);
}

/* The words are looked for where they were, and then anywhere. A block that has
   been edited under a comment keeps the comment: it moves to the block itself
   and says nothing it cannot stand behind, which is the same line the snippet
   check takes. */
function markQuote(host, t) {
  const quote = String(t.where.quote || "");
  if (!quote) return markBlock(host, t);
  const all = host.textContent;
  let at = all.indexOf(quote, Math.max(0, (t.where.start || 0) - 40));
  if (at < 0) at = all.indexOf(quote);
  if (at < 0) return markBlock(host, t);

  const walk = document.createTreeWalker(host, NodeFilter.SHOW_TEXT);
  const range = document.createRange();
  let seen = 0;
  let node = walk.nextNode();
  let started = false;
  while (node) {
    const len = node.nodeValue.length;
    if (!started && seen + len > at) {
      range.setStart(node, at - seen);
      started = true;
    }
    if (started && seen + len >= at + quote.length) {
      range.setEnd(node, at + quote.length - seen);
      break;
    }
    seen += len;
    node = walk.nextNode();
  }
  if (!started) return markBlock(host, t);
  const mark = el("mark", "asked");
  mark.dataset.thread = t.id;
  try {
    range.surroundContents(mark);
  } catch (err) {
    // A selection that runs across a bold word and out the other side cannot be
    // wrapped in one node. The comment is not lost, it just marks the block.
    markBlock(host, t);
  }
}

function markBlock(host, t) {
  host.classList.add("asked-block");
  if (host.querySelector(":scope > .pin[data-thread='" + t.id + "']")) return;
  const pin = el("button", "pin", "#" + t.id);
  pin.type = "button";
  pin.dataset.thread = t.id;
  pin.title = "the comment on this";
  host.appendChild(pin);
}

function reveal(id) {
  opened = id;
  drawColumn();
  const c = box.querySelector(".thread[data-thread='" + id + "']");
  if (c) c.scrollIntoView({ block: "nearest", behavior: "smooth" });
  light(id, true);
  setTimeout(() => light(id, false), 1200);
}

function light(id, on) {
  for (const n of document.querySelectorAll("[data-thread='" + id + "']")) n.classList.toggle("lit", on);
}

// An id out of the document is a name the author chose, and it goes into a
// selector. Anything that is not a plain name is not one of ours.
const cssName = (s) => String(s || "").replace(/['"\\\]]/g, "");

/* ------------------------------------------------------------ the composer */

function offerToComment() {
  if (HOSTED || draft) return;
  const pick = selection();
  if (!pick) {
    if (asking) asking.hidden = true;
    return;
  }
  if (!asking) {
    asking = el("button", "askbtn", "comment");
    asking.type = "button";
    asking.addEventListener("mousedown", (e) => e.preventDefault());
    asking.addEventListener("click", () => startDraft());
    document.body.appendChild(asking);
  }
  const r = pick.range.getBoundingClientRect();
  asking.hidden = false;
  asking.style.top = (r.bottom + window.scrollY + 6) + "px";
  asking.style.left = Math.max(8, Math.min(r.right + window.scrollX - 30,
    window.innerWidth + window.scrollX - 110)) + "px";
}

function selection() {
  const sel = window.getSelection();
  if (!sel || sel.isCollapsed || !sel.rangeCount) return null;
  const range = sel.getRangeAt(0);
  const host = anchorOf(range.commonAncestorContainer);
  if (!host || !$("stage").contains(host)) return null;
  const text = String(sel).replace(/\s+$/, "");
  if (!text.trim()) return null;
  return { host, range, text };
}

function anchorOf(node) {
  let n = node && node.nodeType === 1 ? node : node && node.parentNode;
  while (n && n !== document.body) {
    if (n.dataset && n.dataset.anchor) return n;
    n = n.parentNode;
  }
  return null;
}

function startDraft() {
  const pick = selection();
  if (!pick) return;
  draft = whereOf(pick);
  draft.step = here();
  if (asking) asking.hidden = true;
  window.getSelection().removeAllRanges();

  composer.hidden = false;
  composer.querySelector(".quote").textContent = draft.quote;
  const area = composer.querySelector("textarea");
  area.value = "";
  drawColumn();
  area.focus();
}

function whereOf(pick) {
  const host = pick.host;
  const kind = host.dataset.kind || "text";
  const step = currentStep();
  const w = {
    step: here(),
    title: (step && step.title) || "",
    block: host.dataset.anchor || "",
    kind,
    quote: pick.text.slice(0, 4000),
  };
  if (kind === "code") {
    const rows = [...host.querySelectorAll(".row[data-line]")]
      .filter((row) => pick.range.intersectsNode(row))
      .map((row) => Number(row.dataset.line))
      .filter((n) => !Number.isNaN(n));
    if (rows.length) w.lines = { start: Math.min(...rows), end: Math.max(...rows) };
    if (host.dataset.file) w.file = host.dataset.file;
  } else if (kind === "text") {
    const before = pick.range.cloneRange();
    before.selectNodeContents(host);
    before.setEnd(pick.range.startContainer, pick.range.startOffset);
    w.start = before.toString().length;
  }
  return w;
}

function buildComposer() {
  const c = el("div", "composer");
  c.hidden = true;
  c.appendChild(el("div", "quote"));
  const area = el("textarea");
  area.rows = 4;
  area.placeholder = "what do you want to know about this?";
  c.appendChild(area);

  const foot = el("div", "composer-foot");
  const cancel = el("button", "nav", "cancel");
  cancel.type = "button";
  cancel.addEventListener("click", close);
  const send = el("button", "nav next", "ask");
  send.type = "button";
  send.addEventListener("click", post);
  foot.appendChild(cancel);
  foot.appendChild(el("span", "grow"));
  foot.appendChild(send);
  c.appendChild(foot);

  area.addEventListener("keydown", (e) => {
    e.stopPropagation();
    if (e.key === "Escape") close();
    if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) post();
  });
  return c;
}

async function post() {
  const area = composer.querySelector("textarea");
  const text = area.value.trim();
  if (!text || !draft) return;
  const where = draft;
  try {
    await api("/api/comments", { method: "POST", body: JSON.stringify({ where, text }) });
  } catch (err) {
    toast(String(err.message || err), true);
    return;
  }
  close();
  await reload();
  if (!view.watching) {
    toast("asked. Nobody is watching yet: copy the prompt at the top of the column");
  }
}

function close() {
  draft = null;
  if (composer) composer.hidden = true;
  drawColumn();
}

// Where a thread's step sits now. Ids are what a link hangs off, so a step that
// moved is still found and one that was cut is not guessed at.
function stepAt(route) {
  const ps = parts();
  for (let p = 0; p < ps.length; p++) {
    for (let s = 0; s < ps[p].sections.length; s++) {
      for (let i = 0; i < stepsOf(p, s).length; i++) {
        if (routeOf(p, s, i) === route) return { part: p, section: s, step: i };
      }
    }
  }
  return null;
}
