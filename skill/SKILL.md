---
name: code-walkthrough
description: "Turn a pull request or a subsystem into a page someone can work through at their own pace: parts on an overview, sections inside a part, and steps that carry the prose next to a mermaid diagram, the code, a diff or a small animation. Progress is remembered, every snippet links back to the line it came from, and the whole content is one JSON file written against a schema. Publish it and hand over the link. Use for 'maak een code walkthrough van deze PR', 'leg dit PR uit voor het team', 'walk me through this change', '/code-walkthrough X'."
---

# code-walkthrough

You turn a change into a page a reviewer can work through: an overview of what the change
is made of, and inside each part the sections and steps that explain it, with the code, a
diagram, a diff or an animation next to the prose.

**Your job is one file.** The rest exists: a format called `cw/1`, a reader that runs on
your machine and one that runs as a site, and an API to publish to. None of them knows any
topic. Adding a walkthrough is writing a document and nothing else.

Read these in this order, and do not write anything before the first one:

| | |
|---|---|
| `RULES.md` | what makes a walkthrough worth someone's afternoon. Read it first, every time |
| `DATA.md` | every field, and what good content looks like in it |
| `FORMAT.md` | only if you are building something else that reads walkthroughs |

The design came from a Claude Design project and is reproduced as it was drawn: Sora and
JetBrains Mono, the oklch palette, the three views, the progress bar. Change the data, not
the page.

## When it fits

A pull request several people will read. Onboarding material for a subsystem. Anything
where a diff belongs in the story, or where the reader will come back to it tomorrow
rather than finish it in one sitting.

It does not fit a change small enough to explain in a review comment, and it does not fit
a question. Both of those are answered faster by answering them.

## Steps

1. **Scope it.** Name the change in one line. Then name the parts, before writing any of
   them: three to five, each one thing a reviewer could agree or disagree with on its own.
   For a pull request the bug it fixes, the arrangement before it and the arrangement
   after it are usually three of them.

2. **Read the change.** `gh pr diff <n>` and `gh pr view <n> --json files` for a pull
   request. For a subsystem, invoke `how`, or trace it with `Grep` and `Read`. Write down
   per hop: the file, the symbol, what it decides, what it hands on. Open the files
   themselves; a walkthrough written off a diff summary reads like one.

3. **Lay out parts, sections and steps.** A part holds two to four sections; a section
   holds two to five steps. If a section has one step, it is a step. If it has nine, it is
   two sections.

4. **Write the file.** Point `$schema` at `https://cw.roesink.dev/schema/v1.json` so your
   editor validates while you type, and set `version` to `cw/1`. Fill in `source` with the
   repository and the pull request URL: `cw publish` reads the commit and the changed
   files out of it, and those are what make every snippet link back to the diff. Leave
   `id`, `to` and `sha` out; they are filled in for you.

5. **Check it until it is clean.**
   ```powershell
   & "$env:USERPROFILE\.claude\skills\code-walkthrough\cw.ps1" check walkthrough.json --root C:\Projects\Fincent
   ```
   Errors are the walkthrough being wrong, not the tool. `STALE` means a snippet is not in
   the tree the way you pasted it, which on a pull request usually means you are on the
   wrong branch: `gh pr checkout <n>` first. Warnings do not block anything and are still
   worth fixing: they are most of the difference between a walkthrough that validates and
   one somebody wants to read.

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

`cw.ps1` builds the binary from a checkout when there is one and installs the published
module otherwise, then runs it. `cw.sh` does the same outside PowerShell. Go on PATH is
the only requirement; the binary has no dependencies of its own.

```
cw serve <walkthrough.json> [--root DIR] [--port N] [--no-open] [--offline]
cw check <walkthrough.json> [--root DIR]   validate and verify, exit 1 on an error
cw publish <walkthrough.json> [--slug NAME] [--new] [--force]
cw open <url or name> [--root DIR]         read a published one with the local buttons
cw migrate <walkthrough.json>              lift an older file to cw/1
cw schema [--write]                        print the JSON schema, or write a copy to point at
cw ides                                    list the editors found on this machine
cw settings [--path]                       print the settings file
cw cache warm | clear                      fetch the mermaid and typeface bundle, or drop it
```

Publishing needs no key. What comes back is a key for that one walkthrough, and it is
the only thing that can change it afterwards. `cw publish` stores it in
`%USERPROFILE%\.claude\secrets\cw-keys.json` and uses it the next time you publish the
same file, so you never have to handle it. Never print it, never pass it as an argument,
never put it in a commit or a summary, and never write it beside the walkthrough: that
file lives in a repository.

Someone else who needs to change your walkthrough needs that key from you. Whoever runs
the site has an admin key that works on everything, which is the way back if it is lost.

## The two readers, and what each can do

Locally the server has your working tree and your editor. So it looks for every pasted
snippet in the file it names, on every load, and says whether it is still there, moved, or
gone. A line number opens that line in your editor.

The published page has neither. A snippet links into the pull request diff instead, or to
a permalink on the commit, and the page says which commit the code was true for rather
than pretending to know about today. Anyone who has the repository checked out gets the
full local page back with `cw open <url> --root .`.

That difference is why the tree check happens at publishing time. A stale snippet stops
the publish, and `--force` puts it on the page in as many words instead of hiding it.

## What the site tells an agent that has none of this

`https://cw.roesink.dev` serves its own instructions at `/skill.md`, assembled from
`RULES.md`, `DATA.md` and the API reference. So "make a code walkthrough of this PR on
cw.roesink.dev" is a complete instruction for an agent with nothing installed, and this
skill is the faster path for one that has the code in front of it.

## What the reader gets

An overview of the parts, a card each. Inside a part, its sections. Inside a section, the
steps, with a dot per step. The arrow keys work on every screen and run straight through
the lot, from the overview into part one and on across the part boundaries, and past the
last step they come back to the overview. Progress is kept per walkthrough in their own
browser, keyed on the ids in the file, so inserting a step later does not move where
anyone was. `t` for dark, `s` for settings, `o` to open the step's file, Escape to go up a
level.

Mermaid, the highlighter and the two typefaces are fetched once and cached on disk, so the
page keeps working offline. Without them a diagram falls back to its own source and code
renders unhighlighted, which is still the thing being described.

## Reply

The URL, one line per part on what it covers, and which files the walkthrough touches. Say
what you could not verify, and name any snippet `cw check` reported as moved or gone.
