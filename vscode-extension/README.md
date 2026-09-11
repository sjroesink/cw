# Code Walkthrough for VS Code

Read a `cw/2` walkthrough beside the actual code. Choosing a step opens its first
source location, highlights the relevant lines, and shows the explanation in a
sidebar. Steps with several snippets let you move between files without losing
the surrounding explanation.

## Install and read

Install `cw-walkthrough.vsix` through **Extensions → … → Install from VSIX…**,
or run:

```powershell
code --install-extension .\cw-walkthrough.vsix
```

1. Open the repository the walkthrough describes in VS Code.
2. Run **CW: Open Walkthrough…** from the Command Palette, or right-click a JSON
   file in Explorer and choose **CW: Open Walkthrough…**.
3. Select a step in **Code Walkthrough → Contents**. Read its explanation in the
   **Walkthrough** view and use **Next** / **Previous** to follow the tour.
4. Use **Show code** or **Code →** for another location within the same step.

Both views can be moved to the Secondary Side Bar using VS Code's **Move View**
action, so the walkthrough can sit to the right of your editor.

`Alt+Shift+PageDown` and `Alt+Shift+PageUp` move between steps. **Next** marks the
current step complete; **Finish** completes the final step. You can also mark a
step complete or unread independently. Progress is stored by document URI and
stable step/block IDs in VS Code's workspace state. **CW: Resume Walkthrough**
reopens the current location; **CW: Restart Walkthrough** resets its progress.

The extension uses the workspace containing the JSON file, or the only open
workspace folder when the file was downloaded elsewhere. Use **Repository…** or
**CW: Choose Repository Folder…** when that is not the source repository. With
multiple workspace folders, or a snippet from another repository, choose the
appropriate folder. Each repository mapping is remembered for that walkthrough. Each snippet shows its local
verification status. Source links choose the PR diff when applicable, with commit permalinks
as fallback; **Copy source link** copies that location.

## Supported content

- Markdown, callouts, embedded code, snippet highlights and annotations. Code
  annotations also appear as editor hovers.
- Multiple code locations per step, in document order.
- Local snippet checking, including unsaved editor changes. A uniquely moved
  snippet opens at its current position. Changed, missing or ambiguous snippets
  stay readable in the sidebar without highlighting an unverified location.
- Diffs with syntax coloring, old/new line gutters, copying and source navigation.
- Mermaid diagrams with keyboard-accessible code links, source toggle and full-size zoom.
- Timelines with playback, pause, frame selection and remembered position.
- Overview and part pages with progress and ordered navigation.
- Published walkthrough downloads, password/key access, live updates and an offline copy.
- An extension block's author-provided fallback.
- Live reload of the open walkthrough; temporarily invalid JSON retains the
  last valid reading and displays the error.

The extension reads **cw/2** files in desktop VS Code. Source code uses VS Code's own
syntax highlighting; embedded snippets and diagrams render with bundled offline assets.
It does not change Git revisions or manage worktrees. The web-reader feature audit and test evidence are recorded in `PARITY.md` in the source repository.

Published walkthroughs accept a page URL, API URL or name. `cw.site` controls the site for
names. Successful unlock credentials are kept in VS Code SecretStorage. The downloaded
JSON stays available when the site is offline; access failures and updates appear in the
reader. Embedded content cannot issue network requests.

For `cw/1`, convert a copy using `cw migrate <file> --to cw/2` and resolve any
migration notes. Unsupported format versions are refused.

No separate Go installation or running web server is needed to use the extension.
Opening Comments starts a bundled cw helper on loopback, or connects to the existing
local server for this walkthrough. That server is the only writer of comment data.
The extension does not write to the walkthrough or source files, execute walkthrough
content, or fetch embedded images. Files are resolved within the selected repository, including symlink
containment checks. Web links open only after the reader clicks them.

## Comments and reading settings

Use **Comments** (or `c`) to show questions and agent replies. Select prose or code in the
walkthrough and choose **Comment on selection**, or use **CW: Comment on Editor Selection**
for code in the editor. Replies can contain all eight block types. Archive hides a thread;
**Show archived** and **Restore** bring it back. **Copy agent prompt** includes the bundled
CLI path and the watch instructions. The extension does not start an agent or send your
question to an external service.

Comments use the same local store as `cw comments`, keyed by the walkthrough's local path
or published name. They remain outside the JSON and survive closing the walkthrough.
A helper started by this extension stops when its reading closes; an existing server is
left running. **Reconnect comments** recovers from a stopped server.

**Settings** (`s`) opens the extension's VS Code settings. Choose automatic, light or dark
appearance and an accent color; **Theme** (`t`) switches light/dark in the reader. `Escape`
goes up to the part or overview, and `o` opens the current code. Shortcuts do not intercept
input controls. **Copy page link** copies a published deep link or a VS Code URI for a local
file. A local link requires that file at the same location on the receiving machine.

## Develop

Building requires Node.js and the Go toolchain declared in the parent `go.mod`.
From this directory:

```powershell
npm ci
npm run check
npm test
npm run test:integration
npm run test:browser
npm run package
```

Open this directory in VS Code and press **F5** to launch an Extension Development
Host with the parent repository open. Try `../examples/cw2-tour.json`, which
exercises all seven block types.

The integration runner creates an isolated fixture workspace and VS Code profile
under `.vscode-test`. By default it downloads a VS Code test build. To use an
existing executable, set `CW_VSCODE_EXECUTABLE` to its absolute path.

## Implementation

`src/model.ts` loads the shared JSON Schema from `../schema`, validates the
semantic rules from `../spec/FORMAT.md`, and resolves snippets. The reader allows
future optional properties and enum values while the packaged authoring schema
remains strict. Hashes normalize CRLF to LF and preserve trailing newlines.
Highlights and annotations use 1-based snippet-relative lines; the editor uses
0-based file coordinates. A different repository never inherits the document's
revision.

`src/extension.ts` owns navigation, repository mappings, editor decorations,
workspace progress and view lifecycle. `src/remote.ts` handles bounded downloads and access locks. `src/render.ts` renders content into a
local webview using VS Code theme colors. Scripts are restricted by a nonce-based
Content Security Policy; raw HTML is escaped and messages are checked against
the active view and document before navigation.

The build bundles the shared schema and the local cw helper. `cw serve --comment-key`
lets a downloaded document use the same comments as its published name. Packaging produces
a VSIX for the build machine's platform and architecture, including its native helper.
Build on the intended extension-host platform when using Remote SSH or another OS.


## References (0.2.0)

The standard cw/2 `reference` block opens another walkthrough in this sidebar, optionally at a
`target.stepId`. Local files resolve relative to the current walkthrough's directory; a published
URL is the alternative if the file cannot be read. Downloaded documents use their published URL
targets, not paths next to the cache. **Back to previous walkthrough** restores the previous page
and selected snippet. Missing targets leave the current reading available. Try
[`examples/references.json`](../examples/references.json). The extension bundles the updated v2 schema.
