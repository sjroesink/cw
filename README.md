# cw

Turn a pull request or a subsystem into a page someone can work through at their own pace: an
overview of the parts, sections inside a part, and steps that carry the prose next to the code, a
mermaid diagram, a diff or a small animation. Progress is remembered, every snippet can be opened in
the reader's own editor, and the whole content is one JSON file.

The page holds no topic of its own. A new walkthrough is a file and nothing else.

## Running it

Go on PATH is the only requirement. The binary has no module dependencies.

```
go install github.com/sjroesink/cw@latest
```

```
cw serve <walkthrough.json> [--root DIR] [--port N] [--no-open] [--offline]
cw check <walkthrough.json> [--root DIR]   validate and verify, exit 1 on an error
cw publish <walkthrough.json> [--slug NAME] [--new] [--force]
cw open <url or name> [--root DIR]         read a published one with the local buttons
        [--worktree | --no-worktree]       add or move a worktree for its commit without asking, or never
cw worktrees [clean]                       the worktrees cw open added, and removing them
cw comments [<file, url or name>]          what a reader asked, while one is being served
cw comments watch [target] [--for 10m]     wait for one to answer, and take it
cw comments reply <id> [--file F]          answer it, or --text T, or - for stdin
cw migrate <walkthrough.json> [--to cw/2]  fill in ids and anchors, or lift a cw/1 file
cw schema [--write]                        print the JSON schema, or write a copy to point at
cw ides                                    list the editors found on this machine
cw settings [--path]                       print the settings file
cw cache warm | clear                      fetch the mermaid and typeface bundle, or drop it
```

Running the site rather than one file:

```
cw host [--addr :8080] [--data DIR] [--base-url URL]
cw keys add <name> | list | rm <name>      admin keys, which work on every walkthrough
```

Publishing is open: anyone who can reach the site can put a walkthrough on it. What comes back is a
key for that one walkthrough, and it is the only thing that can change it afterwards. Only its hash
is stored, so a lost key means asking whoever runs the site, whose admin key works on everything.

Whether a first publish locks it is not left to whoever runs the command. `cw publish` asks `gh`
what the repository the walkthrough names is: public goes out open, and everything else goes out
with a password it makes itself, printed once and stored beside the edit key. Not being able to ask
counts as not public. `--password` and `--no-password` overrule that, and a later publish leaves the
lock exactly as it was.

A walkthrough can be locked with a password, with a list of addresses, or with both, and both are
needed when both are set. A locked one is not listed to anyone who has not opened it. Passwords are
stored as salted PBKDF2, and an unlock is a cookie signed over that one name, so it does not open
anything else.

Behind a proxy, the address a rule is checked against is only as trustworthy as the hop that
reported it. `CW_TRUSTED_PROXIES` is the list of hops that may be believed, as addresses or CIDRs;
without it the private ranges and Cloudflare's published edges are trusted, and anything else is
taken at its socket address. `GET /api/v1/whoami` says what the site makes of a given caller, which
is how to check a rule before it locks somebody out.

`serve` opens a browser on 127.0.0.1. Without `--root` the root is `root` from the walkthrough, else
the git root the file sits in. Without any root the page still reads: the open buttons say so and
nothing is checked against a tree.

`open` pulls a published walkthrough down and serves it the same way, so the buttons and the snippet
check come back. It looks for the checkout the walkthrough is about, and when that checkout sits
somewhere else in history it asks `gh` which branch the pull request is on and looks whether that
commit or that branch is already in a worktree. When neither is, it offers to add one, reads the
walkthrough there, and gives it back when the server stops.

A worktree on the right branch is usually behind the commit rather than on it, because the branch
moved on and it did not. Then there is nothing to add: it offers to fast-forward the branch that is
already checked out there, which it does only from a clean tree and only onto a commit that is ahead
of where it is. That one is not given back at the end, because a branch that caught up is not damage
to undo.

`--worktree` and `--no-worktree` answer either question up front, which is what a script wants. A
worktree with work in it is never removed and never moved, and `cw worktrees` is what finds the ones
a killed run left behind.

## Asking about what is on the screen

A walkthrough being served locally can be asked questions. Select a few lines of code or half a
sentence of prose, leave a comment, and it appears in a column beside the text with the words it
is about still marked in place.

`cw comments watch` in the terminal next to it waits until there is one, takes it, and prints
where it hangs: the step, the file and the lines. The page says that somebody picked it up while
it is being written, and shows the answer when `cw comments reply` sends it back. When nothing is
listening the page says that too, and offers the prompt to paste into an agent.

An answer is markdown, or the same seven blocks a step is written in. Send a JSON array of blocks
instead of prose and the page draws them with the renderer it draws the walkthrough with: line
numbers that open the editor, a mermaid diagram, a callout. They are held to the block definition
in the schema and to nothing else, because a reply is not a published document and the rules
about hashes and unique ids are about one.

Nothing deletes a comment. The button archives it, which takes it out of the column and out of
what a watcher is handed, and a checkbox puts the archive back on the screen.

The threads are kept beside the settings, filed under the name the walkthrough was published as,
so they are still there the next time it is opened. They never go into the walkthrough, and the
site has none of this: there is nobody in a terminal there to answer.

## The format

One JSON file per topic, written against `schema/walkthrough.v2.schema.json`. Point at it from the
file and an editor validates while you type:

```jsonc
{
  "$schema": "https://cw.roesink.dev/schema/v2.json",
  "version": "cw/2",
  "title": "feat: reliable webhook processing",
  "source": {
    "kind": "pull-request",
    "provider": "github",
    "repositoryUrl": "https://github.com/innovadis-dev/Fincent",
    "label": "PR #4150",
    "url": "https://github.com/innovadis-dev/Fincent/pull/4150",
    "revision": "9f2c1ab…"
  },
  "summary": "The paragraph on the overview, above the cards.",
  "parts": [ /* parts hold sections, sections hold steps, steps hold blocks */ ]
}
```

A step is a list of blocks in the order they are read: prose, code, a callout, a diagram, a diff, a
timeline, or an extension carrying its own fallback. `skills/code-walkthrough/DATA.md` is the field
reference.

The format is versioned so that something other than this page can read the same file, an editor
plugin or a reader that plays it out loud. The rule for any consumer is to render what it
understands and skip the rest without complaining, and to refuse a version it does not know rather
than guess at which half of it still means what it used to. `cw/1` is still read, still published
and documented in `spec/FORMAT-v1.md`.

## Verifying, and what that is worth

A snippet in the file is a copy, and copies rot. On every load the server looks for those exact
lines in that file: found where you said, nothing is shown; found somewhere else, the chip says
moved; not there at all, the chip says so and the banner counts it. Moving code is not changing it,
which is why the two are reported apart.

`cw check` is the same pass without the page, and it exits 1 on an error, so it belongs in the loop
while you write.

## What the server will not do

- Read or open anything outside the root. Paths are joined, cleaned and then verified to still be
  inside; one that climbs out is refused rather than clamped.
- Answer a request from another page. Everything under `/api` needs the token stamped into the page
  at load, and a cross-origin caller is turned away first.
- Listen anywhere but `127.0.0.1`.
- Write to your code. It writes the settings file, the asset cache, and the comments left on a
  walkthrough, which go beside the settings and never into the walkthrough itself.

## Offline

Mermaid, the highlighter and the two typefaces are fetched once through the server and cached on
disk, so the page keeps working on a plane. Without them a diagram falls back to its own source and
code renders unhighlighted, which is still the thing being described.
