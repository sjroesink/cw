/* The cw/2 step: a list of blocks in the order the author wrote them.

   This is a placeholder. The shell picks it from the version in the document,
   and it renders enough to prove that choice happens; the seven block builders
   are the next commit. */

import { el, mdEl } from "./ui.js";

export default {
  version: "cw/2",

  head(d) {
    const src = d.source || {};
    return { repo: src.repositoryUrl, number: src.label || src.identifier, url: src.url, state: src.state };
  },

  partIntro(p) { return { short: p.summary, long: p.description || p.summary }; },
  sectionIntro(s) { return s.summary; },
  stepCode() { return null; },

  step(step, host) {
    for (const b of step.blocks || []) {
      host.appendChild(b.text ? mdEl("p", "step-body", b.text) : el("p", "step-body", "(" + b.type + ")"));
    }
  },
};
