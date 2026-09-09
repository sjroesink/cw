# The walkthrough file

One JSON file per topic. It holds everything the page shows: the page itself knows no
titles, no code and no diagrams. `examples/cw-itself.json` is a complete worked example
that explains the server serving it, so it resolves on any machine that has this skill.

The contract is `server/schema/walkthrough.schema.json`. Point at it from the file and
your editor will validate while you type:

```jsonc
{
  "$schema": "../server/schema/walkthrough.schema.json",
  "title": "feat: reliable webhook processing",
  "repo": "innovadis-dev/Fincent",     // header crumb, left of the number
  "number": "PR #4150",                // or a ticket key
  "state": "open",                     // small chip: open, merged, draft
  "url": "https://github.com/…/pull/4150",
  "summary": "The paragraph on the overview, above the cards.",
  "root": "../..",                     // the checkout paths are relative to; --root wins
  "language": "typescript",            // fallback for code blocks
  "parts": [ /* see below */ ]
}
```

`cw schema --write` drops a copy next to the settings file and prints the path, for a
walkthrough that does not sit near the skill.

## Parts, sections, steps

Three levels, and each one is a screen the reader lands on.

```jsonc
"parts": [
  {
    "title": "Signature verification",             // the card, and the rail
    "desc": "One line on the card.",
    "long": "The paragraph on the part page, where there is room for the why.",
    "files": ["verify-signature.ts", "webhook-keys.ts"],   // chips on the card
    "sections": [
      {
        "title": "HMAC verification middleware",   // a row on the part page
        "desc": "One line under it.",
        "steps": [ /* see below */ ]
      }
    ]
  }
]
```

Three to five parts, two to four sections each, two to five steps each. The overview is
a set of cards to choose from: past six parts it becomes a list to scroll, and the
validator says so.

## A step

```jsonc
{
  "title": "The comparison is timing-safe",
  "body": "Two or three sentences. What happens here, and why it is done this way.",
  "callout": "The thing that will bite the next person.",
  "diagram": { /* mermaid */ },
  "code":    { /* a snippet */ },
  "diff":    { /* a before and after */ },
  "anim":    { /* frames */ }
}
```

Only `title` and `body` are required. The four blocks render in the order above, and a
step usually earns one or two of them. A step with none is prose, and the validator
warns rather than refuses, because the first step of a part is sometimes exactly that.

## `code`, a snippet

```jsonc
"code": {
  "file": "src/http/middleware/verify-signature.ts",
  "from": 18,                       // the real first line, so the gutter is honest
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

**Line numbers are the numbers the gutter shows.** With `from` they are the file's own
numbering, without it they start at 1. `hi`, `add` and `notes` all use them, and a
number outside the snippet is an error rather than a highlight nobody sees.

Leave `from` out only for a snippet that is not really at a place in a file: a shape, an
example, a config fragment. Everything else gets one, because it is what the open button
and the tree check work from.

At most a handful of `notes`. Past that they stop being asides and become the text.

The snippet is highlighted by language: `lang`, else the walkthrough's `language`, else
the file extension. An extension with no grammar in the bundle stays plain text, because
guessing the language of a snippet is how a comment turns into a string.

**The snippet is a copy, and copies rot.** On every load the server looks for these exact
lines in that file. Found where you said: nothing shown. Found somewhere else: the chip
says moved. Not there at all: the chip says so and the banner counts it. Moving code is
not changing it, which is why the two are reported apart.

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

Any mermaid diagram works: flowchart, sequence, state, class. The page renders it with
the same theme variables in light and dark, and the reader can open the source.

`refs` is what makes it more than a picture. Key it by the **node id** in a flowchart
(`api[API host]` is keyed `api`) or by the **exact label** anywhere else (a sequence
participant `participant V as verifySignature` is keyed `verifySignature`). That block
becomes clickable and opens a short peek at the code behind it, with its own open button.
Keep the peek short: it is a glance from a diagram, not the snippet panel.

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

`ctx` shows on both sides, `del` only on the left, `add` only on the right, and the
reader can switch between unified and side by side. Write the lines without their leading
`+` or `-`.

A diff is not verified against the tree: half of it is by definition not there any more.
Use `code` with `note: "new file"` when what matters is the result, and a diff when what
matters is the difference.

## `anim`, a handful of frames

For something that happens over time and has no code to point at: a rollout, a retry
ladder, a migration window.

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

Every frame holds **the same nodes in the same order**, and only their state changes.
That is what makes the transition read as one thing changing rather than two pictures.
Different node counts across frames is an error.

`state`: `idle` waiting, `active` happening now, `done` happened, `gone` removed,
`alert` the failure this frame is about.

## What the page remembers

Progress is per walkthrough, in the reader's own browser: which steps are marked as read.
Nothing goes back to the server, and nothing is shared between readers. The address bar
carries `#part-section-step`, so a link to one step is a link to that step.

## Settings, which are about the machine and not the topic

One settings file per user, not per walkthrough:
`%APPDATA%\code-walkthrough\settings.json` on Windows, `~/.config/…` elsewhere.

| field | what it does |
|-------|--------------|
| `ide` | `auto`, a catalogue id (`vscode` `cursor` `zed` `rider` `idea` `webstorm` `goland` `pycharm` `phpstorm` `clion` `fleet` `sublime` `windsurf` `vscodium` `vscode-insiders` `helix` `visualstudio`), or `custom` |
| `idePath` | override the executable that was found |
| `ideCommand` | for `custom`: a command line with `{file}` `{line}` `{col}` `{fileUri}` `{slashed}` |
| `theme` | `auto` `light` `dark` |
| `accent` | one of the four the design ships with, or your own oklch value |
| `port` | 0 picks a free one |
| `openBrowser` | launch a browser when the server starts |
| `offline` | never fetch mermaid or the typefaces, use the cache only |

On first run there is no settings file. The server looks at the terminal it was started
from, then PATH, then the places the installers put things, and writes what it found.

## What the server will not do

- Read or open anything outside the root. Paths are joined, cleaned and then verified to
  still be inside; one that climbs out is refused rather than clamped.
- Answer a request from another page. Everything under `/api` needs the token stamped
  into the page at load, and a cross-origin caller is turned away first.
- Listen anywhere but `127.0.0.1`.
- Write to your code. The only things it writes are the settings file and the asset cache.
