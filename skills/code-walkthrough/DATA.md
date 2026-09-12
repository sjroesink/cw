# The walkthrough file

One JSON file per topic. It holds everything the page shows: the page itself knows no titles, no
code and no diagrams. The contract is `walkthrough.v2.schema.json`, published at
`https://cw.roesink.dev/schema/v2.json`. Point at it from the file and your editor will validate
while you type.

This is the field reference. `RULES.md` is what makes a walkthrough worth reading; `FORMAT.md` is
for writing a second reader rather than a walkthrough.

```jsonc
{
  "$schema": "https://cw.roesink.dev/schema/v2.json",
  "version": "cw/2",                    // required, and what decides how the file is read
  "title": "feat: reliable webhook processing",
  "summary": "The paragraph on the overview, above the cards.",
  "language": "typescript",             // fallback for code blocks
  "source": { /* where it comes from, see below */ },
  "parts": [ /* see below */ ]
}
```

`cw/1` files still open and still publish, and they are documented in `FORMAT-v1.md`. Write new ones
as `cw/2`: a step is a list of blocks there, so it can say things the old fixed slots could not.

## `source`, where the walkthrough comes from

```jsonc
"source": {
  "kind": "pull-request",               // pull-request, commit, branch, subsystem, release, other
  "provider": "github",
  "repositoryUrl": "https://github.com/innovadis-dev/Fincent",
  "identifier": "4150",                 // the number, on its own
  "label": "PR #4150",                  // as it should read in the header
  "state": "open",                      // small chip: open, merged, draft
  "url": "https://github.com/innovadis-dev/Fincent/pull/4150",
  "revision": "9f2c1ab…",               // the commit every snippet was taken from
  "comparison": {                       // both ends, as commits rather than branch names
    "baseRevision": "3ab77e1…",
    "headRevision": "9f2c1ab…"
  },
  "changedFiles": [
    { "file": "src/http/verify.ts", "status": "modified" },
    { "file": "src/orders/store.ts", "status": "added" }
  ]
}
```

This is what turns a file and a line number into a link. With `changedFiles` and a pull request URL,
a snippet of changed code links into the diff; with `revision`, anything else links to a permalink
that stays right after the branch moves on. Without either, a snippet has nowhere to point.

`status` is one of `added`, `modified`, `deleted`, `renamed`, `copied`, `type-changed` or `other`,
and a rename or a copy also names `previousFile`.

`cw publish` fills in `revision`, `comparison`, `state` and `changedFiles` from git and `gh`, so
writing `kind`, `provider`, `repositoryUrl`, `label` and `url` by hand is enough. Publishing straight
at the API fills in nothing: write them yourself, or the page loses its links.

## Parts, sections, steps

Three levels, and each one is a screen the reader lands on.

```jsonc
"parts": [
  {
    "id": "signature-verification",              // required, and it never changes afterwards
    "title": "Signature verification",           // the card, and the rail
    "summary": "One line on the card.",
    "description": "The paragraph on the part page, where there is room for the why.",
    "files": ["verify-signature.ts", "webhook-keys.ts"],   // chips on the card
    "blocks": [ /* optional, above the section rows */ ],
    "sections": [
      {
        "id": "hmac-middleware",
        "title": "HMAC verification middleware",  // a row on the part page
        "summary": "One line under it.",
        "steps": [ /* see below */ ]
      }
    ]
  }
]
```

Three to five parts, two to four sections each, two to five steps each. The overview is a set of
cards to choose from: past six parts it becomes a list to scroll, and the validator says so.

**`id` is required, and unique across the whole document.** Not per level, the way `cw/1` had it:
a diagram links to a block by name, and nothing in a name says what kind of thing it points at. It
is a name that survives editing, because progress, bookmarks and deep links hang off it. Once it
exists, do not change it: changing an id loses every reader's place.

**The overview and a part page can hold blocks too.** `"blocks"` beside `"parts"` at the top of the
document is what the overview shows under its summary, and `"blocks"` in a part is what its page
shows above the section rows. The same seven, checked the same way, snippets and all.

Use one for the picture that only makes sense before the reader has been anywhere: the shape of the
subsystem, the four services and the one that changed. A diagram there can link into a step, so the
overview becomes a way in rather than a table of contents. What does not belong there is the content
of a step: if it takes a paragraph and a snippet to explain, it is a step, and the overview is
where somebody decides which part to read first.

## A step, and its blocks

```jsonc
{
  "id": "timing-safe-comparison",
  "title": "The comparison is timing-safe",
  "speech": "Optional. The same step written for an ear rather than a screen.",
  "blocks": [
    { "type": "markdown", "text": "Two or three sentences. What happens here, and why." },
    { "type": "code", "id": "the-compare", "snippet": { /* see below */ } },
    { "type": "callout", "severity": "warning", "text": "The thing that will bite the next person." }
  ]
}
```

