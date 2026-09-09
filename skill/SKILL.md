---
name: code-walkthrough
description: "Turn a pull request or a subsystem into a page someone can read at their own pace: parts on an overview, sections inside a part, and steps that carry the prose, a mermaid diagram, the code, a diff or a small animation. Progress is remembered, every snippet has an open in IDE button, and the whole content is one JSON file written against a schema. Publish it to share the link. Use for 'maak een code walkthrough van deze PR', 'leg dit PR uit voor het team', 'walk me through this change', '/code-walkthrough X'."
---

# code-walkthrough

You turn a change into a page a reviewer can work through: an overview of what the
change is made of, and inside each part the sections and steps that explain it, with
the code, a diagram, a diff or an animation next to the prose.

**The tool and the page already exist.** `cw` is a dependency-free Go binary that serves
the page, opens files in the reader's editor, checks the pasted snippets against the
working tree, and publishes to a site where the rest of the team can read it. Your job
per topic is a single walkthrough file, written against the schema.

Read **`RULES.md`** before you write anything: it is what makes a walkthrough worth an
afternoon. Read **`DATA.md`** for the fields. `FORMAT.md` is only for someone building a
second reader.

The design came from a Claude Design project and is reproduced as it was drawn: Sora and
JetBrains Mono, the oklch palette, the three views, the progress bar. Change the data,
not the page.

## Which of the two skills

`visually-explain-code` and this one both explain code, and they are not the same tool.

| | `visually-explain-code` | `code-walkthrough` |
|---|---|---|
| the code | anchors, read live from the tree, never pasted | pasted into the file, checked against the tree on every load |
| the picture | one scene that lights up and animates per step | a mermaid diagram per step, with clickable blocks |
| the shape | one run of steps, or parts | parts, sections, steps, with a progress bar |
| the reader | watches it move, in one sitting | works through it, comes back tomorrow |
| sharing | a local server | a link, published to cw.roesink.dev |

Pick this one for a pull request several people will read, for onboarding material, or
whenever a diff belongs in the story. Pick the other when the code has to be guaranteed
current, or when the point is a single picture the reader watches move.

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

4. **Write the file.** `RULES.md` for how, `DATA.md` for the fields. Point `$schema` at
   `https://cw.roesink.dev/schema/v1.json` so your editor validates while you type. Set
   `source.url` to the pull request: `cw publish` reads the commit and the changed files
   out of it, and those are what make every snippet link back to the diff.

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

7. **Publish it**, unless it is only for you.
   ```powershell
   & "$env:USERPROFILE\.claude\skills\code-walkthrough\cw.ps1" publish walkthrough.json --root C:\Projects\Fincent
   ```
   It verifies against the tree once more, fills in the ids and the snippet anchors, and
   prints the URL. Publishing the same file again updates that same URL, so the link you
   sent round keeps working. `--new` is how you deliberately make a second one.

## Running it

`cw.ps1` builds the binary on first use and whenever the source changed, then runs it.
`cw.sh` does the same outside PowerShell. Go on PATH is the only requirement.

```
cw serve <walkthrough.json> [--root DIR] [--port N] [--no-open] [--offline]
cw check <walkthrough.json> [--root DIR]   validate and verify, exit 1 on an error
cw publish <walkthrough.json> [--slug NAME] [--new] [--force]
cw open <url or name> [--root DIR]         read a published one with the local buttons
cw migrate <walkthrough.json>              lift an older file to cw/1
cw schema [--write]                        print the schema, or write a copy to point at
cw ides                                    list the editors found on this machine
cw settings [--path]                       print the settings file
cw cache warm | clear                      fetch the mermaid and typeface bundle, or drop it
```

Publishing needs a key in `%USERPROFILE%\.claude\secrets\cw-api-key`, or `$env:CW_API_KEY`.
Never print it, never pass it as an argument, never put it in a commit or a summary.

## Where it goes when you publish

`https://cw.roesink.dev/w/<name>`. The hosted page cannot open a file in the reader's
editor and has no working tree to check against, so a snippet links into the pull request
diff instead, and the page says which commit the code was true for. Anyone who has the
repository checked out can get the full local page back with `cw open <url> --root .`.

The site serves its own instructions at `/skill.md`, assembled from `RULES.md`, `DATA.md`
and the API reference. That is what an agent reads when someone says "make a code
walkthrough of this PR on cw.roesink.dev" and has none of this installed.

## What the reader gets

An overview of the parts, a card each. Inside a part, its sections. Inside a section, the
steps, with a dot per step. The arrow keys work on every screen and run straight through
the lot, from the overview into part one and on across the part boundaries, and past the
last step they come back to the overview. Progress is kept per walkthrough in their own
browser, keyed on the ids in the file, so inserting a step later does not move where
anyone was. `t` for dark, `s` for settings, `o` to open the step's file, Escape to go up
a level.

Mermaid, the highlighter and the two typefaces are fetched once and cached on disk, so
the page keeps working offline. Without them a diagram falls back to its own source and
code renders unhighlighted, which is still the thing being described.

## Reply

The URL, one line per part on what it covers, and which files the walkthrough touches.
Say what you could not verify, and name any snippet `cw check` reported as moved or gone.
