# The walkthrough file

One JSON file per topic. It holds everything the page shows: the page itself knows no titles, no
code and no diagrams. The contract is `walkthrough.schema.json`, published at
`https://cw.roesink.dev/schema/v1.json`. Point at it from the file and your editor will validate
while you type.

This is the field reference. `RULES.md` is what makes a walkthrough worth reading; `FORMAT.md` is
for writing a second reader rather than a walkthrough.

```jsonc
{
  "$schema": "https://cw.roesink.dev/schema/v1.json",
  "version": "cw/1",                    // required, and the only required version
  "title": "feat: reliable webhook processing",
  "source": { /* where it comes from, see below */ },
  "summary": "The paragraph on the overview, above the cards.",
  "root": "../..",                      // the checkout paths are relative to; --root wins
  "language": "typescript",             // fallback for code blocks
  "parts": [ /* see below */ ]
}
```

## `source`, where the walkthrough comes from

```jsonc
"source": {
  "kind": "pull-request",               // pull-request, commit, branch, subsystem, release, other
  "provider": "github",
  "repo": "innovadis-dev/Fincent",
  "number": "PR #4150",                 // as it should read in the header. Or a ticket key
  "state": "open",                      // small chip: open, merged, draft
  "url": "https://github.com/innovadis-dev/Fincent/pull/4150",
  "commit": "9f2c1ab…",                 // the commit every snippet was taken from
  "base": "main",
  "head": "feature/FIN-8107-webhooks",
  "changedFiles": ["src/http/verify.ts", "src/orders/event-store.ts"]
}
```

This is what turns a file and a line number into a link. With `changedFiles` and a pull request URL,
a snippet of changed code links into the diff; with `commit`, anything else links to a permalink
that stays right after the branch moves on. Without either, a snippet has nowhere to point.

`cw publish` fills in `commit`, `base`, `head`, `state` and `changedFiles` from git and `gh`, so
writing `kind`, `provider`, `repo`, `number` and `url` by hand is enough. Publishing straight at the
API fills in nothing: write them yourself, or the page loses its links.

## Prose, and the markdown in it

The fields that hold sentences (`summary`, `desc`, `long`, `body`, `callout`, a line note, a ref
note, a frame note) render a small inline subset of markdown: `` `code` ``, `**bold**`, `*italic*`
and `[text](url)`. Backticks around a name from the codebase are the one worth using; the others are
there so a sentence that needs them is not stuck.

Nothing block-level, so no lists and no headings inside a body. A body that wants a list wants to be
two steps. Titles take no markdown at all, because they also appear in the rail and in tooltips.

## Parts, sections, steps

Three levels, and each one is a screen the reader lands on.

```jsonc
"parts": [
  {
    "id": "signature-verification",              // optional, filled in on publish
    "title": "Signature verification",           // the card, and the rail
    "desc": "One line on the card.",
    "long": "The paragraph on the part page, where there is room for the why.",
    "files": ["verify-signature.ts", "webhook-keys.ts"],   // chips on the card
    "sections": [
      {
        "title": "HMAC verification middleware",  // a row on the part page
        "desc": "One line under it.",
        "steps": [ /* see below */ ]
      }
    ]
  }
]
```

Three to five parts, two to four sections each, two to five steps each. The overview is a set of
cards to choose from: past six parts it becomes a list to scroll, and the validator says so.

**`id`** is a name that survives editing. Progress, bookmarks and deep links hang off it, so a
published walkthrough has one on every part, section and step. Leave them out and they are derived
from the titles on publish; once they exist, do not change them, because changing an id loses every
reader's place.

## A step

```jsonc
{
  "title": "The comparison is timing-safe",
  "body": "Two or three sentences. What happens here, and why it is done this way.",
  "speech": "Optional. The same step written for an ear rather than a screen.",
  "callout": "The thing that will bite the next person.",
  "diagram": { /* mermaid */ },
  "code":    { /* a snippet */ },
  "diff":    { /* a before and after */ },
  "anim":    { /* frames */ }
}
```

Only `title` and `body` are required. The four blocks render in the order above, and a step usually
earns one or two of them. A step with none is prose, and the validator warns rather than refuses,
because the first step of a part is sometimes exactly that.

**`speech`** is only worth writing when `body` leans on what is on screen: "the highlighted line"
means nothing to someone being read to. A consumer that has no screen falls back to `body`.

## `code`, a snippet