The blocks render in the order you write them, as many as the step needs. That is the point of the
version: prose, a snippet, a sentence about it, a second snippet. A step made only of prose is
allowed and the validator warns, because the first step of a part is sometimes exactly that.

**`speech`** is only worth writing when the prose leans on what is on screen: "the highlighted line"
means nothing to somebody being read to. A reader with no screen falls back to the markdown blocks.

**`id` on a block** is only needed when something points at it, which today means a diagram link.

Seven types, and an eighth is not an extension but an invalid document. `extension` is where
anything the format has no opinion about goes.

## `markdown`, the prose

```jsonc
{ "type": "markdown", "text": "The handler puts the command on a queue. A worker runs it later." }
```

Paragraphs, `` `code` ``, `**bold**`, `*italic*`, `[links](url)`, lists one level deep, ATX
headings, fenced code, a quote and a rule. That list is the whole of it: anything past it shows as
the characters you typed. Raw HTML is text, never markup.

Two markdown blocks in a row are two paragraphs you chose to separate. Use that instead of writing
one long one.

## `code`, a snippet

```jsonc
{
  "type": "code",
  "id": "verify-hmac",                  // only needed if a diagram links to it
  "snippet": {
    "label": "The comparison",          // the chip next to the file name
    "language": "typescript",           // optional: worked out from the extension otherwise
    "text": "const expected = createHmac('sha256', key)\n  .update(payload)\n  .digest();\n",
    "source": {
      "file": "src/http/middleware/verify-signature.ts",
      "startLine": 18,                  // the real first line, so the gutter is honest
      "endLine": 20                     // filled in on publish
    },
    "hash": { "algorithm": "sha256", "value": "5f1c…" },   // filled in on publish
    "highlights": [
      { "lines": { "start": 2, "end": 3 } },               // focus, the lines the step is about
      { "lines": { "start": 3 }, "kind": "added" }         // marked with a +
    ],
    "annotations": [
      { "lines": { "start": 3 }, "text": "One sentence for what the code does not say itself." }
    ]
  }
}
```

**Line numbers inside a snippet are relative to the snippet.** Line 2 is the second line of `text`,
not line 2 of the file. This is the one thing most likely to go wrong coming from `cw/1`, where they
were the file's own numbering. The page shows `startLine + n - 1` in the gutter, because that is the
number you type into your editor, but the file never carries it.

**`source` is a claim that the snippet is a verbatim excerpt.** Leave it out for something that is
not really at a place in a file: pseudocode, a shape, an example, a config fragment. Then nothing
will tell the reader it went stale, which is right, because it was never in a file. Use several
snippets rather than one that stitches together lines that are not next to each other.

**`endLine` and `hash` are filled in for you**, by `cw publish`, by the API, or by
`cw migrate`. Do not write them by hand: if they disagree with `text` the document is refused, which
is the point of having them.

`highlights` come in two kinds. `focus` is the default and lights the line; `added` marks it with a
`+` and a green wash, for a line this change introduces. `annotations` hang off a range and open
under the snippet when the reader clicks the marked line.

**The snippet is a copy, and copies rot.** Locally the server looks for these exact lines in that
file on every load. Found where you said: nothing shown. Found somewhere else: the chip says moved.
Not there at all: the chip says so and the banner counts it. On a published page there is no working
tree, so what is shown is what the publisher's tree said at the time, recorded in `verification`
along with when and against which revision.

## `callout`, the sentence that matters more

```jsonc
{ "type": "callout", "severity": "warning", "title": "Optional", "text": "One or two sentences." }
```

`info` is context, `tip` saves the reader time, `warning` is about something that will go wrong
later, and `danger` is for what deletes data or lets somebody in. Use `danger` about twice a year.

## `diagram`, a mermaid picture

```jsonc
{
  "type": "diagram",
  "format": "mermaid",
  "alt": "A request passes through verifySignature before it reaches orderHandler.",
  "caption": "Only a verified request reaches orderHandler.",
  "text": "sequenceDiagram\n    participant R as router\n    R->>V: rawBody buffer",
  "links": [ { "nodeId": "verifySignature", "blockId": "verify-hmac" } ]
}
```

Any mermaid diagram works: flowchart, sequence, state, class. The page renders it with the same
theme variables in light and dark, and the reader can open the source.

**`alt` is required.** One sentence saying what the picture shows, not what it looks like. It is the
diagram for anybody who cannot see it, and the page prints it under the picture.

