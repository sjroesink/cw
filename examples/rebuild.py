"""Build the shipped example for the code-walkthrough skill.

Every snippet is sliced out of the real source, so `cw check` on the result
passes on any machine that has the skill. Run it again after touching the
server and re-run `cw check` to see what moved.
"""
import io, json, os, re, sys

SKILL = r"C:/Users/SanderRoesink/.claude/skills/code-walkthrough"
SRC = SKILL + "/server"


def slice_block(rel, start_re, end_re, after=0, plus=0):
    """Return {file, from, text} for the first block matching start_re through end_re.

    `plus` extends past the matched end line, for a block whose real close sits a
    couple of lines below the thing worth searching for.
    """
    path = os.path.join(SRC, rel)
    lines = io.open(path, encoding="utf-8").read().replace("\r\n", "\n").split("\n")
    start = next(i for i, l in enumerate(lines) if re.search(start_re, l))
    end = next(i for i in range(start + after, len(lines)) if re.search(end_re, lines[i])) + plus
    return {
        "file": "server/" + rel,
        "from": start + 1,
        "text": "\n".join(lines[start:end + 1]),
    }


def code(rel, start_re, end_re, plus=0, **extra):
    block = slice_block(rel, start_re, end_re, after=1, plus=plus)
    block.update(extra)
    return block


def ref(rel, start_re, end_re, label, note):
    block = slice_block(rel, start_re, end_re, after=1)
    return {"label": label, "file": block["file"], "from": block["from"],
            "note": note, "code": block["text"]}


schema_step = code(
    "schema/walkthrough.schema.json", r'^    "step": \{', r'^    \},',
    lang="json",
    note="the step definition",
    hi=[],
    notes=[],
)
# The gutter numbers are the file's own, so the highlights are too.
first = schema_step["from"]
step_lines = schema_step["text"].split("\n")
for i, line in enumerate(step_lines):
    if '"$ref": "#/$defs/' in line:
        schema_step["hi"].append(first + i)
    if '"required"' in line:
        schema_step["notes"].append({
            "line": first + i,
            "text": "Title and body are the only fields a step cannot do without. Everything that makes it worth looking at is optional, which is deliberate: a step that has nothing to show should read as thin, not fail to load.",
        })
    if '"callout"' in line:
        schema_step["notes"].append({
            "line": first + i,
            "text": "The one block that is prose rather than a picture. It is for the thing that will bite the next person, which is why it is a field of its own and not a paragraph in the body.",
        })

validator = code(
    "schema.go", r"^func \(v \*validator\) object\(", r"v\.fail\(join\(at, k\)", plus=2,
    note="the closed-object check",
    hi=[],
    notes=[],
)
vfirst = validator["from"]
for i, line in enumerate(validator["text"].split("\n")):
    if "is not a field here" in line or "additionalProperties" in line:
        validator["hi"].append(vfirst + i)
    if "An unknown key is nearly always" in line:
        validator["notes"].append({
            "line": vfirst + i,
            "text": "This is the whole reason the schema is enforced at load time and not only in the editor. A key nobody reads is silent: the step renders, the field does nothing, and the author has no way to find out.",
        })
    if "quoteAll(missing)" in line:
        validator["notes"].append({
            "line": vfirst + i,
            "text": "Missing fields are reported together rather than one per run, because an author fixing a file wants the whole list.",
        })

in_range = code(
    "doc.go", r"^func checkStep\(", r'^\t\tfor i, n := range c\.Add \{',
    note="what the schema cannot say",
    hi=[],
    notes=[],
)
ifirst = in_range["from"]
for i, line in enumerate(in_range["text"].split("\n")):
    if "points at line %d" in line:
        in_range["hi"].append(ifirst + i)
        in_range["notes"].append({
            "line": ifirst + i,
            "text": "A schema can say hi is a list of positive integers. It cannot say those integers have to be inside this particular snippet, and getting that wrong lights the wrong line rather than failing.",
        })
    if "first, last :=" in line:
        in_range["hi"].append(ifirst + i)

