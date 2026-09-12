# cw/2: the code walkthrough format

A walkthrough is one JSON document. It holds the whole content: the prose, the code, the diagrams
and the order they are read in. A program that shows a walkthrough holds no topic of its own, which
is what makes it possible for more than one program to show the same file.

This document is for someone writing a second reader. If you are writing a walkthrough, read
`DATA.md` instead: it is the field reference, and it says what good content looks like rather than
what is legal.

The contract is `walkthrough.v2.schema.json`, published at `https://cw.roesink.dev/schema/v2.json`.
Where this text and the schema disagree, the schema is right. What the schema cannot say is in
**Rules a schema cannot state**, and a reader may assume all of it.

## Versions

Every document carries `"version": "cw/2"`. A consumer that does not recognise the version refuses
the file. It does not guess, and it does not read the parts it happens to understand: a format it
does not know may well have changed what a field it does recognise means.

`$schema` beside it is what an editor follows while an author types. It is not what a reader acts
on: it can be a relative path, a stale URL, or absent. The version decides.

The promise for `cw/2`:

- No field is removed, and no field changes meaning.
- New fields are optional, and a document without them stays valid.
- New values may appear in an enum. A consumer treats a value it does not know the way it treats a
  block it cannot render: it falls back, it does not fail.
- Standard block types may be added within cw/2. This revision adds `reference` as the eighth
  type. Existing documents remain valid; older strict readers must be upgraded to accept
  documents using it. Readers should show an unsupported-block notice for unknown types.
  Custom content still uses `extension` with its required fallback.

This explicitly replaces the earlier cw/2 promise of a fixed seven-type vocabulary. Schema and
reader releases must accompany new standard types; version `cw/2` alone does not identify which
additions an installed reader supports.

Anything that breaks one of those gets a new version string, and both versions are served. `cw/1` is
still served, still read and still published: see `FORMAT-v1.md`.

## The shape

```
walkthrough
  version, title, summary?, language?, source?, ext?
  blocks[]?                   what the overview shows besides the parts
  parts[]                     one chapter, and one screen a reader lands on
    id, title, summary?, description?, files[]?, ext?
    blocks[]?                 what the part page shows above its sections
    sections[]                a run of steps about a single idea
      id, title, summary?, ext?
      steps[]                 one screen, one idea
        id, title, speech?, ext?
        blocks[]              what the step is made of, in the order it is read
          markdown | code | callout | diagram | diff | timeline | reference | extension
```

Three levels, and each one is somewhere a reader can be. Two to five steps in a section, two to four
sections in a part, three to six parts. Those are the numbers the content is written to, not rules
the schema enforces.

`root` is gone. Which checkout the paths hang off is a fact about the machine reading the document,
not about the document, so it belongs to the reader's own configuration.

## The blocks

A step is an ordered list. Two snippets with a paragraph between them is a sentence the format can
say, which is the whole reason this version exists.

| type | what it holds |
|---|---|
| `markdown` | `text`. Prose. See **Text** |
| `code` | `snippet`, which is text plus an optional place it came from |
| `callout` | `text`, `severity` (`info`, `tip`, `warning`, `danger`), an optional `title` |
| `diagram` | `format` (`mermaid`), `text`, a required `alt`, an optional `caption` and `links[]` |
| `diff` | `before`/`after` locations and `hunks[]` in unified-diff coordinates |
| `timeline` | `nodes[]` and `frames[]`, each frame a complete picture |
| `reference` | `title`, `target`, optional `description` and `relation`; see **References** |
| `extension` | `name`, `version`, `data`, and a `fallback` a reader can print |

A type absent from the current schema is invalid for that schema. Readers may encounter a newer
standard type and show an unsupported-block notice. Custom content uses `extension` and its fallback.