`links` is what makes it more than a picture. `nodeId` is the **node id** in a flowchart (`api[API
host]` is `api`) or the **exact label** anywhere else (a participant `participant V as
verifySignature` is `verifySignature`). `blockId` is the id of a `code` block, and it may be
anywhere in the document: clicking the shape takes the reader to that snippet, three steps away if
that is where it lives. In `cw/1` a diagram carried its own copy of the code, so the same lines were
pasted twice and went stale separately.

A `nodeId` that does not appear in `text` is a warning, because nothing will be clickable.

## `diff`, a before and after

```jsonc
{
  "type": "diff",
  "before": { "file": "src/orders/event-store.ts" },
  "after": { "file": "src/orders/event-store.ts" },
  "language": "typescript",
  "caption": "Optional.",
  "hunks": [
    {
      "oldStart": 12, "oldLines": 3,
      "newStart": 12, "newLines": 3,
      "heading": "processOnce",
      "lines": [
        { "kind": "context", "text": "export async function processOnce(event, handle) {" },
        { "kind": "delete",  "text": "  const seen = await db.findProcessedEvent(event.id);" },
        { "kind": "add",     "text": "  return db.transaction(async (tx) => {" },
        { "kind": "context", "text": "}" }
      ]
    }
  ]
}
```

Write the lines without their leading `+` or `-`; `kind` says which side they are on. The counts
have to agree with the lines: `oldLines` is the number of `context` plus `delete` lines, `newLines`
is `context` plus `add`. The validator checks that, because the counts are the half of a diff nobody
looks at and therefore the half that rots.

Omit `before` for a new file and `after` for a deleted one. Hunks walk down the file and may not
overlap. `noNewlineAtEnd` on a line is about the end of a file, so it only goes on the last line of
its side.

A diff is not verified against the tree: half of it is by definition not there any more. Use a
`code` block when what matters is the result, and a diff when what matters is the difference.

## `timeline`, a handful of frames

For something that happens over time and has no code to point at: a rollout, a retry ladder, a
migration window.

```jsonc
{
  "type": "timeline",
  "caption": "Optional.",
  "nodes": [ { "id": "key-a", "label": "key A" }, { "id": "key-b", "label": "key B" } ],
  "frames": [
    {
      "label": "t0 · before",
      "note": "What is true here.",
      "durationMs": 2500,
      "states": [
        { "nodeId": "key-a", "state": "active", "detail": "active" },
        { "nodeId": "key-b", "state": "idle", "detail": "not created" }
      ]
    },
    {
      "label": "t0 + deploy",
      "note": "What changed.",
      "states": [
        { "nodeId": "key-a", "state": "done", "detail": "previous" },
        { "nodeId": "key-b", "state": "active", "detail": "active" }
      ]
    }
  ]
}
```

**Every frame names every node, exactly once.** A node that is not doing anything is `idle`, not
absent. That is what lets a reader jump to the third frame without having seen the first two, and a
frame that leaves a node out is an error.

`state`: `idle` waiting, `active` happening now, `done` happened, `gone` removed, `alert` the
failure this frame is about. `durationMs` is a hint about that one frame.

## `extension`, for what the format has no opinion about

```jsonc
{
  "type": "extension",
  "name": "roesink/queue-simulator",
  "version": 1,
  "data": { "workers": 2, "tasks": 5 },
  "fallback": "Two workers take five tasks off one queue, one at a time each."
}
```

A reader that does not know both the `name` and the `version` shows the `fallback` instead. Every
reader you will meet today does exactly that, so **write the fallback as if it is the only thing
anybody will read**, because it is. An extension whose fallback does not stand on its own is not an
extension, it is a hole in the walkthrough.

## `ext`, the escape hatch

Every level takes an optional `ext` object, keyed by `owner/name`, and it is the only place the
schema does not check what you put. It is for a particular consumer's own data. Nothing a reader
needs in order to follow the walkthrough belongs in it, because every other consumer ignores it.

## What the page remembers

Progress is per walkthrough, in the reader's own browser: which steps are marked as read. Nothing
goes back to the server, and nothing is shared between readers. The address bar carries
`#part-id/section-id/step-id`, so a link to one step is a link to that step, and back and forward
walk the way the reader came.

Read locally, it remembers one more thing, and that one is not in the browser. A comment left on a
selection is kept by the server that is serving the walkthrough, beside the settings and filed under
the name it was published as, so it is still there the next time the same walkthrough is opened.
`cw comments` is how an agent reads and answers them, and an answer may be written in these same
blocks: a question about a function is answered best by that function, at its own line numbers.

None of that touches the document. A walkthrough with fifty comments on it publishes as the same
bytes as one with none: the comments are about a reading, and the file is about the change. Nothing
in this reference is where they go, and there is no field to add for them.