guard = code(
    "main.go", r"^// guard keeps another page", r"^\}$",
    note="every /api route goes through here",
    hi=[],
    notes=[],
)
gfirst = guard["from"]
for i, line in enumerate(guard["text"].split("\n")):
    if "X-Cw-Token" in line or "cross-origin requests are not served" in line:
        guard["hi"].append(gfirst + i)
    if "X-Cw-Token" in line:
        guard["notes"].append({
            "line": gfirst + i,
            "text": "Sixteen random bytes, new on every start, stamped into the page as it is served. A page this server never handed out cannot know it, which is what stops another tab from driving the editor.",
        })

goto = code(
    "ide.go", r"^// gotoArg is the ", r"^\}$",
    note="the shared spelling",
    hi=[],
    notes=[{"line": 0, "text": ""}],
)
gtfirst = goto["from"]
goto["notes"] = []
for i, line in enumerate(goto["text"].split("\n")):
    if "return file + \":\"" in line:
        goto["hi"].append(gtfirst + i)
        goto["notes"].append({
            "line": gtfirst + i,
            "text": "JetBrains wants --line N before the path, and Visual Studio has no way to say it at all. Each entry in the catalogue builds its own argv, and nothing above the catalogue knows the difference.",
        })

find = code(
    "doc.go", r"^// find looks for the block in the file", r'^\t\treturn &Check\{State: "gone"\}$',
    note="the check behind the chip",
    hi=[],
    notes=[],
)
ffirst = find["from"]
for i, line in enumerate(find["text"].split("\n")):
    if 'State: "missing-file"' in line or 'State: "gone"' in line:
        find["hi"].append(ffirst + i)
    if 'State: "unchecked"' in line:
        find["notes"].append({
            "line": ffirst + i,
            "text": "Without a root there is nothing to compare against, and the page says unchecked rather than pretending the snippet was verified.",
        })

