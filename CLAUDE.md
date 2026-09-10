# cw

The walkthrough tool and the site that hosts walkthroughs, as one Go binary. `README.md` is what it
does; this is what is worth knowing before changing it.

## Running the checks

```
go build ./... && go test ./... && gofmt -l . && go vet ./...
node --check web/app.js
python examples/rebuild.py && ./cw check examples/cw-itself.json
```

`examples/cw-itself.json` is the golden copy: it explains this repository, so every snippet in it is
a slice of a real file here. Editing the code moves those lines, and `cw check` will say so. Fix it
by re-running `rebuild.py`, not by hand-editing the JSON.

## Pushing

The remote is `sjroesink/cw`, and `gh` is usually signed in as the work account, which cannot push
there:

```
gh auth switch --user sjroesink && git push origin main && gh auth switch --user S-Roesink_innobv
```

## Things that are the way they are on purpose

**One package, one module.** Everything is `package main` in the repository root. The local reader
and the hosted server share the datamodel, the validator, the schema and the whole front-end, and
splitting them would be two copies of the thing that has to agree.

**No dependencies.** Not stdlib-only as a badge, but because the binary is copied around, built by a
skill wrapper on someone else's laptop, and put in a scratch container. `schema.go` is a small
JSON Schema reader rather than a library for that reason. It covers the keywords the walkthrough
schema uses and skips the rest rather than guessing.

**The schema is the contract, and it runs twice.** An editor validates against
`schema/walkthrough.schema.json` while an author types, and the same file, embedded, runs at load
time. There are two of each now, one per version: `schema/walkthrough.schema.json` with `doc.go`
for `cw/1`, and `schema/walkthrough.v2.schema.json` with `doc2.go` for `cw/2`. Adding a field means
the schema, the struct, and `DATA.md`, plus the matching `spec/FORMAT.md` if a second consumer would
need to know about it.

**`inspect()` in `doc.go` is where the real checks live.** Anything about content rather than shape:
a highlighted line inside its snippet, an id unique among its siblings, an anchor matching the text
under it. New checks go there, and errors refuse while warnings do not.

## The format is versioned, and other people may read it

`cw/2` is what a new walkthrough is written as, and `cw/1` is still read, still published and still
specified. Adding an optional field is free; removing one, renaming one or changing what one means
is not, and needs a new version string with both served. `spec/FORMAT.md` states that promise, so it
is a promise.

For `cw/2` the promise goes further: **no new block type.** The seven are the seven, and anything
else is an `extension`, which carries the fallback that makes a reader who does not know it still
able to read the walkthrough. `ext` is the other open place, for what one consumer needs and the
format has no opinion about.

Which version a document is read as comes from `version` in the document and nothing else.
`load.go` is that seam: it picks the schema, the struct and the checks, and the page picks its
renderer the same way.

## Who may read what

`gate.go` is the only place that decides. Two independent locks, a password and a list of
addresses, and a walkthrough that sets both needs both. The key that can change a walkthrough also
opens it, which is what lets whoever published it read their own page.

Which lock a walkthrough gets on its first publish is not the publisher's memory either.
`defaultLock` in `publish.go` asks `gh` whether the repository is public and puts a password on
everything else, including everything it could not confirm. It is one function and it takes the
answer rather than fetching it, so the rule can be read and tested without a forge. Republishing
never touches the lock: `f.password` stays nil and the site keeps what it had.

Both secrets live in their own file beside the walkthrough rather than in `meta.json`, because
`meta.json` is handed to every reader. Keep it that way: a secret that is not in the struct that
gets serialised cannot leak by someone adding a field to a response, and there is a test that fails
if one ever ends up in one.

The address a rule is checked against comes from `clientIP` in `access.go`, which believes a header
only when every hop that could have written it is in `CW_TRUSTED_PROXIES`. `TestAForgedForwardedForIsIgnored`
is the test that matters there; do not weaken it to make something convenient work.

## What the hosted page cannot do

It has no working tree and no editor. So there is no `/api/open`, no live snippet check, and the
freshness of the code is whatever the publisher's tree said, stored per snippet in `check` and in
aggregate in `meta.verified`. Anything tempting a hosted page into reaching onto a reader's machine
belongs in `cw open` instead, which pulls the walkthrough down to where the code already is.

## A worktree is borrowed, never taken

`cw open` can add a worktree so a walkthrough is read at the commit it was written against, and
`worktree.go` is the only place that touches anybody's checkout. Three rules hold it together.

It removes only what it added, which is why every one goes into `worktrees.json` beside the settings
the moment it is made: that record is what `cw worktrees clean` works from after a run that was
killed, and nothing outside it is ever a candidate. It never passes `--force` to `git worktree
remove`, so one with work in it stays and says so. And the question it asks cannot block: `askYes`
wants a terminal on both ends and gives up after thirty seconds, because `cw open` waiting on a
prompt nobody can see never serves the page it was asked for.