Blocks are not only a step's. The walkthrough and each part may carry a list of their own, drawn on
the screen a reader lands on rather than inside a step: the shape of the thing before anybody is
sent into it. Same seven types, same rules, and both lists are optional, so a document without them
is what every cw/2 document was until now. Two things follow. Ids are one namespace for the whole
document, so a diagram on the overview may link to a block three steps in, and the other way round.
And a snippet is a snippet wherever it sits: a consumer that checks snippets against a working tree
checks these too, and one that lists the files a walkthrough touches lists theirs.

## Rendering: what is required of a consumer

**Render what you understand, skip what you do not, and say nothing about it.** A reader that plays
a walkthrough out loud has no use for `timeline` and should not apologise for it. A reader that
cannot draw mermaid shows the diagram's `text` as text, which is still the thing being described,
and its `alt` as the sentence it is.

**Never invent an ordering.** Parts, sections, steps and blocks are read in array order. There is no
priority field and no dependency graph; if the order is wrong, the document is wrong.

**Render an extension's `fallback` when you do not support both its `name` and its `version`.** Not
a placeholder, not a warning, not nothing: the sentence the author wrote for exactly this. An
extension whose fallback does not stand on its own is a broken extension.

**Prefer `speech` when you are heard rather than read.** It is the same step written for an ear,
present only when the written form leans on what is on screen. Absent, fall back to the step's
markdown blocks rather than skipping the step.

**Treat `ext` as someone else's.** It is the one open object in the schema, its keys are namespaced
(`owner/name`), and a reader ignores the ones it did not put there and keeps them when it writes the
document back out. Never make a reader's ability to follow the walkthrough depend on one.

## Text

`markdown`, `callout.text`, an annotation, a caption, a summary, a description, a frame note and an
extension's fallback all hold CommonMark. Raw HTML in them is text, not markup, and a reader that
builds nodes rather than a string of HTML cannot get that wrong.

A reader has to handle this much:

| | |
|---|---|
| paragraphs | with a hard break on a line ending in two spaces or a backslash |
| `` `code` ``, `**bold**`, `*italic*`, `[text](url)` | inline, and `\` escapes any of their markers |
| ATX headings | `#` to `######`, mapped under whatever heading the step itself already has |
| lists | ordered and unordered, one level of nesting |
| fenced code | with an optional language |
| blockquote and thematic break | |

Anything else it shows as the characters that are there, which is conformant and is what the
render-what-you-understand rule already allows. A reader may do more; none of them may do less.

A title holds none of it. Titles end up in menus, breadcrumbs and tooltips, where markup is noise.

The prose is written in English even for a Dutch team, so the same walkthrough travels. Domain nouns
from the codebase stay exactly as they are in the code.

## Code, and how much to trust it

`snippet.text` is a copy of the source, pasted at the time of writing. It is what makes a
walkthrough readable without a checkout, and it is the part that rots.

**A snippet with a `source` is a claim that it is a verbatim excerpt.** Without one it is an
illustration: pseudocode, a shape, a config fragment. Do not try to resolve it, and do not tell a
reader it went stale, because it was never in a file to begin with. Use several snippets rather than
one that stitches together lines that are not next to each other.

| field | |
|---|---|
| `source.file` | repository-relative, forward slashes, no `.` or `..` segment |
| `source.startLine`, `source.endLine` | the first and last line, in the file's own numbering |
| `hash` | sha256 of the text, CRLF folded to LF, **trailing newline kept** |
| `verification` | what a working tree said about it, when, and against what |

**Line numbers inside a snippet are relative to the snippet.** A highlight or an annotation at line
2 means the second line of the text, not line 2 of the file. The line in the file is
`source.startLine + n - 1`, and a reader showing a gutter should show that, because it is the number
somebody types into their editor. This is the one rule most likely to be got wrong on the way from
`cw/1`, where those numbers were absolute.

