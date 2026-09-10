/* code-walkthrough: the page. It holds no topic of its own. Everything it shows
   comes from one JSON file the server hands over, and everything it can do to
   the machine (open a file, save a setting) goes back through the server.

   This file is the shell. Parts, sections and steps are the same three levels
   in both versions of the format, so the rail, the overview, the part page, the
   progress and the arrow keys are here and work on either. What is inside a step
   is not the same, so that is loaded per version: render1.js for cw/1, and
   render2.js for cw/2, chosen from the version in the document itself. */

import {
  $, HOSTED, SOURCE, STAMP, api, toast, el, mdEl, pad2, plural, baseName,
  applyTheme, effectiveDark, settingsStore, clearTimers, copyText,
  useLinks, openAt, whereOpens,
} from "./ui.js";
import {
  state, doc, parts, partAt, sectionAt, stepsOf, currentStep, totalSteps, stepsIn,
  isDone, sectionComplete, partComplete, loadProgress, readHash,
  onChange, go, openStep, markDone, moveLabel, move,
} from "./state.js";

// The renderer for the version in front of us. Nothing draws before load() has
// picked one.
let R = null;

/* The version in the document decides, and only the version. The $schema beside
   it is what an editor follows while somebody types, and it can be a relative
   path or a stale URL, so it is not what a reader acts on.

   The stamp goes on the URL so that the renderer is cached as hard as the script
   that imported it. Without it a proxy has to revalidate a file that only ever
   changes when the build does. */
async function rendererFor(version) {
  const file = version === "cw/2" ? "./render2.js" : "./render1.js";
  const mod = await import(file + (STAMP ? "?v=" + STAMP : ""));
  return mod.default;
}

/* ------------------------------------------------------------------ chrome */

function renderChrome() {
  const d = doc();
  const head = R.head(d) || {};
  document.title = d.title + " · walkthrough";
  $("title").textContent = d.title;
  $("repo").textContent = head.repo || state.data.rootName || "";
  $("slash").hidden = !(head.repo && head.number);

  const num = $("number");
  num.textContent = head.number || "";
  if (head.url) { num.href = head.url; num.removeAttribute("aria-disabled"); }
  else { num.removeAttribute("href"); }

  $("state").textContent = head.state || "";
  $("state").hidden = !head.state;
  renderVerified();

  const total = totalSteps();
  $("progressLabel").textContent = state.done.length + " / " + total;
  $("progressFill").style.width = total ? Math.round((state.done.length / total) * 100) + "%" : "0";
}

/* Hosted, the page has no working tree to check the snippets against, so the
   only honest thing it can say is what the publisher's tree said at the time.
   That is worth stating rather than leaving out: a reader deserves to know
   which commit the code in front of them was true for. */
function renderVerified() {
  const chip = $("verified");
  const v = HOSTED && state.data.meta && state.data.meta.verified;
  if (!v || (!v.commit && !v.checked)) { chip.hidden = true; return; }

  const when = state.data.meta.updatedAt
    ? new Date(state.data.meta.updatedAt).toLocaleDateString(undefined, { day: "numeric", month: "short" })
    : "";
  const at = v.commit ? v.commit.slice(0, 7) : "";
  const bits = [];
  if (v.checked && !v.stale && !v.moved) bits.push("code checked");
  else if (v.stale) bits.push(plural(v.stale, "snippet") + " already stale");
  else if (v.moved) bits.push(plural(v.moved, "snippet") + " had moved");
  else bits.push("unchecked");
  if (at) bits.push(at);
  if (when) bits.push(when);

  chip.textContent = bits.join(" · ");
  chip.classList.toggle("stale", !!v.stale);
  chip.title = v.checked
    ? plural(v.checked, "snippet") + " were compared against the publisher's working tree when this was published"
    : "nothing was compared against a working tree when this was published";
  chip.hidden = false;
}

function problems(extra) {
  const d = state.data;
  const box = $("problems");
  const errs = d.errors || [], warns = d.warnings || [];
  const trouble = extra ? [extra] : [];
  if (d.moved) trouble.push(d.moved + " snippet(s) still exist but have moved to another line");
  if (d.stale) trouble.push(d.stale + " snippet(s) are no longer in the working tree");
  const v = d.meta && d.meta.verified;
  if (v && v.stale) trouble.push(v.stale + " snippet(s) were already out of date when this was published");
  if (!errs.length && !warns.length && !trouble.length) { box.hidden = true; return; }
  const list = [...errs, ...trouble, ...warns].slice(0, 20);
  box.textContent = "";
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
  clearTimers();
  renderChrome();
  renderRail();
  const stage = $("stage");
  stage.textContent = "";
  if (state.view === "overview") stage.appendChild(viewOverview());
  else if (state.view === "part") stage.appendChild(viewPart());
  else stage.appendChild(viewStep());
  window.scrollTo({ top: 0 });
}

