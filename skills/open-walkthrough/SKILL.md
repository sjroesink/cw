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
| `this was written against … and the checkout is on …` | Right repository, wrong place in history. What follows says what it did about that. See below |
| `pull request … is on branch …` | What `gh` said. `which is on … now` means the branch has moved since, so checking the branch out is not the same as reading this |
| `added the worktree at …` | It made one and is reading there. It removes it again when the server stops |
| `is protected. Give the password with --password` | Pass `--password`, or set `$env:CW_PASSWORD`. Ask them for it; never guess |
| `does not allow this machine to read it` | An address restriction. Only whoever published it can change that |

**4. The commit matters more than it looks.** A walkthrough is written against one commit. Read it
against a different one and most snippets report as moved or gone, which reads as the walkthrough
being broken rather than the checkout being elsewhere in history.

`cw open` works that out on its own. It compares the checkout against the commit the walkthrough
records, asks `gh` which branch the pull request is on when the document does not say, and looks
through `git worktree list` for one already on that commit or that branch. Finding one, it reads
there and says so, and you do nothing.

When there is none, it offers to add one. **Pass `--worktree` or `--no-worktree` rather than
neither**, because the question is put to a terminal and you are not one:

```powershell
& "$env:USERPROFILE\.claude\skills\code-walkthrough\cw.ps1" open https://cw.roesink.dev/w/fincent-pr-4164 --worktree
```

Ask the person first, and say what it costs: a second directory, fetched if the commit is not there
yet, removed again when they stop the server. It is theirs to say no to, and `--no-worktree` prints
the `git worktree add` line for them to run by hand instead.

Do not reach for `gh pr checkout`. `cw open` prints it as a last line, but it moves the branch under
whatever the person is working on, and they may have uncommitted changes. A worktree costs them
nothing.

**5. Leave nothing behind.** The worktree goes when the server is stopped with ctrl-c. A kill, a
crash or a closed laptop skips that, and then `cw worktrees` lists what is still there and
`cw worktrees clean` removes it. One with changes in it is kept, on purpose: somebody started
working in there. Say that rather than reaching for `--force`, which this deliberately does not do.

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
