/* The walkthrough as a thing a reader moves through: where they are, where they
   have been, and the address of both.

   Parts, sections and steps are the same three levels in cw/1 and cw/2, which
   is what lets one file do this for both. What differs is only what is inside a
   step, and that is the renderer's business. */

import { SLUG } from "./ui.js";

export const state = {
  data: null,
  view: "overview",
  part: null,
  section: null,
  step: 0,
  done: [],
  // Whatever is open on the screen right now: which note, which frame, which
  // block. It belongs to the panel showing it and is thrown away the moment the
  // reader moves, so each renderer fills it however it likes.
  ui: {},
  // And the handful a reader sets once and means for the rest of the
  // walkthrough. A diff put side by side stays side by side on the next step.
  sticky: {},
  stamp: "",
};

/* ------------------------------------------------------------------ the data */

export const doc = () => state.data.doc;
export const parts = () => doc().parts || [];
export const partAt = (p) => parts()[p];
export const sectionAt = (p, s) => (partAt(p) ? partAt(p).sections[s] : null);
export const stepsOf = (p, s) => (sectionAt(p, s) ? sectionAt(p, s).steps : []);

export function currentStep() {
  if (state.view !== "step" || state.part === null || state.section === null) return null;
  return stepsOf(state.part, state.section)[state.step] || null;
}

export function totalSteps() {
  return parts().reduce((a, p) => a + p.sections.reduce((b, s) => b + s.steps.length, 0), 0);
}

export function stepsIn(p) {
  return p.sections.reduce((a, s) => a + s.steps.length, 0);
}

// Progress and deep links hang off the ids in the document, not off where a
// step happens to sit today. Inserting a step used to move everybody's saved
// place along by one.
const idOf = (thing, fallback) => (thing && thing.id) || String(fallback);
export const key = (p, s, i) =>
  idOf(partAt(p), p) + "/" + idOf(sectionAt(p, s), s) + "/" + idOf(stepsOf(p, s)[i], i);
export const isDone = (p, s, i) => state.done.indexOf(key(p, s, i)) !== -1;
export const sectionComplete = (p, s) => stepsOf(p, s).every((_, i) => isDone(p, s, i));
export const partComplete = (p) => partAt(p).sections.every((_, s) => sectionComplete(p, s));

/* ------------------------------------------------------------------ storage */

function storeKey() {
  const d = state.data || {};
  return "cw:progress:" + (SLUG || d.file || "unknown");
}

export function loadProgress() {
  try {
    const raw = localStorage.getItem(storeKey());
    if (!raw) return;
    const saved = JSON.parse(raw);
    state.done = Array.isArray(saved.done) ? saved.done : [];
    liftProgress();
  } catch { /* a fresh browser is a fresh start, which is fine */ }
}

// Progress saved before ids existed is a list of "0.1.2". Lift it once, so a
// reader who was halfway through yesterday is still halfway through today.
function liftProgress() {
  let changed = false;
  state.done = state.done.map((k) => {
    const m = /^(\d+)\.(\d+)\.(\d+)$/.exec(k);
    if (!m) return k;
    const [p, s, i] = [Number(m[1]), Number(m[2]), Number(m[3])];
    if (!stepsOf(p, s)[i]) return k;
    changed = true;
    return key(p, s, i);
  });
  if (changed) saveProgress();
}

function saveProgress() {
  try {
    localStorage.setItem(storeKey(), JSON.stringify({ done: state.done }));
  } catch { /* private windows refuse, and nothing here is worth failing over */ }
}

/* ------------------------------------------------------------------ routing */

// A link to a step is #part-id/section-id/step-id. The old numeric form is
// still read, because links to it are already out there.
export function readHash() {
  const h = decodeURIComponent((location.hash || "").replace(/^#/, ""));
  if (!h) { state.view = "overview"; state.part = null; state.section = null; return; }
  const found = h.includes("/") ? locateByID(h.split("/")) : locateByNumber(h);
  if (!found) return;
  Object.assign(state, found);
}

/* A level without an id is addressed by its position, because that is what
   key() writes into the address bar for it. Without this the page hands out
   links to itself that it then cannot read back, which is what happens to every
   walkthrough that has not been through publish. */
function at(list, want) {
  const found = list.findIndex((x) => x.id === want);
  if (found >= 0) return found;
  if (!/^\d+$/.test(want)) return -1;
  const n = Number(want);
  return n < list.length ? n : -1;
}

function locateByID(bits) {
  const p = at(parts(), bits[0]);
  if (p < 0) return null;
  if (bits.length < 2) return { view: "part", part: p, section: null };
  const s = at(partAt(p).sections, bits[1]);
  if (s < 0) return null;
  const steps = stepsOf(p, s);
  const i = bits.length > 2 ? at(steps, bits[2]) : 0;
  if (i < 0) return null;
  return { view: "step", part: p, section: s, step: i };
}

function locateByNumber(h) {
  const m = h.match(/^(\d+)(?:-(\d+))?(?:-(\d+))?$/);
  if (!m) return null;
  const p = Number(m[1]) - 1;
  if (!partAt(p)) return null;
  if (m[2] === undefined) return { view: "part", part: p, section: null };
  const s = Number(m[2]) - 1;
  if (!sectionAt(p, s)) return null;
  const i = m[3] === undefined ? 0 : Number(m[3]) - 1;
  return { view: "step", part: p, section: s, step: Math.max(0, Math.min(i, stepsOf(p, s).length - 1)) };
}

/* Every screen has an address, and moving between them is navigation, so it
   goes on the history stack: back walks a reader out of a walkthrough the way
   they walked in. Opening a note or pausing an animation is not a move and does
   not change the address, so it never reaches the push. */
function writeHash() {
  let h = "";
  if (state.view === "part") h = "#" + idOf(partAt(state.part), state.part + 1);
  if (state.view === "step") h = "#" + key(state.part, state.section, state.step);
  if (location.hash === h) return;
  history.pushState(null, "", h || location.pathname + location.search);
}

/* The shell owns drawing, so it hands its render in rather than being imported
   from here. That keeps the arrow pointing one way: the shell knows about the
   state, and the state knows nothing about the page. */
let repaint = () => {};
export function onChange(fn) { repaint = fn; }

export function go(next) {
  Object.assign(state, next);
  saveProgress();
  writeHash();
  repaint();
}

// setUI is go() for the state that belongs to whatever is on the screen: an
// open note, the frame of a timeline, the block a diagram is pointing at.
export function setUI(next) {
  go({ ui: Object.assign({}, state.ui, next) });
}

// setSticky is the same for the settings that outlive the screen.
export function setSticky(next) {
  go({ sticky: Object.assign({}, state.sticky, next) });
}

export function openStep(p, s, i) {
  go({ view: "step", part: p, section: s, step: i, ui: {} });
}

export function markDone(p, s, i, on) {
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
export function moveLabel(dir) {
  const to = pageAhead(dir);
  let what = dir > 0 ? "Next" : "Previous";
  if (to.view === "overview") what = "Overview";
  else if (to.view === "part") what = "Part " + String(to.part + 1).padStart(2, "0");
  else if (state.view === "overview" && dir < 0) what = "Last step";
  return dir > 0 ? what + " →" : "← " + what;
}

export function move(dir) {
  if (dir > 0 && state.view === "step") {
    const k = key(state.part, state.section, state.step);
    if (state.done.indexOf(k) === -1) state.done = state.done.concat([k]);
  }
  go(Object.assign({ ui: {} }, pageAhead(dir)));
}