**Counting and hashing follow different rules, on purpose.** For counting, fold CRLF to LF and do
not count a trailing newline as an extra empty line: three lines ending in a newline are three
lines. For hashing, fold CRLF to LF and keep the trailing newline, so that a file which ends with
one and a file which does not are not the same file. `cw/1` trimmed before hashing and lost that.

A consumer with the repository in front of it should prefer the file over the paste, and `hash` is
how it decides. Hash `startLine` to `endLine` the same way and compare. Equal: show the file.
Different: look for the pasted text elsewhere in the file, and say whether it moved or is gone.

`verification` is that same question answered once, by the publisher, at a moment that has passed.
`match`, `moved`, `different`, `missing-file` or `unavailable`, with `checkedAt` and an `against`
naming the revision. It is provenance rather than content: a consumer holding the repository works
the answer out for itself, and one without a checkout can at least say that a snippet was already
out of date when it was written down. **Absent means unchecked**, and a reader must not read that as
a clean bill of health.

`diff` is never verifiable: half of it is by definition no longer in the tree.

## Diffs

A hunk says how big it is twice, once in its counts and once in its lines, and both have to agree:
`oldLines` is the number of `context` plus `delete` lines, `newLines` is `context` plus `add`. Hunks
walk down a file, so each starts after the last one ended on both sides.

`before` and `after` are locations without line ranges; the hunks carry the coordinates. Omit
`before` for a new file and `after` for a deleted one, and then every line is an add or a delete
respectively. Empty `hunks` is a real thing to say: a file that was renamed and not otherwise
touched.

`noNewlineAtEnd` is about the end of a file, so it can only be set on the last line of its side.

## Timelines

A frame is a complete snapshot: every node the timeline defines appears exactly once, and a node
that is not doing anything is `idle` rather than absent. That is what lets a reader be dropped into
the third frame without having drawn the first two, and it is why a frame that leaves a node out is
a broken frame rather than an implied one.

Node ids are local to their timeline. `durationMs` is a hint about one frame, not a speed for the
whole run. Without animation, show the frames in order.

## Linking back to the source

`source` is what turns a path and a line into a URL. It says where things are in URLs and revisions
rather than in one forge's shorthand, so a walkthrough about something that is not on GitHub is
still saying something true.

A location may name its own repository and revision. What it leaves out falls back:

| field | falls back to |
|---|---|
| `location.repositoryUrl` | `source.repositoryUrl` |
| a code snippet's revision | `source.revision`, then `source.comparison.headRevision` |
| a diff's `before` revision | `source.comparison.baseRevision` |
| a diff's `after` revision | `source.comparison.headRevision`, then `source.revision` |
| language | the document's `language`, then plain text |

**Naming a different repository turns revision inheritance off.** One walkthrough can then explain
code from more than one repository without quietly resolving the second one at the first one's
commit.

With `changedFiles` and a pull request URL, a snippet of changed code links into the diff; with a
revision, anything else links to a permalink that stays right after the branch moves on. On GitHub
the second is `…/blob/<revision>/<path>#L<from>-L<to>`, and the first is
`…/pull/<n>/files#diff-<sha256 of the path>R<line>`. That anchor is not documented by GitHub, so
treat it as a convenience that may stop working, and keep the permalink as the fallback.

## Identity

`id` is required on a part, a section and a step, and on a block that anything points at. It is a
name that survives editing, and progress, bookmarks and deep links hang off it.

**One namespace for the whole document.** `cw/1` gave each level its own, which was enough while
only steps were addressable. A diagram link names a block, and nothing in the name says what kind of
thing it is pointing at, so all of them share one space.

A step id is addressed as `part-id/section-id/step-id`. A tool that fills in a missing id derives it
from the title and **never rewrites one that is already there**: changing an id loses every reader's
place. A consumer that stores progress stores ids.

Timeline node ids are the exception. They are local to their timeline and do not enter the
document's namespace, because they name a box in a picture rather than a thing anybody links to.

## Rules a schema cannot state

A reader may assume all of these, and a validator should check them:

