---
name: migrate-walkthrough
description: "Lift a code walkthrough from cw/1 to cw/2, the version where a step is a list of blocks. The tool does the mechanical half and prints a numbered list of what it refused to guess; this finishes that list. Use for 'migreer deze walkthrough naar cw/2', 'zet deze walkthrough om', 'update this walkthrough to the new format', '/migrate-walkthrough <file or url>'."
---

# migrate-walkthrough

A walkthrough written as `cw/1` still opens, still publishes and still reads. Migrating one is
worth doing when it is about to be edited anyway, or when it wants something `cw/1` could not say:
two snippets with a paragraph between them, a diff with real coordinates, a diagram that links to
code somewhere else in the document.

`cw migrate --to cw/2` does everything that follows from the old document. What it will not do is
guess, and the numbered list it prints is the job.

## What you do

**1. Get a copy, and work on the copy.**

A local file is already one, so copy it beside itself before touching it. A published walkthrough
comes down with `cw open`, which puts it in a temporary directory and prints where:

```powershell
& "$env:USERPROFILE\.claude\skills\code-walkthrough\cw.ps1" open https://cw.roesink.dev/w/fincent-pr-3347
```

Copy that file into the working directory under a name that says what it is. Never migrate over the
only copy: until `cw check` is clean the new one is not a walkthrough yet.

**2. Run the lift.**

```powershell
& "$env:USERPROFILE\.claude\skills\code-walkthrough\cw.ps1" migrate --to cw/2 .\walkthrough.json
```

It prints what it filled in, then a numbered list of what it could not work out. Read the list. It
is the whole assignment, and nothing else in the file needs your attention.

**3. Work the list.** Each kind of entry has one right answer.

| What it says | What to do |
|---|---|
| `needs an alt` | Read the mermaid source and write one sentence saying **what the picture shows**, not what it looks like. It is the diagram for somebody who cannot see it. "A request passes through verifySignature before it reaches orderHandler", not "a flowchart with four boxes" |
| `kept as a snippet rather than a diff` | Get the real coordinates: `git diff <base>...<head> -- <file>` or `gh pr diff <n>`. Write the hunks out with both starting lines and both counts. If the commits are gone or the file is not in the diff any more, leave it a snippet and say so |
| `snippet check(s) were dropped` | Nothing. `cw publish` against a real checkout writes them back, with the date and the revision that `cw/1` never recorded |
| `base and head are branch names` | `gh pr view <n> --json baseRefOid,headRefOid` and put them in `source.comparison` |
| `changedFiles were dropped` | `gh pr view <n> --json files` for the list, `git diff --name-status --find-renames <base>...<head>` for what happened to each. Without them a snippet links to the commit rather than into the diff, which still works and is less useful |
| `had a note, which became a paragraph` | Read it. A sentence about the code belongs in the paragraph where the lift put it; a two-word label belongs in `snippet.label` and the paragraph goes |
| `is in state "..."` | Map it onto `idle`, `active`, `done`, `gone` or `alert`. Only you know which one was meant |
| `the id "..." was already taken` | Nothing, unless the walkthrough is already published: then a renamed step id loses that step from everyone's saved progress. Say so in the reply |
| `root is not a field any more` | Nothing. Pass `--root` when you serve it |

**4. Check it until it is clean.**

```powershell
& "$env:USERPROFILE\.claude\skills\code-walkthrough\cw.ps1" check .\walkthrough.json --root C:\Projects\Fincent
```

Errors refuse the document. Warnings are about whether it is worth reading and a migrated
walkthrough inherits whatever it had. The snippet count at the bottom should be the same as before
the lift: if snippets that were fine are now moved or gone, a line number went wrong somewhere.

**5. Read it before you publish it.** `cw serve` it and walk the steps that had a diagram, a diff or
an animation, because those are the three the lift changed the most. Then publish with the slug it
already had, so the URL and every link to it survive:

```powershell
& "$env:USERPROFILE\.claude\skills\code-walkthrough\cw.ps1" publish .\walkthrough.json --slug fincent-pr-3347
```

## Do not

- **Do not recompute line numbers by hand.** The lift already made them relative to their snippet,
  and doing it twice is how they end up wrong. If a highlight looks off, the snippet text is the
  thing to check, not the number.
- **Do not rewrite the prose** because the shape changed. The content did not change, the structure
  did. A sentence that read well as a `body` reads the same as a markdown block.
- **Do not invent what the tool refused to invent.** If the commits behind a diff are gone, the
  honest outcome is a snippet with a note, not a hunk with coordinates you made up.
- **Do not migrate what nobody asked about.** One walkthrough at a time, and only when there is a
  reason.

## Reply

Which walkthrough, how many of the numbered points there were, which of them you resolved and how,
and which you left standing and why. If any step id was renamed and the walkthrough was already
published, say that saved progress for those steps is lost.
