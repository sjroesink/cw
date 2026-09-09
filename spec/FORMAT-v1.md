# cw/1: the code walkthrough format

> This is the older version, kept because documents written in it are still published and
> still read. `FORMAT.md` is `cw/2`, which is what a new walkthrough is written in.

A walkthrough is one JSON document. It holds the whole content: the prose, the code, the diagrams
and the order they are read in. A program that shows a walkthrough holds no topic of its own, which
is what makes it possible for more than one program to show the same file.

This document is for someone writing a second reader. If you are writing a walkthrough, read
`DATA.md` instead: it is the field reference, and it says what good content looks like rather than
what is legal.

The contract is `walkthrough.schema.json`, published at `https://cw.roesink.dev/schema/v1.json`. Where
this text and the schema disagree, the schema is right.

## Versions

Every document carries `"version": "cw/1"`. A consumer that does not recognise the version refuses
the file. It does not guess, and it does not read the parts it happens to understand: a format it
does not know may well have changed what a field it does recognise means.

The promise for `cw/1`:

- No field is removed, and no field changes meaning.
- New fields are optional, and a document without them stays valid.
- New values may appear in an enum. A consumer treats a value it does not know the way it treats a
  block it cannot render: it falls back, it does not fail.

Anything that breaks one of those gets a new version string, and both versions are served.

`cw/1` widened once, in September 2026: the prose fields went from plain text to the small inline
Markdown subset under **Text**. No document changed, none became invalid, and a consumer that keeps
printing the source shows the same characters it showed before, because prose about code was already
written with backticks around the names. That is the bar for widening inside a version rather than
minting a new one.

## The shape

```
walkthrough
  version, title, source?, summary?, root?, language?, ext?
  parts[]                     one chapter, and one screen a reader lands on
    id?, title, desc?, long?, files[]?, ext?
    sections[]                a run of steps about a single idea
      id?, title, desc?, ext?
      steps[]                 one screen, one idea
        id?, title, body, speech?, callout?, ext?
        diagram?  code?  diff?  anim?
```

Three levels, and each one is somewhere a reader can be. Two to five steps in a section, two to four
sections in a part, three to six parts. Those are the numbers the content is written to, not rules
the schema enforces.

## Rendering: what is required of a consumer

**Render what you understand, skip what you do not, and say nothing about it.** A reader that plays
a walkthrough out loud has no use for `anim` and should not apologise for it. A reader that cannot
draw mermaid shows `diagram.def` as text, which is still the thing being described.

**Never invent an ordering.** Parts, sections and steps are read in array order. There is no
priority field and no dependency graph; if the order is wrong, the document is wrong.

**`body` is the step.** `title`, `callout` and the four blocks are all around it. A consumer that
shows only one thing per step shows `body`.

**Prefer `speech` when you are heard rather than read.** It is the same step written for an ear,
present only when the written form leans on what is on screen. Absent, fall back to `body` rather
than skipping the step.

**Prose is a small Markdown subset, and never HTML.** See **Text**. Rendering the source exactly as
it stands is conformant, and plainer is not wrong. Rendering it as HTML is not conformant: the
document was written by somebody else, and `<img src=x onerror=...>` in a body is characters an
author typed.

**Treat `ext` as someone else's.** It is the one open object in the schema. Read the key you put
there, ignore the rest, and never make a reader's ability to follow the walkthrough depend on it.

## Code, and how much to trust it

`code.text` is a copy of the source, pasted at the time of writing. It is what makes a walkthrough
readable without a checkout, and it is the part that rots.

A snippet that has a place in a file says so in full:

| field | |
|---|---|
| `file` | repository-relative, forward slashes |
| `from` | the first line, in the file's own numbering |
| `to` | the last line |
| `sha` | sha256 over the text, CRLF folded to LF, trailing newlines removed |
| `check` | what the publisher's tree said about it when it was published |

A consumer with the repository in front of it should prefer the file over the paste, and `sha` is
how it decides. Hash the lines `from` to `to` the same way and compare:

- **equal**: show the file, the paste and the file agree.
- **different**: the code moved or changed. Look for the pasted text elsewhere in the file. Found,
  it moved, so renumber and say so. Not found, it is gone, so show the paste and mark it as history.
- **no `sha`**: the document was written by hand and never published through a tool that fills them
  in. Show the paste.

`check` is the same question answered once, by the publisher, at the moment they published:
`ok`, `moved`, `gone`, `missing-file`, `outside-root` or `unchecked`. It is provenance rather than
content. A consumer holding the repository ignores it and works the answer out for itself; one
without a checkout can at least tell the reader that a snippet was already out of date when it was
written down. Never treat it as current: it is a statement about a moment that has passed.

A snippet without `from` is a shape, an example or a config fragment rather than a location. Do not
try to resolve it.

`diff` is never verifiable: half of it is by definition no longer in the tree. Write the lines
without their leading `+` or `-`; `kind` says which side they are on.

## Linking back to the source

`source` is what turns a path and a line into a URL. `kind` says what is being explained,
`provider` says who hosts it, and for a pull request `commit` and `changedFiles` are what make a
precise link possible:

- The file is in `changedFiles`: link into the diff, so the reader sees the change and not only the
  result.
- Otherwise: link to the file at `commit`, which stays correct after the branch moves on.
- No `commit`: link to `source.url` and stop there. Do not link to a branch tip and call it a
  permalink.

On GitHub the second is `…/blob/<commit>/<path>#L<from>-L<to>`, and the first is
`…/pull/<n>/files#diff-<sha256 of the path>R<line>`. That anchor is not documented by GitHub, so
treat it as a convenience that may stop working, and keep the permalink as the fallback.

## Identity

`id` on a part, a section or a step is a name that survives editing. Progress, bookmarks and deep
links hang off it, so:

- An id is unique among its siblings, not across the document. A step id is addressed as
  `part-id/section-id/step-id`.
- A tool that fills in missing ids derives them from the title, and **never rewrites one that is
  already there**. Changing an id loses every reader's place.
- A consumer that stores progress stores ids. Storing indices means inserting one step silently
  moves everybody.

Ids are optional in the file because they are tedious to write by hand. They are filled in on
publish, and a document that has been published has them.

## Text

The prose fields hold a small inline subset of Markdown, and that subset is the whole list:

| | |
|---|---|
| `` `code` `` | a name from the codebase, mid-sentence |
| `**bold**` | |
| `*italic*`, `_italic_` | `_` only against a non-word character, so `snake_case` is left alone |
| `[text](url)` | `http`, `https`, `mailto`, or a path |
| `\`` `\*` `\_` `\[` `\\` | the character itself |

Nothing block-level. No headings, no lists, no images, no HTML. These fields are one paragraph of
prose about code, and the format has real blocks for everything they are prose about.

The fields are `summary`, a part's `desc` and `long`, a section's `desc`, a step's `body` and
`callout`, `code.notes[].text`, `diagram.refs[].note` and `anim.frames[].note`. A title is not one of
them and holds none of it: titles end up in menus, breadcrumbs and tooltips, where markup is noise.

A consumer may do less. Printing the source as it stands is conformant. What a consumer must not do
is treat the text as HTML, and a consumer that builds nodes rather than a string never can.

Line breaks in `body` are the author's, and `\n` in `diagram.def` and `code.text` is a real newline.

The prose is written in English even for a Dutch team, so the same walkthrough travels. Domain nouns
from the codebase stay exactly as they are in the code.