- ids are unique across parts, sections, steps and blocks together
- `diagram.links[].blockId` names a block that exists, and it is a `code` block
- a line range ends at or after it starts, and highlights and annotations fall inside their snippet
- `source.endLine - source.startLine + 1` equals the number of lines in the text
- `hash` matches the text
- per hunk, `oldLines` is context plus delete and `newLines` is context plus add
- hunks ascend and do not overlap, and `noNewlineAtEnd` is only on the last line of its side
- every timeline frame holds every node of its timeline exactly once, and no others

## Coming from cw/1

Mechanically, for the most part:

| cw/1 | cw/2 |
|---|---|
| `step.body` | a `markdown` block |
| `step.callout` | a `callout` with `severity: "warning"` |
| `step.code` | a `code` block; `file`/`from`/`to` become `snippet.source` |
| `code.hi`, `code.add` | `highlights` with `kind` `focus` and `added` |
| `code.notes` | `annotations`, on a range rather than a line |
| `desc`, `long` | `summary`, `description` |
| `diagram.refs` | a `code` block each, plus `diagram.links` |
| `anim` | a `timeline` with stable node ids |
| `source.commit` | `source.revision` |
| `root` | reader configuration, outside the document |

Three things do not come across on their own, and a migrator must not invent them. Line numbers have
to be made snippet-relative. Hashes have to be recomputed, because the rule changed. A `cw/1` diff
has one starting number for both sides, and `cw/2` needs coordinates for each. Missing verification
dates and revisions are not there to be guessed at either: leave the field out, and the next publish
against a real working tree fills it in.

`cw migrate --to cw/2` does the mechanical part and prints a numbered list of what it would have had
to guess.


## References

`reference` is a standard cw/2 block, not an extension. It links to another complete walkthrough;
it never imports blocks, alters reading order, or automatically loads a dependency.

```json
{
  "type": "reference",
  "title": "Authentication",
  "description": "Follow token validation in more detail.",
  "relation": "deep-dive",
  "target": {
    "file": "./authentication.json",
    "url": "https://cw.roesink.dev/w/authentication",
    "stepId": "validate-token"
  }
}
```

The URL above is illustrative. `title` is nonempty plain text. `description` is optional CommonMark.
`relation` is optional: `related` (default), `deep-dive`, `prerequisite`, or `next`. An unknown relation
renders as `related`. These labels describe intent; they do not enforce prerequisites or sequencing.
As with other blocks, `id` and namespaced `ext` are optional.

`target` requires at least one of `file` and `url`. If both are present, the author asserts they
represent the same walkthrough; no revision pinning or identity check is implied. A local reader
prefers a readable, valid file and otherwise uses the URL. A hosted reader uses the URL. A downloaded
cache does not give a published document a local base directory.

`file` is relative to the containing walkthrough's directory, not its code repository root or the
process working directory. It follows the portable path rules, with an optional leading `./`.
Parent, empty and interior dot segments are forbidden. Readers enforce directory containment after
resolving symlinks. The file must contain walkthrough JSON; its extension is not significant.
`url` is an absolute HTTP(S) reader URL; readers must not forward authentication to another origin.

`stepId` identifies a step in the target document, never in the referring document's ID namespace.
Omitting it opens the target overview. A missing step opens the overview with a notice. The web
reader accepts `#step=<percent-encoded-id>` for this purpose; existing full step addresses still work.

Navigation happens only after an explicit user action. Readers preserve the original position and
completion state and provide a way back. A web reader may retain the current tab and open a published
URL in a separate tab, so a locked or unavailable target cannot replace the current walkthrough.
Unavailable files or URLs show an actionable error without invalidating the referring document.
References may form cycles: readers must not recursively fetch, validate or expand their targets.
Structural validation never requires network access or the target files to exist.

Authors keep each walkthrough self-contained. References add useful context or further reading;
they do not replace the explanation needed to understand the current scope.
