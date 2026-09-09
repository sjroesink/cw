---
name: open-walkthrough
description: "Open a published code walkthrough on this machine, so the snippets are checked against your own checkout and every line number opens in your editor. Use when someone gives you a cw.roesink.dev link and wants it read locally: 'open https://cw.roesink.dev/w/fincent-pr-3347 lokaal', 'open deze walkthrough lokaal', 'lees dit lokaal in', 'open this walkthrough locally', '/open-walkthrough <url>'. Also for a walkthrough.json file on disk."
---

# open-walkthrough

Someone has a link to a walkthrough and wants the version that knows about their machine.

The hosted page cannot do two things, because it has neither: open a file in the reader's editor,
and check the pasted snippets against their working tree. Both come back when the walkthrough is
served locally instead. Same page, same content, pulled down to where the code already is.

## What you do

**1. Is `cw` there?**

```powershell
& "$env:USERPROFILE\.claude\skills\code-walkthrough\cw.ps1" --help
```

That wrapper builds from a checkout if there is one and installs the published module otherwise, so
in practice it either works or tells you Go is missing. If Go is missing too, say so and give them
the plain link: the hosted page reads fine, it just has no buttons. Do not try to install Go.

**2. Open it.**

```powershell
& "$env:USERPROFILE\.claude\skills\code-walkthrough\cw.ps1" open https://cw.roesink.dev/w/fincent-pr-3347
```

A bare name works too (`cw open fincent-pr-3347`), and so does a local file, which goes through
`serve` rather than `open`:

```powershell
& "$env:USERPROFILE\.claude\skills\code-walkthrough\cw.ps1" serve .\walkthrough.json --root C:\Projects\Fincent
```

It prints a `http://127.0.0.1:<port>/` and opens a browser. Give the person that URL.

**3. Read what it says back**, because that is where this goes wrong.

| What it printed | What it means, and what to do |
|---|---|
| `found the checkout at …` | It matched `source.repo` against the origin remote of a directory it found. Nothing to do |
| `no checkout found` | It could not find the repository on this machine. Ask where it is and pass `--root <path>` |
| `a worktree at … is on …, so that is what will be read` | It found a checkout already sitting on the right commit and used that one. Nothing to do |
| `this was written against … and the checkout is on …` | Right repository, wrong place in history, and no worktree for it. See below |
| `is protected. Give the password with --password` | Pass `--password`, or set `$env:CW_PASSWORD`. Ask them for it; never guess |
| `does not allow this machine to read it` | An address restriction. Only whoever published it can change that |

**4. The commit matters more than it looks.** A walkthrough is written against one commit. Read it
against a different one and most snippets report as moved or gone, which reads as the walkthrough
being broken rather than the checkout being elsewhere in history.

`cw open` handles the good case on its own: it looks through `git worktree list` and, if one of them
is already on that commit or on the branch, reads there instead and says so. You do nothing.

When there is none it prints the command for a new one, which is the part to act on:

```powershell
git worktree add --detach C:\Users\you\.claude-worktrees\Fincent\pr-4164 144da9a
```

Ask first, then run exactly what it printed, then run `cw open` again. The second run finds the
worktree by itself, so no `--root` is needed. The path it suggests follows wherever that repository
already keeps its worktrees, and `--detach` puts it on the commit the walkthrough was written
against rather than on a branch that has moved on since.

Do not reach for `gh pr checkout`. `cw open` prints it as a last line, but it moves the branch under
whatever the person is working on, and they may have uncommitted changes. A worktree costs them
nothing. Only offer it if they say they would rather not have another directory.

If the commit is not in the object store yet, `cw open` says so and gives the fetch to run first.

## What they get that the link does not give them

- **Every snippet checked against their tree**, on every load. A chip says moved or gone per
  snippet, and the banner counts them. That is the difference between reading a walkthrough and
  trusting it.
- **Line numbers that open their editor** on that line, in whichever one they use.
- The rest is identical: the same parts, sections and steps, the same diagrams, and progress
  remembered per walkthrough in their browser.

## Do not

- Do not copy the walkthrough JSON into the repository. `cw open` puts it somewhere temporary on
  purpose; it is a copy of something that lives on the site.
- Do not pass a password on the command line when you can put it in `$env:CW_PASSWORD`. Command
  lines end up in shell history.
- Do not publish anything. This skill reads. `code-walkthrough` is the one that writes.

## Reply

The local URL, which repository it resolved to, and whether the snippets came back clean. If any
came back moved or gone, say how many and that they are probably on a different commit rather than
that the walkthrough is wrong.