function viewOverview() {
  const d = doc();
  const v = el("div", "view");
  v.appendChild(el("div", "eyebrow", "This walkthrough has " + plural(parts().length, "part")));
  if (d.summary) v.appendChild(mdEl("p", "lede", d.summary));

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
    const intro = R.partIntro(p) || {};
    if (intro.short) card.appendChild(mdEl("div", "desc", intro.short));
    if (p.files && p.files.length) {
      const chips = el("div", "chips");
      for (const f of p.files) {
        const chip = el("span", "chip", baseName(f));
        if (baseName(f) !== f) chip.title = f;
        chips.appendChild(chip);
      }
      card.appendChild(chips);
    }
    card.addEventListener("click", () => go({ view: "part", part: pi, section: null }));
    grid.appendChild(card);
  });
  v.appendChild(grid);
  if (HOSTED) v.appendChild(localBlock());
  v.appendChild(pageFoot());
  return v;
}

/* The two things this page cannot do are the two things a checkout gives back:
   a line number that opens an editor, and a snippet checked against the code as
   it is now. Both come back by pulling the walkthrough down to the machine that
   has the repository, so the command that does it is on the page rather than in
   a document somebody has to find. */
function localBlock() {
  const box = el("div", "local");
  box.appendChild(el("div", "local-head", "Read it against your own checkout"));
  box.appendChild(el("p", "local-why",
    "Locally every line number opens your editor, and every snippet is checked against your " +
    "working tree on each load. This page has neither, so it shows what the publisher's tree said."));

  const cmd = "cw open " + location.origin + location.pathname;
  const row = el("div", "copyrow");
  const input = el("input");
  input.type = "text";
  input.readOnly = true;
  input.spellcheck = false;
  input.value = cmd;
  input.setAttribute("aria-label", "the command that opens this walkthrough locally");
  input.addEventListener("focus", () => input.select());
  row.appendChild(input);

  const btn = el("button", "copy", "copy");
  btn.type = "button";
  btn.addEventListener("click", async () => {
    if (await copyText(cmd)) {
      btn.textContent = "copied";
      btn.classList.add("done");
      setTimeout(() => { btn.textContent = "copy"; btn.classList.remove("done"); }, 1600);
    } else {
      input.focus();
      toast("Could not reach the clipboard. The command is selected, so copy it", true);
    }
  });
  row.appendChild(btn);
  box.appendChild(row);

  const hint = el("p", "local-hint");
  hint.appendChild(document.createTextNode("Needs cw on your machine: "));
  hint.appendChild(el("code", "md", "go install github.com/sjroesink/cw@latest"));
  if (state.data.meta && state.data.meta.locked) {
    hint.appendChild(document.createTextNode(
      ". This walkthrough is locked, so add --password if you had to type one to get in."));
  }
  box.appendChild(hint);
  return box;
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
  const intro = R.partIntro(p) || {};
  if (intro.long) v.appendChild(mdEl("p", "part-long", intro.long));

  const list = el("div", "sections");
  p.sections.forEach((s, si) => {
    const row = el("button", "section-row");
    row.type = "button";
    row.appendChild(el("span", "no", pad2(si + 1)));
    const mid = el("span", "mid");
    mid.appendChild(el("span", "t", s.title));
    const said = R.sectionIntro(s);
    if (said) mid.appendChild(mdEl("span", "d", said));
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
  v.appendChild(dots);

  v.appendChild(el("h2", "step-title", step.title));
  R.step(step, v);
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
  const next = el("button", "nav next", moveLabel(1));
  next.type = "button";
  next.addEventListener("click", () => move(1));
  foot.appendChild(next);
  return foot;
}

/* ------------------------------------------------------------------ settings */

// The theme lives in settings. This is the shortcut for it, and it saves the
// same way the sheet does, so the next start comes up in the theme you left.
async function toggleTheme() {
  state.data.settings.theme = effectiveDark() ? "light" : "dark";
  applyTheme(state.data.settings);
  render();
  try { await settingsStore.save(state.data.settings); }
  catch { /* the page still switched */ }
}

function openSettings() {
  const s = state.data.settings;
  // Hosted there is no machine to open a file on, so the editor half of the
  // sheet is not hidden as a disabled thing: it is simply not there.
  $("wrapIde").hidden = HOSTED;
  $("wrapIdePath").hidden = HOSTED;
  $("btnTestOpen").hidden = HOSTED;
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
  // Locally this is the file the settings are written to, which is worth
  // knowing. Hosted there is no file, and a line saying so is a line about
  // nothing.
  const where = HOSTED ? "" : (state.data.settingsPath || "");
  $("settingsPath").textContent = where;
  $("settingsPath").hidden = !where;

  const sw = $("swatches");
  sw.textContent = "";
  for (const a of ["oklch(0.52 0.14 255)", "oklch(0.58 0.14 25)", "oklch(0.5 0.11 190)", "oklch(0.48 0.12 300)"]) {
    const b = el("button", "swatch" + (a === s.accent ? " on" : ""));
    b.type = "button";
    b.style.background = a;
    b.setAttribute("aria-label", a);
    b.addEventListener("click", () => {
      state.data.settings.accent = a;
      applyTheme(state.data.settings);
      [...sw.children].forEach((c) => c.classList.toggle("on", c === b));
    });
    sw.appendChild(b);
  }
  syncIdeHint();
  $("settings").showModal();
}

function syncIdeHint() {
  if (HOSTED) return;
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
  const s = Object.assign({}, state.data.settings, { theme: $("setTheme").value });
  if (!HOSTED) {
    s.ide = $("setIde").value;
    s.ideCommand = $("setIdeCommand").value.trim();
    s.idePath = $("setIdePath").value.trim();
  }
  try {
    const r = await settingsStore.save(s);
    state.data.settings = r.settings;
    state.data.settingsPath = r.path;
    useLinks(state.data.github, state.data.settings.ide);
    applyTheme(state.data.settings);
    render();
    toast(HOSTED ? "Saved in this browser" : "Saved to " + r.path);
  } catch (err) {
    toast("Could not save: " + err.message, true);
  }
}

/* ------------------------------------------------------------------ start */

// firstCode walks the document for something to open, so the editor test has a
// real file behind it. What counts as code in a step is the renderer's answer.
function firstCode() {
  for (const p of parts()) {
    for (const s of p.sections) {
      for (const st of s.steps) {
        const found = R.stepCode(st);
        if (found) return found;
      }
    }
  }
  return null;
}

function fatal(message) {
  $("boot").textContent = "";
  const box = el("div");
  box.style.maxWidth = "620px";
  box.style.textAlign = "center";
  box.appendChild(el("b", null, "The walkthrough cannot be read."));
  box.appendChild(el("p", null, message));
  $("boot").appendChild(box);
}

async function load() {
  const data = await api(SOURCE);
  if (data.fatal) { fatal(data.fatal); return false; }

  state.data = data;
  state.data.ides = data.ides || [];
  state.stamp = data.stamp;
  // Hosted, the reader's own theme wins over the defaults the server sent.
  Object.assign(state.data.settings, settingsStore.stored() || {});

  const version = (data.doc && data.doc.version) || "";
  if (version !== "cw/1" && version !== "cw/2") {
    fatal("This walkthrough says its version is " + (version || "missing") +
      ", and this page can read cw/1 and cw/2. A reader that does not know a version refuses the file.");
    return false;
  }
  R = await rendererFor(version);
  useLinks(data.github, state.data.settings.ide);

  loadProgress();
  readHash();
  applyTheme(state.data.settings);

  $("boot").hidden = true;
  document.querySelector(".top").hidden = false;
  document.querySelector(".layout").hidden = false;
  problems();
  render();
  return true;
}

function wire() {
  onChange(render);
  $("btnHome").addEventListener("click", () => go({ view: "overview", part: null, section: null }));
  $("btnReload").addEventListener("click", () => location.reload());
  $("btnSettings").addEventListener("click", openSettings);
  $("setIde").addEventListener("change", syncIdeHint);
  $("settingsForm").addEventListener("submit", (e) => {
    if (e.submitter && e.submitter.value === "save") saveSettings();
  });
  $("btnTestOpen").addEventListener("click", async () => {
    const first = firstCode();
    if (!first) { toast("This walkthrough has no code to try", true); return; }
    const s = Object.assign({}, state.data.settings, {
      ide: $("setIde").value, ideCommand: $("setIdeCommand").value.trim(), idePath: $("setIdePath").value.trim(),
    });
    try {
      await settingsStore.save(s);
      state.data.settings = s;
      useLinks(state.data.github, s.ide);
      await openAt(first.file, first.line);
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
        const found = st && R.stepCode(st);
        if (found) openAt(found.file, found.line);
        break;
      }
      default: return;
    }
  });

  window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => {
    if (!document.documentElement.dataset.theme) render();
  });

  /* Locally the walkthrough is read from disk on every request, so an edit to
     the file or to the code it points at makes this page stale within seconds.
     Hosted, it only changes when somebody republishes, so the same check runs
     far less often and asks a smaller endpoint. */
  const stateURL = HOSTED ? SOURCE + "/state" : "/api/state";
  setInterval(async () => {
    if (document.hidden || !state.data || !$("btnReload").hidden) return;
    try {
      const r = await api(stateURL);
      if (r.stamp && r.stamp !== state.stamp) {
        $("btnReload").textContent = HOSTED ? "this was republished, reload" : "the code changed, reload";
        $("btnReload").hidden = false;
      }
    } catch (err) {
      if (!HOSTED && String(err.message || "").includes("token")) {
        $("btnReload").textContent = "the server restarted, reload";
        $("btnReload").hidden = false;
      }
    }
  }, HOSTED ? 30000 : 2500);
}

(async function main() {
  wire();
  try {
    await load();
  } catch (err) {
    $("boot").textContent = "The page could not start: " + (err.message || err);
  }
})();

// The whole point of the shell is that a second version of the format changes
// this file not at all. Everything a version is allowed to differ in is behind
// the five things a renderer answers: head, partIntro, sectionIntro, stepCode
// and step.
export { render, problems };