```jsonc
"code": {
  "file": "src/http/middleware/verify-signature.ts",
  "from": 18,                       // the real first line, so the gutter is honest
  "to": 24,                         // filled in on publish
  "sha": "5f1c…",                   // filled in on publish
  "lang": "typescript",             // optional: worked out from the extension otherwise
  "note": "+24 lines",              // the chip next to the file name
  "hi": [22, 24],                   // lit lines: the ones the step is about
  "add": [23, 24, 25],              // lines this change adds, marked with a +
  "notes": [
    { "line": 24, "text": "One sentence for what the code does not say itself." }
  ],
  "text": "const expected = createHmac('sha256', key)\n  .update(payload)\n  .digest();"
}
```

**Line numbers are the numbers the gutter shows.** With `from` they are the file's own numbering,
without it they start at 1. `hi`, `add` and `notes` all use them, and a number outside the snippet
is an error rather than a highlight nobody sees.

Leave `from` out only for a snippet that is not really at a place in a file: a shape, an example, a
config fragment. Everything else gets one, because it is what the link and the tree check work from.

**`to` and `sha` are filled in for you**, by `cw publish` or by the API. Do not write them by hand:
if they disagree with `text` the document is refused, which is the point of having them.

The snippet is highlighted by language: `lang`, else the walkthrough's `language`, else the file
extension. An extension with no grammar in the bundle stays plain text, because guessing the
language of a snippet is how a comment turns into a string.

**The snippet is a copy, and copies rot.** Locally the server looks for these exact lines in that
file on every load. Found where you said: nothing shown. Found somewhere else: the chip says moved.
Not there at all: the chip says so and the banner counts it. Moving code is not changing it, which
is why the two are reported apart. On a published page there is no working tree, so what is shown is
what the publisher's tree said at the time, recorded per snippet in `check`.

## `diagram`, a mermaid picture

```jsonc
"diagram": {
  "kind": "sequence diagram",       // the label above it
  "caption": "Only a verified request reaches orderHandler.",
  "def": "sequenceDiagram\n    participant R as router\n    R->>V: rawBody buffer",
  "refs": {
    "router": {
      "label": "router",
      "file": "src/http/routes/webhooks.ts",
      "from": 8,
      "note": "Why this block is worth opening.",
      "code": "router.post(\n  '/webhooks/orders',\n  verifySignature,\n);"
    }
  }
}
```

Any mermaid diagram works: flowchart, sequence, state, class. The page renders it with the same
theme variables in light and dark, and the reader can open the source.

`refs` is what makes it more than a picture. Key it by the **node id** in a flowchart (`api[API
host]` is keyed `api`) or by the **exact label** anywhere else (a sequence participant `participant
V as verifySignature` is keyed `verifySignature`). That block becomes clickable and opens a short
peek at the code behind it. Keep the peek short: it is a glance from a diagram, not the snippet
panel.

A ref whose key does not appear in `def` is a warning, because nothing will be clickable.

## `diff`, a before and after

```jsonc
"diff": {
  "file": "src/orders/event-store.ts",
  "from": 12,
  "lines": [
    { "kind": "ctx", "t": "export async function processOnce(event, handle) {" },
    { "kind": "del", "t": "  const seen = await db.findProcessedEvent(event.id);" },
    { "kind": "add", "t": "  return db.transaction(async (tx) => {" }
  ]
}
```

`ctx` shows on both sides, `del` only on the left, `add` only on the right, and the reader can
switch between unified and side by side. Write the lines without their leading `+` or `-`.

A diff is not verified against the tree: half of it is by definition not there any more. Use `code`
with `note: "new file"` when what matters is the result, and a diff when what matters is the
difference.

## `anim`, a handful of frames

For something that happens over time and has no code to point at: a rollout, a retry ladder, a
migration window.

```jsonc
"anim": {
  "frames": [
    { "label": "t0 · before", "note": "What is true here.",
      "nodes": [ { "label": "key A", "sub": "active", "state": "active" },
                 { "label": "key B", "sub": "not created", "state": "idle" } ] },
    { "label": "t0 + deploy", "note": "What changed.",
      "nodes": [ { "label": "key A", "sub": "previous", "state": "done" },
                 { "label": "key B", "sub": "active", "state": "active" } ] }
  ]
}
```

Every frame holds **the same nodes in the same order**, and only their state changes. That is what
makes the transition read as one thing changing rather than two pictures. Different node counts
across frames is an error.

`state`: `idle` waiting, `active` happening now, `done` happened, `gone` removed, `alert` the
failure this frame is about.

## `ext`, the escape hatch

Every level takes an optional `ext` object, and it is the only place the schema does not check what
you put. It is for a particular consumer's own data. Nothing a reader needs in order to follow the
walkthrough belongs in it, because every other consumer will ignore it.

## What the page remembers

Progress is per walkthrough, in the reader's own browser: which steps are marked as read. Nothing
goes back to the server, and nothing is shared between readers. The address bar carries
`#part-id/section-id/step-id`, so a link to one step is a link to that step.
