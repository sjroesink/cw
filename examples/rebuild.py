#!/usr/bin/env python3
"""Rebuild examples/cw-itself.json from the source it describes.

The example walkthrough explains this repository, so every snippet in it is a
slice of a real file. Pasting them by hand is how a snippet quietly stops
matching the code; slicing them here means the only thing that can be wrong is
which lines were asked for, and `cw check` catches that too.

Run from the repository root:

    python examples/rebuild.py && cw check examples/cw-itself.json
"""

import json
import pathlib
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
OUT = ROOT / "examples" / "cw-itself.json"


def slice_of(path, first, last):
    """The lines first..last of a file, inclusive, numbered the way an editor does."""
    text = (ROOT / path).read_text(encoding="utf-8").replace("\r\n", "\n")
    lines = text.split("\n")
    if last > len(lines):
        sys.exit(f"{path} has {len(lines)} lines, and {last} was asked for")
    return "\n".join(lines[first - 1:last])


def code(path, first, last, **extra):
    block = {"file": path, "from": first, "text": slice_of(path, first, last)}
    block.update(extra)
    return block


def ref(path, first, last, note):
    return {"file": path, "from": first, "note": note, "code": slice_of(path, first, last)}


WALKTHROUGH = {
    "$schema": "../schema/walkthrough.schema.json",
    "version": "cw/1",
    "title": "cw, walked through itself",
    "source": {
        "kind": "subsystem",
        "provider": "github",
        "repo": "sjroesink/cw",
        "number": "the cw binary",
        "url": "https://github.com/sjroesink/cw",
    },
    "summary": (
        "This walkthrough explains the tool that is showing it. Three parts: what a walkthrough "
        "actually is and what is checked about it, what the local reader can do because it has your "
        "working tree in front of it, and what the published page does instead, having neither your "
        "tree nor your editor. Every snippet below is sliced out of this repository by "
        "examples/rebuild.py, so it is the real code or the check fails."
    ),
    "root": "..",
    "language": "go",
    "parts": [
        {
            "title": "A walkthrough is a file",
            "desc": "One JSON document, a schema that is both the authoring contract and what the server enforces.",
            "long": (
                "Nothing in the page or the server knows any topic. They read a document and render "
                "whatever is in it, which is what makes it possible for something else to read the same "
                "document: an editor plugin, or something that reads it out loud. That only works if the "
                "document says which format it is in and states its own facts in full, so that is what "
                "cw/1 is for."
            ),
            "files": ["doc.go", "schema.go", "walkthrough.schema.json"],
            "sections": [
                {
                    "title": "The document, and its version",
                    "desc": "What is in a walkthrough, and why it says so out loud.",
                    "steps": [
                        {
                            "title": "The whole content is one struct",
                            "body": (
                                "A walkthrough is a title, a summary, and a list of parts that hold sections "
                                "that hold steps. There is no template and no per-topic code anywhere: adding "
                                "a walkthrough is adding a file."
                            ),
                            "code": code("doc.go", 21, 40,
                                         hi=[23, 37, 38],
                                         notes=[{"line": 37, "text": "The one field that has to be an exact value, and there are two of them now. A reader that does not know the version refuses the file instead of guessing which half of it it still understands."}]),
                            "diagram": {
                                "kind": "flow",
                                "caption": "One document, and more than one thing that can read it.",
                                "def": "flowchart LR\n  json[\"walkthrough.json\"] --> schema[\"walkthrough.schema.json\"]\n  schema --> local[\"cw serve\"]\n  schema --> host[\"cw host\"]\n  local --> tree[\"your working tree\"]\n  host --> gh[\"GitHub\"]\n  json -.-> other[\"an editor plugin,\\nsomething that reads it aloud\"]",
                                "refs": {
                                    "schema": ref("schema.go", 233, 242, "The same schema an editor validates against is enforced here, so the two cannot drift apart."),
                                },
                            },
                        },
                        {
                            "title": "Where it came from is one block, not four fields",
                            "body": (
                                "The repository, the pull request and the commit used to be loose strings for "
                                "the header. Together they are the only thing that can turn a file path and a "
                                "line number into a link, so they belong in one place a machine can read."
                            ),
                            "callout": "commit is what makes a link permanent. Without it a snippet can only point at a branch tip, which is a link that quietly starts lying.",
                            "code": code("doc.go", 42, 55, hi=[49, 52]),
                        },
                    ],
                },
                {
                    "title": "What a schema cannot say",
                    "desc": "The checks that need to look at the content, not the shape.",
                    "steps": [
                        {
                            "title": "An anchor has to match the text under it",
                            "body": (
                                "A snippet states where it ends and what it hashes to. Both are written by the "
                                "publisher rather than by hand, so if they disagree with the text, the text was "
                                "edited afterwards and the anchor is now something a consumer would act on and "
                                "be wrong about."
                            ),
                            "code": code("doc.go", 294, 310, hi=[303, 307]),
                        },
                        {
                            "title": "Two ids in one place is worse than none",
                            "body": (
                                "Progress and every deep link hang off the ids in the document. Two siblings "
                                "with the same id means a link lands on whichever one the reader's browser "
                                "happened to find first, which is why this is an error rather than a warning."
                            ),
                            "code": code("doc.go", 170, 180, hi=[176]),
                        },
                    ],
                },
            ],
        },
        {
            "title": "Where the code is",
            "desc": "The local reader has your working tree and your editor, and both are things worth being careful with.",
            "long": (
                "Running cw serve puts a server on your machine that reads your files and starts your "
                "editor. That is a lot of authority for a page in a browser to have, so most of the "
                "interesting code here is about what it refuses rather than what it does."
            ),
            "files": ["main.go", "doc.go", "ide.go"],
            "sections": [
                {
                    "title": "Nothing else gets to drive it",
                    "desc": "Two refusals before any path is resolved.",
                    "steps": [
                        {
                            "title": "The caller has to prove where it came from",
                            "body": (
                                "A server on localhost can be reached by any site you have open. So the origin "
                                "has to be this loopback server, and the caller has to know the token that was "
                                "stamped into the page when it was served. Only then does anything get read."
                            ),
                            "code": code("main.go", 623, 635, hi=[625, 629]),
                        },
                        {
                            "title": "A path is checked, not cleaned",
                            "body": (
                                "Every file path in a walkthrough is joined onto the root, cleaned, and then "
                                "asked whether it is still inside. A path that climbs out is refused. Clamping "
                                "it instead would open a file the walkthrough never named."
                            ),
                            "code": code("doc.go", 334, 345, hi=[340, 342]),
                        },
                    ],
                },
                {
                    "title": "Is that code still there?",
                    "desc": "The snippet is a copy, and this is the only thing that notices when a copy goes stale.",
                    "steps": [
                        {
                            "title": "Look where it says, then look everywhere",
                            "body": (
                                "First the lines are compared where the snippet claims to be. If they are not "
                                "there, the whole file is searched for them. Finding them somewhere else is a "
                                "different answer from not finding them at all."
                            ),
                            "code": code("doc.go", 422, 436, hi=[422, 429, 435]),
                            "notes": None,
                        },
                        {
                            "title": "Moving is not changing",
                            "body": (
                                "Code that shifted down because something was inserted above it is still the "
                                "code the walkthrough describes. Code that is gone is not. The page shows the "
                                "two differently, and the count in the banner only holds the second."
                            ),
                            "code": code("doc.go", 118, 127, hi=[122]),
                        },
                    ],
                },
            ],
        },
        {
            "title": "Where the code is not",
            "desc": "The published page has no working tree and no editor, so it has to say less and say it honestly.",
            "long": (
                "A walkthrough is worth more once other people can read it, and they will not have your "
                "checkout. Publishing is therefore split: everything that needs the code happens on the "
                "machine that has it, and the site stores the answer."
            ),
            "files": ["publish.go", "api.go", "github.go", "app.js"],
            "sections": [
                {
                    "title": "Publishing does the part that needs the code",
                    "desc": "Check first, then fill in what nobody should have to type.",
                    "steps": [
                        {
                            "title": "A stale snippet stops the publish",
                            "body": (
                                "The site cannot check anything, so this is the last moment anyone can. A "
                                "snippet that is no longer in the tree stops the publish rather than going out "
                                "quietly, and --force is how you say you meant it."
                            ),
                            "callout": "--force does not hide anything: the page says which snippets were already out of date when it was published.",
                            "code": code("publish.go", 109, 122, hi=[113, 118]),
                        },
                        {
                            "title": "Ids and anchors are filled in, not typed",
                            "body": (
                                "By the time a document is stored it has an id on every part, section and step "
                                "and a hash on every snippet. An author never writes those, and a consumer can "
                                "always rely on them being there."
                            ),
                            "code": code("api.go", 155, 160, hi=[158, 159]),
                        },
                    ],
                },
                {
                    "title": "A line number without an editor",
                    "desc": "What the open button becomes when there is no machine to open anything on.",
                    "steps": [
                        {
                            "title": "Into the diff, or onto the commit",
                            "body": (
                                "A file the pull request touches links into the diff, where the reader sees the "
                                "change rather than only the result. Anything else links to the file at the "
                                "commit, which stays right after the branch has moved on. With neither, there "
                                "is nothing to link to and the answer is nil rather than an object full of "
                                "empty strings."
                            ),
                            "code": code("github.go", 37, 65, hi=[44, 47, 50]),
                            "diagram": {
                                "kind": "flow",
                                "caption": "One decision, made once per walkthrough rather than once per click.",
                                "def": "flowchart TD\n  s[\"source\"] --> q{\"file in\\nchangedFiles?\"}\n  q -->|yes| diff[\"pull/N/files#diff-<sha256>R<line>\"]\n  q -->|no| blob[\"blob/<commit>/<path>#L18-L24\"]\n  q -->|no commit,\\nno pull request| none[\"no link\"]",
                                "refs": {
                                    "diff": ref("github.go", 68, 73, "The anchor is the sha256 of the path. GitHub does not document this, so it has a test and a fallback."),
                                },
                            },
                        },
                        {
                            "title": "The page asks for a link, not for an editor",
                            "body": (
                                "The server works the link out once and hands the page a base URL and a map of "
                                "anchors. So the page does no hashing, makes no request, and the same function "
                                "in the same file serves both the local button and the hosted one."
                            ),
                            "code": code("web/app.js", 1022, 1033, lang="javascript", hi=[1025, 1029]),
                        },
                    ],
                },
            ],
        },
    ],
}


def strip_nones(value):
    if isinstance(value, dict):
        return {k: strip_nones(v) for k, v in value.items() if v is not None}
    if isinstance(value, list):
        return [strip_nones(v) for v in value]
    return value


def main():
    OUT.write_text(json.dumps(strip_nones(WALKTHROUGH), indent=2) + "\n", encoding="utf-8")
    steps = sum(len(sec["steps"]) for part in WALKTHROUGH["parts"] for sec in part["sections"])
    print(f"{OUT.relative_to(ROOT)}: {len(WALKTHROUGH['parts'])} parts, {steps} steps")


if __name__ == "__main__":
    main()
