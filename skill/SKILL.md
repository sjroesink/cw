---
name: code-walkthrough
description: "Turn a pull request or a subsystem into a page someone can read at their own pace: parts on an overview, sections inside a part, and steps that carry the prose, a mermaid diagram, the code, a diff or a small animation. Progress is remembered, every snippet has an open in IDE button, and the whole content is one JSON file written against a schema. Use for 'maak een code walkthrough van deze PR', 'leg dit PR uit voor het team', 'walk me through this change', '/code-walkthrough X'."
---

# code-walkthrough

You turn a change into a page a reviewer can work through: an overview of what the
change is made of, and inside each part the sections and steps that explain it, with
the code, a diagram, a diff or an animation next to the prose.

**The UI and the server already exist.** `server/` is a dependency-free Go binary that
serves the page, opens files in the reader's editor and checks the pasted snippets
against the working tree. Your job per topic is a single `walkthrough.json`, written
against `server/schema/walkthrough.schema.json`.

The design came from a Claude Design project and is reproduced as it was drawn: Sora
and JetBrains Mono, the oklch palette, the three views, the progress bar. Change the
data, not the page.

## Which of the two skills

`visually-explain-code` and this one both explain code, and they are not the same tool.

| | `visually-explain-code` | `code-walkthrough` |
|---|---|---|
| the code | anchors, read live from the tree, never pasted | pasted into the JSON, checked against the tree on every load |
| the picture | one scene that lights up and animates per step | a mermaid diagram per step, with clickable blocks |
| the shape | one run of steps, or parts | parts, sections, steps, with a progress bar |
| the reader | watches it move, in one sitting | works through it, comes back tomorrow |

Pick this one for a pull request several people will read, for onboarding material, or
whenever a diff belongs in the story. Pick the other when the code has to be guaranteed
current, or when the point is a single picture the reader watches move.

## Rules

- **Read the code before you write about it.** Every claim in a step has to be visible
  in that step's snippet, or in one the reader has already passed.
- **A step is one idea.** If the body needs three paragraphs, it is two steps.
- **Paste only what you have read in full.** The snippet is a copy, so getting it wrong
  is invisible until someone opens the file. `cw check` is what catches that.
- **Say what changed, not what exists.** For a pull request, a snippet of untouched code
  needs a `note` that says so.
- **Every step earns its blocks.** A diagram that restates the prose costs the reader
  more than it saves. The validator warns about a step with nothing but prose; it cannot
  warn about a diagram nobody needed.
- All prose in English, including for a Dutch reader. Domain nouns from the codebase stay
  as they are in the code.
- Run every sentence through **unslop** before it goes into the file.

## Steps

1. **Scope it.** Name the change in one line. Then name the parts, before writing any of
   them: three to five, each one thing a reviewer could agree or disagree with on its own.
   For a pull request the bug it fixes, the arrangement before it and the arrangement
   after it are usually three of them.

2. **Read the change.** `gh pr diff <n>` and `gh pr view <n> --json files` for a pull
   request. For a subsystem, invoke `how`, or trace it with `Grep` and `Read`. Write down
   per hop: the file, the symbol, what it decides, what it hands on.

3. **Lay out parts, sections and steps.** A part holds two to four sections; a section
   holds two to five steps. If a section has one step, it is a step. If it has nine, it
   is two sections.

4. **Write the file.** Read `DATA.md` for the fields, then write one JSON file with
   `"$schema"` pointing at the schema so your editor validates while you type.

5. **Check it until it is clean.**
   ```powershell
   & "$env:USERPROFILE\.claude\skills\code-walkthrough\cw.ps1" check walkthrough.json --root C:\Projects\Fincent
   ```
   Errors are the walkthrough being wrong, not the tool. `STALE` means a snippet is not
   in the tree the way you pasted it, which on a pull request usually means you are on
   the wrong branch: `gh pr checkout <n>` first.

6. **Look at it.**
   ```powershell
   & "$env:USERPROFILE\.claude\skills\code-walkthrough\cw.ps1" serve walkthrough.json --root C:\Projects\Fincent
   ```
   It opens a browser on 127.0.0.1. If Playwright is available, step through two or three
   steps and screenshot them: the console clean, every mermaid diagram rendered, no step
   scrolling past two screens.

## Running it

`cw.ps1` builds the server on first use and whenever the source changed, then runs it.
`cw.sh` does the same outside PowerShell. Go on PATH is the only requirement; the server
has no module dependencies.

```
cw serve <walkthrough.json> [--root DIR] [--port N] [--no-open] [--offline]
cw check <walkthrough.json> [--root DIR]   validate and verify, exit 1 on an error
cw schema [--write]                        print the schema, or write a copy to point at
cw ides                                    list the editors found on this machine
cw settings [--path]                       print the settings file
cw cache clear                             drop the cached mermaid and typefaces
```

Without `--root` the root is `root` from the walkthrough, else the git root the
walkthrough sits in. Without any root the page still reads: the open buttons say so and
nothing is checked against a tree.

## What the reader gets

An overview of the parts, a card each. Inside a part, its sections. Inside a section, the
steps, with a dot per step. The arrow keys work on every screen and run straight through
the lot, from the overview into part one and on across the part boundaries, and past the
last step they come back to the overview. Progress is kept per walkthrough in their own
browser, so the page remembers where they were. Every snippet, every diagram block and
every line number opens that line in their own editor, detected on first run. `t` for
dark, `s` for settings, `o` to open the step's file, Escape to go up a level.

Mermaid, the highlighter and the two typefaces are fetched once through the server and
cached on disk, so the page keeps working offline. Without them a diagram falls back to
its own source and code renders unhighlighted, which is still the thing being described.

Snippets are highlighted by language, worked out from `lang` or the file extension. An
extension the bundle has no grammar for stays plain rather than being guessed at.

## Reply

The URL, one line per part on what it covers, and which files the walkthrough touches.
Say what you could not verify, and name any snippet `cw check` reported as moved or gone.
