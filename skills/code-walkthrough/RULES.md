# Writing a walkthrough

The rules that decide whether a walkthrough is worth someone's afternoon. They hold whether you are
writing it with the local tool or posting it straight at the API.

## Before you write anything

**Define the boundary.** State in `summary` what the reader will understand, where the flow
starts and ends, and which adjacent concerns are intentionally outside it. Assume no access
to the author's conversation. Use existing schema fields; do not invent a `scope` field.

**Pull request mode.** Identify the repository and PR, capture its head and comparison
revisions, and inspect changed paths, including renames and deletions. Read actual head
files and relevant base files as well as the diff. Explain the behavioral change, why it
matters and its consequences. Several concerns in one PR can be parts of the same document.
Include unchanged code only to explain a caller, contract or effect of the change, and label
it as context. Inspect relevant tests; distinguish reading a test from executing it.

**Component mode.** Identify a path, symbol or concrete behavior. Follow input or entry
point, important decisions and state changes, result, and relevant failure behavior.
Read the callers and dependencies needed to establish that flow. Stop at unrelated component
boundaries and explain their interface contract rather than their internals. For example,
signature verification may need its middleware caller and failure response, but not a tour
of order storage or deployment. Record each hop's file, symbol, decision and next hop.

Use the requested scope when explicit. Otherwise infer the narrowest useful boundary from
the request and code and state it. Ask for the missing target only when ambiguity would
lead to explaining a different PR or component. Do not split a requested topic merely
because its scope needs more than one sentence.

**Tie evidence to a snapshot.** Record repository-relative paths and the revision actually
read. A branch name alone is not immutable. Describe local edits when they matter; do not
claim HEAD contains those edits. Keep machine-specific checkout paths outside the JSON.
Before-state code needs its own source revision, or a diff with the before/after revisions;
never present deleted code as current code at the PR head.

## While you write

**Every claim has to be visible.** A sentence in a step is backed by that step's snippet, or by one
the reader has already passed. If it is backed by neither, show the evidence or qualify the
claim. Label interpretation and uncertainty; do not infer guarantees from function names.

**A step is one idea.** Split when the reader needs a new idea or location, not to meet a
word count. Keep prose concise and explain why each jump to another code location matters.

**Paste only what you have read in full.** The snippet is a copy, so getting it wrong is invisible
until someone opens the file. Never reconstruct a snippet from memory or from a diff hunk; take the
lines out of the file. Each sourced snippet is contiguous; use separate ordered blocks for
separate locations. Label pseudocode and examples explicitly and omit their snippet `source`.

**Say what changed, not what exists.** For a pull request, a snippet of untouched code needs a
`snippet.label` or adjacent prose that says so, otherwise the reader assumes the change touched it.

**Every step earns its blocks.** A diagram that restates the prose costs the reader more than it
saves. A validator can warn about a step with nothing but prose; nothing can warn about a diagram
nobody needed.

**cw/2 ranges are snippet-relative.** `highlights[].lines` and `annotations[].lines` start at
1. The editor gutter shows `snippet.source.startLine + relativeLine - 1`. For a snippet
starting on file line 80, its third line uses `{"lines":{"start":3}}`, not 82.
The old `from`, `hi`, `add` and `notes` fields do not belong in a cw/2 snippet.

**Light up the lines the step is about, and no more.** Broad highlights hide the focus.

**At most a handful of line notes.** Past four they stop being asides and become the text.

## The prose itself

Use English prose by default unless the user requests another language. Domain nouns
from the codebase stay exactly as they are in the code.

Write it the way you would explain it to the colleague sitting next to you: what happens, and why it
is done this way. No throat-clearing, no summary of what you are about to say, no closing paragraph
that says it again.

Avoid the tells that make a text read as generated: em dashes, "it's worth noting", "in essence",
"this ensures", triples of adjectives, and a sentence that restates the previous one in different
words. If a sentence would survive being deleted, delete it.

## The shape

Use the required parts/sections/steps structure without quotas. One part with one section
and one step is valid. Name parts around behavior or decisions, not a directory inventory.
Do not pad a small PR with a repository tour. Diagrams should explain relationships,
branches or sequence; mark external context and link relevant nodes to existing code block IDs.

## Before handing it over

Validate against cw/2 and run `cw check` against the snapshot named by the document.
Investigate stale snippets before editing anchors. A different working tree is not proof
the excerpt is wrong. Use a matching checkout or isolated worktree when necessary;
preserve the user's active checkout. Report unavailable or unresolved verification.

Read from entry to result: can someone follow it without the chat or an unrelated walkthrough?
Does each included file or block contribute to the scope? Are necessary branches and boundary
contracts explained, and unrelated architecture left outside? Verify source locations,
snippet-relative annotations, diff sides and diagram links. Render diagrams and check native
code navigation when a reader is available. State what was inspected and what was executed.

Deliver a portable JSON file containing its explanation, snippets and diagrams. Publishing
follows the user's requested delivery; it is not required for a complete walkthrough.
