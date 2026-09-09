# Writing a walkthrough

The rules that decide whether a walkthrough is worth someone's afternoon. They hold whether you are
writing it with the local tool or posting it straight at the API.

## Before you write anything

**Scope it in one line.** What is this change, said once. If that takes two sentences, you are
explaining two changes and they are two walkthroughs.

**Name the parts before writing any of them.** Three to five, and each one a thing a reviewer could
agree or disagree with on its own. For a pull request, the bug it fixes, the arrangement before it
and the arrangement after it are usually three of them.

**Read the code.** `gh pr diff <n>` and `gh pr view <n> --json files` for a pull request. For a
subsystem, trace it: per hop write down the file, the symbol, what it decides and what it hands on.
A walkthrough written off a diff summary reads like one.

## While you write

**Every claim has to be visible.** A sentence in a step is backed by that step's snippet, or by one
the reader has already passed. If it is backed by neither, either show the code or drop the claim.

**A step is one idea.** If the body needs three paragraphs, it is two steps. Two or three sentences
is the working length; past about 450 characters the column stops reading well.

**Paste only what you have read in full.** The snippet is a copy, so getting it wrong is invisible
until someone opens the file. Never reconstruct a snippet from memory or from a diff hunk; take the
lines out of the file.

**Say what changed, not what exists.** For a pull request, a snippet of untouched code needs a
`note` that says so, otherwise the reader assumes the change touched it.

**Every step earns its blocks.** A diagram that restates the prose costs the reader more than it
saves. A validator can warn about a step with nothing but prose; nothing can warn about a diagram
nobody needed.

**Line numbers are the numbers the gutter shows.** With `from` they are the file's own numbering,
without it they start at 1. `hi`, `add` and `notes` all use them.

**Light up the lines the step is about, and no more.** `hi` on twelve lines highlights nothing.

**At most a handful of line notes.** Past four they stop being asides and become the text.

## The prose itself

All prose in English, including for a Dutch reader, so the same walkthrough travels. Domain nouns
from the codebase stay exactly as they are in the code.

Write it the way you would explain it to the colleague sitting next to you: what happens, and why it
is done this way. No throat-clearing, no summary of what you are about to say, no closing paragraph
that says it again.

Avoid the tells that make a text read as generated: em dashes, "it's worth noting", "in essence",
"this ensures", triples of adjectives, and a sentence that restates the previous one in different
words. If a sentence would survive being deleted, delete it.

## The shape

- Three to five parts. Past six the overview stops being a set of cards to choose from.
- Two to four sections in a part. A part with one section is a section.
- Two to five steps in a section. A section with one step is a step; with nine it is two sections.

## Before you publish

Validate. Errors are the walkthrough being wrong, not the tool being difficult. `STALE` on a snippet
means it is not in the tree the way you pasted it, which on a pull request usually means you are on
the wrong branch: check the pull request out first.

Then read it as a reader would. Every diagram rendered, no step scrolling past two screens, no step
that made you scroll back to understand it.
