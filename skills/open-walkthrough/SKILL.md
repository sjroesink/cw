---
name: open-walkthrough
description: "Open a published code walkthrough on this machine, so the snippets are checked against your own checkout and every line number opens in your editor. Use when someone gives you a cw.roesink.dev link and wants it read locally: 'open https://cw.roesink.dev/w/fincent-pr-3347 lokaal', 'open deze walkthrough lokaal', 'lees dit lokaal in', 'open this walkthrough locally', '/open-walkthrough <url>'. Also for a walkthrough.json file on disk."
---

# open-walkthrough

Someone has a link to a walkthrough and wants the version that knows about their machine.

The hosted page cannot do three things. Two because it has neither a working tree nor an editor:
open a file where the reader keeps it, and check the pasted snippets against it. The third
because there is nobody behind it: answer a question about what is on the screen. All three come
back when the walkthrough is served locally instead. Same page, same content, pulled down to
where the code already is, with you in the terminal next to it.

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
| `it is on … though, so a snippet may still have moved` | The worktree is on the right branch but behind. What follows is the one step that fixes it |
| `that worktree is on … now` | It fast-forwarded the branch that was already checked out there. That one stays put after the server stops |
| `there is work in it, so it stays where it is` | Somebody is using that worktree. Read it as it is, or ask them |
| `comments  cw comments watch <name>` | The command that hands you what they ask while they read. See step 5 |
| `is protected. Give the password with --password` | Pass `--password`, or set `$env:CW_PASSWORD`. Ask them for it; never guess |
| `does not allow this machine to read it` | An address restriction. Only whoever published it can change that |

**4. The commit matters more than it looks.** A walkthrough is written against one commit. Read it
against a different one and most snippets report as moved or gone, which reads as the walkthrough
being broken rather than the checkout being elsewhere in history.

`cw open` works that out on its own. It compares the checkout against the commit the walkthrough
records, asks `gh` which branch the pull request is on when the document does not say, and looks
through `git worktree list` for one already on that commit or that branch. Finding one, it reads
there and says so, and you do nothing.

Finding one that is on the branch but behind the commit, which is the ordinary state of a worktree
made a while ago, it offers to fast-forward it instead of adding a second one. That is a change to a
directory the person made themselves, so it happens only from a clean tree and only forwards, and it
stays after the server stops.

When there is neither, it offers to add one. **Pass `--worktree` or `--no-worktree` rather than
neither**, because the question is put to a terminal and you are not one:

```powershell
& "$env:USERPROFILE\.claude\skills\code-walkthrough\cw.ps1" open https://cw.roesink.dev/w/fincent-pr-4164 --worktree
```

Ask the person first, and say what it costs: a second directory, fetched if the commit is not there
yet, removed again when they stop the server. A fast-forward costs them less and lasts longer, so
say which of the two is on the table. Both are theirs to say no to, and `--no-worktree` prints the
`git worktree add` or `git merge --ff-only` line for them to run by hand instead.

Do not reach for `gh pr checkout`. `cw open` prints it as a last line, but it moves the branch under
whatever the person is working on, and they may have uncommitted changes. A worktree costs them
nothing.

**5. Stay for the questions.** The page can be asked things: they select a few lines or half a
sentence, leave a comment, and it lands in a column beside the text. Nothing answers those unless a
session is listening, and the session that opened it is the one that should be. So start the watch,
in the background, and leave it running:

```powershell
& "$env:USERPROFILE\.claude\skills\code-walkthrough\cw.ps1" comments watch --for 10m --json
```

It waits until there is a comment, takes it so the page can show that somebody picked it up, prints
it and stops. What it prints is the page, the file and the lines they selected, the words
themselves, and what they asked. That is enough to go and read the code before answering, which is
the point of being the session with the checkout.

Write the answer to a file and send the file. A markdown answer with a fenced block in it does not
survive a PowerShell command line:

```powershell
& "$env:USERPROFILE\.claude\skills\code-walkthrough\cw.ps1" comments reply 3 --file .\answer.md
```

Then start the watch again, and keep doing that until they say to stop or the server does. A turn
that comes back with nothing has lost nothing; `--for` only decides how long one turn waits.

The answer is markdown, read in a narrow column, beside the thing they were looking at. Answer what
they asked and stop. A fenced block when the code is the answer, no headings, and no recap of what
they are already reading.

**Or answer in blocks.** A `.json` file holding an array of cw/2 blocks is drawn by the same
renderer the walkthrough is drawn with, so a code block gets the file's own line numbers and the
button that opens them, and a diagram gets drawn. Worth it when the answer is code somewhere else
in the tree, or a shape:

```json
[{"type": "markdown", "text": "It is checked here, before anything is read."},
 {"type": "code", "snippet": {"language": "go", "text": "...",
   "source": {"file": "main.go", "startLine": 808, "endLine": 820}}}]
```

`DATA.md` in the `code-walkthrough` skill is the field reference for what goes in one. The blocks
are checked against the schema and refused with the field that is wrong, so a mistake comes back
as a sentence rather than as a broken card. Prose is still the right answer to most questions.

If nobody is listening, the page says so and offers a prompt to copy, so somebody reading on their
own can start a session for it. Not being there is a state it can show, which is why the watch is
worth starting even when the questions come later.

**6. Leave nothing behind.** The worktree goes when the server is stopped with ctrl-c. A kill, a
crash or a closed laptop skips that, and then `cw worktrees` lists what is still there and
`cw worktrees clean` removes it. One with changes in it is kept, on purpose: somebody started
working in there. Say that rather than reaching for `--force`, which this deliberately does not do.

## What they get that the link does not give them

- **Every snippet checked against their tree**, on every load. A chip says moved or gone per
  snippet, and the banner counts them. That is the difference between reading a walkthrough and
  trusting it.
- **Line numbers that open their editor** on that line, in whichever one they use.
- **Somewhere to ask.** A comment on the words they are looking at, answered by whoever is
  watching, with the answer beside those words rather than in a chat somewhere else.
- The rest is identical: the same parts, sections and steps, the same diagrams, and progress
  remembered per walkthrough in their browser.

## Do not

- Do not copy the walkthrough JSON into the repository. `cw open` puts it somewhere temporary on
  purpose; it is a copy of something that lives on the site.
- Do not pass a password on the command line when you can put it in `$env:CW_PASSWORD`. Command
  lines end up in shell history.
- Do not publish anything. This skill reads. `code-walkthrough` is the one that writes.
- Do not answer a comment that says the walkthrough itself is wrong by explaining it away. That
  comment is a bug report against the document, and fixing it is `code-walkthrough`'s job. Say
  what you will change, and then change it there.
- Do not copy what somebody asked into a commit message, a PR or a ticket. It was a question
  asked while reading, not something they published.
- Do not tidy up the comments. Archiving one is the reader's move, not yours: the answer under it
  is often the most useful thing on that step.

## Reply

The local URL, which repository it resolved to, and whether the snippets came back clean. If any
came back moved or gone, say how many and that they are probably on a different commit rather than
that the walkthrough is wrong.

Say that the watch is running and that they can select anything on the page and ask about it. As
questions come in, answer them on the page and keep the session's own reply to one line: which
comment, and what it was about.
