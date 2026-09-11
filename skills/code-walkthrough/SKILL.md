---
name: code-walkthrough
description: "Create a self-contained cw/2 walkthrough of one pull request or a specific part of a codebase, with a clear scope, verified code locations, and guided explanations for web and VS Code readers. Use for requests such as 'maak een code walkthrough van deze PR', 'walk me through this subsystem', or '/code-walkthrough X'."
---

# code-walkthrough

You turn a change into a page a reviewer can work through: an overview of what the change
is made of, and inside each part the sections and steps that explain it, with the code, a
diagram, a diff or an animation next to the prose.

**Your job is one file.** The rest exists: a format called `cw/2`, a reader that runs on
your machine, a VS Code extension and a site, and an API to publish to. None of them knows any
topic. Adding a walkthrough is writing a document and nothing else.

Read [RULES.md](RULES.md) for scope and evidence, then [DATA.md](DATA.md) for the cw/2
fields. Change the document, not the reader. For an ordinary code question, answer directly;
when a walkthrough is requested, even a small topic can have a short walkthrough.

## Steps

1. **Choose the boundary.** Identify the requested PR or path, symbol or behavior. Apply
   the PR or component mode in `RULES.md`. Put the question answered, entry point, result
   and relevant exclusions in `summary`, so the scope is clear without this conversation.
   Keep one requested PR together even if it contains several concerns. Ask for a missing
   target only when it cannot be inferred.

2. **Read evidence at a known revision.** Inspect PR metadata, changed files and diff, then
   the actual head files and relevant base files. For a component, trace the requested flow
   from entry to result. Record each hop's file, symbol, decision and next hop. Follow
   dependencies only far enough to explain their contract in this flow. Record repository
   and commit metadata from the same snapshot; describe any relevant uncommitted changes.

3. **Lay out the explanation.** Order parts and steps by behavior, with a reason for each
   transition to another location. Use the required parts/sections/steps structure without
   quotas. One part with one section and one step is valid. Do not pad a small PR with a
   repository tour.

4. **Write one portable file.** Set `$schema` to `https://cw.roesink.dev/schema/v2.json`
   and `version` to `cw/2`. Fill `source` with repository URL and revision, plus PR URL,
   comparison revisions and changed files when applicable. This must be useful before
   publishing. Embed the prose, verbatim snippets and diagrams; source paths are relative
   to the repository, never to a machine's checkout. Label examples and omit their source.
   Use ordered blocks for separate locations. Preserve stable IDs when editing. Ranges in
   `highlights` and `annotations` are snippet-relative. Optional `endLine` and `hash` can be
   filled by `cw migrate`; do not invent them or rely on publishing to identify the snapshot.

5. **Check scope and evidence.** Run:
   ```text
   cw check walkthrough.json --root <checkout-for-the-recorded-revision>
   ```
   Investigate stale snippets: the cause may be the wrong snapshot, changed code or an
   incorrect excerpt. Use a matching checkout or isolated worktree, preserving the user's
   active checkout. Apply the final isolation checks in `RULES.md`. Report unresolved
   warnings and any verification that could not be run.

6. **Read the result.** Open the JSON in the VS Code reader, or run:
   ```text
   cw serve walkthrough.json --root <checkout-for-the-recorded-revision>
   ```
   Check entry, representative transitions and conclusion, including code navigation and
   diagram links. Render each diagram when tooling is available. State any reader checks
   that could not be performed.

7. **Deliver the file.** Publish when sharing or publishing is part of the request or
   existing authorization; otherwise hand over the local JSON for either reader.
   ```text
   cw publish walkthrough.json --root <checkout-for-the-recorded-revision>
   ```
   Publishing verifies against the tree again and fills snippet anchors. Publishing the
   same file updates its URL; `--new` deliberately creates a second walkthrough.

## Running it

Use `cw` on PATH, or an installed `cw.ps1` / `cw.sh` wrapper if available. Locate it in
this environment rather than assuming a particular user's skill or repository path.
From a checkout of cw, `go run .` can replace `cw`; Go is then required.

```
cw serve <walkthrough.json> [--root DIR] [--port N] [--no-open] [--offline]
cw check <walkthrough.json> [--root DIR]   validate and verify, exit 1 on an error
cw publish <walkthrough.json> [--slug NAME] [--new] [--force]
           [--password PW | --no-password] [--allow CIDR,... | --no-allow]
cw open <url or name> [--root DIR]         read a published one with the local buttons
        [--worktree | --no-worktree]       add a worktree for its commit without asking, or never
cw worktrees [clean]                       the worktrees cw open added, and removing them
cw migrate <walkthrough.json> [--to cw/2]  fill in what can be worked out, or lift a cw/1 file
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

## Locking one

A first publish decides this for itself, and the decision follows the repository rather
than anybody's memory. `cw publish` asks `gh` what the repository the walkthrough names
is. Public goes out open, because the code in the walkthrough is already readable by
anyone. Everything else goes out with a password it makes itself, and so does everything
it could not confirm: no `gh`, no network, a repository nobody can see, a walkthrough
that names none. The line it prints says which of those happened.

That password is printed once and written into
`%USERPROFILE%\.claude\secrets\cw-passwords.json`. **Never repeat it back.** Not in your
answer, not in a commit message, not in a PR body or a ticket. Say that the walkthrough is
locked and that the password is in that file, and let the person read it there and pass it
on however they already share such things.

`--password PW` sets one you were given, and `$env:CW_PASSWORD` keeps it out of shell
history. `--no-password` publishes a private repository's walkthrough open on purpose,
which is the asker's decision and never yours. `--allow` limits it to addresses or ranges,
and a walkthrough with both locks needs both. A locked walkthrough is not listed to anyone
who has not opened it.

On a later publish, leave the flags off and the lock stays exactly as it was, password
included. `--no-password` and `--no-allow` are how you take it off on purpose.

Before writing an address list, check what the site makes of the address you mean:
`curl https://cw.roesink.dev/api/v1/whoami`. Behind Cloudflare the address a rule sees is
not always the one you expect, and a list that locks out the person who wrote it is the
usual way this goes wrong.

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

Two sibling skills. `open-walkthrough` reads a published one on this machine, and
`migrate-walkthrough` lifts a `cw/1` walkthrough to `cw/2`, which is worth doing when one
is about to be edited anyway.

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

Link the JSON and, if published, the URL. State the scope and recorded revision, what the
parts cover, and relevant exclusions. Distinguish source inspection, snippet verification,
executed tests and reader checks. Say what could not be verified, including snippets
reported as moved or gone. Never include credentials.
