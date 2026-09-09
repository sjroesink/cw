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
cw migrate <walkthrough.json>              lift an older file to cw/1, fill in ids and anchors
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

`serve` opens a browser on 127.0.0.1. Without `--root` the root is `root` from the walkthrough, else
the git root the file sits in. Without any root the page still reads: the open buttons say so and
nothing is checked against a tree.

## The format

One JSON file per topic, written against `schema/walkthrough.schema.json`. Point at it from the file
and an editor validates while you type:

```jsonc
{
  "$schema": "https://cw.roesink.dev/schema/v1.json",
  "version": "cw/1",
  "title": "feat: reliable webhook processing",
  "source": {
    "kind": "pull-request",
    "provider": "github",
    "repo": "innovadis-dev/Fincent",
    "number": "PR #4150",
    "url": "https://github.com/innovadis-dev/Fincent/pull/4150",
    "commit": "9f2c1ab…"
  },
  "summary": "The paragraph on the overview, above the cards.",
  "parts": [ /* parts hold sections, sections hold steps */ ]
}
```

`skill/DATA.md` is the field reference. The format is versioned so that something other than this
page can read the same file, an editor plugin or a reader that plays it out loud, and the rule for
any consumer is to render what it understands and skip the rest without complaining.

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
- Write to your code. The only things it writes are the settings file and the asset cache.

## Offline

Mermaid, the highlighter and the two typefaces are fetched once through the server and cached on
disk, so the page keeps working on a plane. Without them a diagram falls back to its own source and
code renders unhighlighted, which is still the thing being described.