doc = {
    "$schema": "../server/schema/walkthrough.schema.json",
    "title": "code-walkthrough, walked through itself",
    "repo": "SanderClaudeSkills",
    "number": "skill: code-walkthrough",
    "state": "shipped",
    "summary": "This walkthrough explains the server that is serving it. Two parts: how a walkthrough is nothing but a JSON file and the schema behind it, and what happens between clicking a line number here and the cursor landing in your editor. Every snippet below is sliced out of the skill directory, so it resolves on any machine that has the skill.",
    "root": "..",
    "language": "go",
    "parts": [
        {
            "title": "The data is the whole walkthrough",
            "desc": "One JSON file per topic, and a schema that is both the authoring contract and what the server enforces.",
            "long": "The page holds no topic of its own: no titles, no code, no diagrams. It reads one JSON file and renders whatever is in it. That file is written against a JSON schema, so an editor validates it while it is being typed, and the same schema runs again at load time, so the two cannot drift apart.",
            "files": ["walkthrough.schema.json", "schema.go", "doc.go"],
            "sections": [
                {
                    "title": "One file, one schema",
                    "desc": "What a step is allowed to contain, and who says so.",
                    "steps": [
                        {
                            "title": "A step is some prose and up to four blocks",
                            "body": "Title and body are required. Everything that makes a step worth looking at is optional: a mermaid diagram, a snippet, a diff, a small animation. A step picks the ones that earn their place, and the page renders them in that order.",
                            "diagram": {
                                "kind": "flow",
                                "caption": "The page is a renderer. Everything it knows arrives as JSON.",
                                "def": "flowchart LR\n  json[\"walkthrough.json\"] --> schema[\"walkthrough.schema.json\"]\n  schema --> server[\"cw server\"]\n  server --> page[\"the page\"]\n  page -.->|\"open in IDE\"| editor[\"your editor\"]\n  server -.->|\"reads\"| tree[\"the working tree\"]",
                                "refs": {
                                    "schema": ref("schema.go", r"^func loadSchema\(", r"^\}$",
                                                  "the schema, loaded once",
                                                  "The schema is embedded in the binary, so the check the editor runs and the check the server runs are the same bytes."),
                                    "server": ref("doc.go", r"^func LoadDoc\(", r"^\}$",
                                                  "reading a walkthrough",
                                                  "Validate first, unmarshal second. A file with an unknown key is reported before anything tries to make sense of it."),
                                },
                            },
                            "code": schema_step,
                        },
                        {
                            "title": "The schema is not advice, it is the gate",
                            "body": "A JSON schema in an editor is a suggestion: it helps while you type and does nothing afterwards. The server reads the same file and enforces it, so a typo in a field name is an error with the alternatives listed, rather than a field that silently does nothing.",
                            "code": validator,
                            "callout": "The validator reads the part of JSON Schema this file actually uses: type, properties, required, additionalProperties, items, enum, minimum, minItems, minLength, pattern and a local $ref. Reach for a keyword outside that set and it will be skipped in the server while your editor still honours it, which is the one way the two can disagree.",
                        },
                    ],
                },
                {
                    "title": "What a schema cannot say",
                    "desc": "The checks that need to know what the fields mean.",
                    "steps": [
                        {
                            "title": "A highlighted line has to be inside its own snippet",
                            "body": "Line numbers in hi, add and notes are the numbers the gutter shows, which is the file's own numbering when the snippet says where it starts. A schema can only say those are positive integers. Whether they land inside this snippet is something only the loader can know.",
                            "code": in_range,
                        }
                    ],
                },
            ],
        },
        {
            "title": "From a line number to your editor",
            "desc": "The server half: a token, a path check, and one entry per editor.",
            "long": "Clicking a line number here puts the cursor on that line in your own editor. That is a local server launching a process, so the interesting part is not the launching: it is everything that has to be refused first.",
            "files": ["main.go", "ide.go", "doc.go"],
            "sections": [
                {
                    "title": "The request has to prove where it came from",
                    "desc": "Localhost is reachable from every page in the browser.",
                    "steps": [
                        {
                            "title": "Two checks before anything is opened",
                            "body": "A server on localhost can be reached by any site you have open, so the origin has to be this loopback server and the caller has to know the token that was stamped into the page at load. Only then does the path get resolved, and a path that climbs out of the root is refused rather than clamped.",
                            "diagram": {
                                "kind": "sequence diagram",
                                "caption": "Three ways to be turned away, one way through.",
                                "def": "sequenceDiagram\n    autonumber\n    participant P as the page\n    participant G as guard\n    participant T as Tree\n    participant E as your editor\n    P->>G: POST /api/open\n    G->>G: origin and token\n    G-->>P: 403 for a stranger\n    G->>T: safePath(file)\n    T-->>P: 400 outside the root\n    T->>E: launch at file:line",
                                "refs": {
                                    "Tree": ref("doc.go", r"^func \(t \*Tree\) safePath\(", r"^\}$",
                                                "safePath",
                                                "Join, clean, then verify the result is still under the root. One function, used by the open handler and by the check that reads files."),
                                },
                            },
                            "code": guard,
                        },
                        {
                            "title": "Every editor spells \"line 42\" differently",
                            "body": "The VS Code family, Zed and Sublime take the line glued onto the path. JetBrains wants a flag. Visual Studio cannot be told at all. One catalogue entry per editor holds its own argument builder, and finding the editor is a ladder: the terminal this server was started from, then PATH, then the places the installers put things.",
                            "code": goto,
                            "anim": {
                                "frames": [
                                    {"label": "the terminal", "note": "Started from inside an editor? Its environment says so, and that counts as an answer.", "nodes": [
                                        {"label": "terminal hint", "sub": "TERM_PROGRAM", "state": "active"},
                                        {"label": "PATH", "sub": "", "state": "idle"},
                                        {"label": "install paths", "sub": "", "state": "idle"},
                                        {"label": "URL handler", "sub": "vscode://", "state": "idle"}]},
                                    {"label": "PATH", "note": "Otherwise the shell commands are tried in catalogue order.", "nodes": [
                                        {"label": "terminal hint", "sub": "nothing", "state": "done"},
                                        {"label": "PATH", "sub": "code, cursor, zed", "state": "active"},
                                        {"label": "install paths", "sub": "", "state": "idle"},
                                        {"label": "URL handler", "sub": "vscode://", "state": "idle"}]},
                                    {"label": "install paths", "note": "Plenty of installs have the editor but never added its shell command.", "nodes": [
                                        {"label": "terminal hint", "sub": "nothing", "state": "done"},
                                        {"label": "PATH", "sub": "nothing", "state": "done"},
                                        {"label": "install paths", "sub": "per platform", "state": "active"},
                                        {"label": "URL handler", "sub": "vscode://", "state": "idle"}]},
                                    {"label": "the fallback", "note": "Nothing found, so the operating system is handed the editor's own URL scheme, and the reply says so instead of pretending it worked.", "nodes": [
                                        {"label": "terminal hint", "sub": "nothing", "state": "done"},
                                        {"label": "PATH", "sub": "nothing", "state": "done"},
                                        {"label": "install paths", "sub": "nothing", "state": "done"},
                                        {"label": "URL handler", "sub": "and it says so", "state": "alert"}]},
                                ]
                            },
                        },
                    ],
                },
                {
                    "title": "Is that code still there?",
                    "desc": "A pasted snippet cannot notice that it has gone stale.",
                    "steps": [
                        {
                            "title": "The working tree gets the last word",
                            "body": "The code in this file is a paste, and a paste rots. On every load the server looks for each snippet in the file it names: found where it says, found somewhere else, or gone. The page prints that next to the file name, so a walkthrough that has aged says so instead of showing yesterday as if it were today.",
                            "code": find,
                            "callout": "Moving code is not changing it. A block found at a different line is reported as moved, not as gone, because a walkthrough that cried wolf every time someone added an import would be ignored within a week.",
                        },
                        {
                            "title": "The design linked, this server launches",
                            "body": "The imported design opened files with a URL template, which puts the whole question of which editor you have on the reader. Here the button asks the server, and the server knows: it detected the editors, it holds the setting, and it reports back what actually happened.",
                            "diff": {
                                "file": "server/web/app.js",
                                "lines": [
                                    {"kind": "ctx", "t": "function openButton(file, line, cls) {"},
                                    {"kind": "del", "t": "  const b = el(\"a\", cls, \"open in IDE ↗\");"},
                                    {"kind": "del", "t": "  const root = props.repoRoot, tpl = props.ideUrlTemplate;"},
                                    {"kind": "del", "t": "  b.href = tpl.replace(\"{path}\", root + \"/\" + file).replace(\"{line}\", line);"},
                                    {"kind": "add", "t": "  const b = el(\"button\", cls, \"open in IDE ↗\");"},
                                    {"kind": "add", "t": "  b.type = \"button\";"},
                                    {"kind": "add", "t": "  b.title = \"open \" + file + \" at line \" + line + \" in \" + whereOpens();"},
                                    {"kind": "add", "t": "  b.addEventListener(\"click\", () => openAt(file, line));"},
                                    {"kind": "ctx", "t": "  return b;"},
                                    {"kind": "ctx", "t": "}"},
                                ],
                            },
                        },
                    ],
                },
            ],
        },
    ],
}

out = SKILL + "/examples/cw-itself.json"
io.open(out, "w", encoding="utf-8", newline="\n").write(json.dumps(doc, indent=2, ensure_ascii=False) + "\n")
print("wrote", out)
