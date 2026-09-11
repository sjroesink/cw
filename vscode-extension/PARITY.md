# Web reader parity audit

Goal: a cw/2 walkthrough must work fully in VS Code as it does in the web reader.
This checklist records the requirement-by-requirement completion evidence. The source of
truth is the web reader, the cw/2 format specification, and observed runtime behavior.

| Requirement | Reference | Current evidence / remaining work |
| --- | --- | --- |
| cw/2 reading only | spec/FORMAT.md, schema/walkthrough.v2.schema.json | cw/2 tested; cw/1 deliberately refused as requested |
| Overview, part pages, ordered sections/steps | web/app.js, web/state.js | Overview/part/step navigation and wrap-around tested in browser and VS Code |
| Progress, resume, unread, restart | web/state.js | Extension integration test |
| Markdown and callouts | web/ui.js, web/render2.js | Core renderer and browser interaction tests |
| Snippets, syntax coloring, copy, annotations | web/render1.js, web/render2.js | Native navigation integration; browser highlighting, clipboard messages and annotation tests |
| Mermaid diagrams, source toggle, linked nodes, full-size zoom | web/ui.js, web/render2.js | Browser SVG/link/zoom tests and actual VS Code webview CSP report pass |
| Timeline playback, pause, frame selection and timing | web/render1.js, web/render2.js | Browser frame/play/pause/resume tests; actual webview initialization verified |
| Diffs, accurate dual gutters, source links | web/render2.js | Browser dual gutters and dispatch tested; native after-file selection, diff copy and stale-token rejection pass in VS Code |
| Extension fallback | spec/FORMAT.md | Core renderer test |
| Local code checks and moved/missing states | doc.go, web/render2.js | Core match/moved/missing/ambiguous tests; VS Code verifies all snippet statuses before code selection, including dirty buffers |
| Published walkthroughs, including access locks | open.go, gate.go, web/unlock.html | Password/key, redirects and polling tested; VS Code download/update/offline integration added |
| Source/PR links and copying location links | github.go, web/ui.js, web/app.js | PR new/old/rename links and commit fallbacks tested; native page/source clipboard tested; URI handler uses the same open/address navigation |
| Comments: selection, replies, archive, watcher/CLI interoperability | comments.go, web/comments.js | Real CLI watch/reply/archive/restart test preserves quote and lines; browser selection/interactive reply test; actual VS Code polling and reply SVG test |
| Keyboard navigation, accessible controls, theme and reading preferences | web/app.js, web/ui.js | Browser shortcuts, focused-control exclusion, theme override and high-contrast preservation tested; native preferences tested; rendered layouts inspected |
| Offline assets and safe untrusted content | vendor.go, spec/FORMAT.md | Local bundles, actual CSP, containment, hostile and malformed diagram tests pass |
| Packaged extension and instructions | vscode-extension/package.json | Windows x64 VSIX contents and target verified; the extracted artifact passes the complete native integration suite |

The platform artifact built here targets Windows x64. Other extension hosts require a build
for their platform; this follows the bundled native helper. No Marketplace publication or
installation into the user's normal profile is part of this work. Editor selection and
repository mappings replace the web reader's external-editor settings.

The final gate loads the extension extracted from the VSIX in an isolated VS Code host and
runs the native integration suite, including the bundled comment helper. Core and browser
suites cover schema/content semantics and actual webview DOM interactions respectively.

Initial parity verification (2026-09-11): 16 core tests, 9 browser tests, TypeScript checking,
Go tests/vet, diff whitespace checks, and the extracted-VSIX integration suite passed.
Rendered sidebar and comments screenshots were inspected. The VSIX is 4.7 MB and includes
its native helper, shared v2 schema, JavaScript/CSS and third-party notices.

Reference release 0.2.0 (2026-09-11): the eighth standard cw/2 block supports local files,
published URL fallback, target step IDs and a return stack in VS Code. The local web reader
shares its server across referenced documents, reuses cyclic destinations and keeps each
document's CLI-accessible comments separate. Published browser links retain the original tab.
Validation: 17 core tests, 10 browser tests (including real Go-served reference navigation),
Go tests/vet, TypeScript checking and extracted-VSIX native integration passed. The native
tests cover local and published references, exact target steps, return position and missing
files. Browser checks cover local navigation, reload, return, missing steps, URL fallback and
CLI access to the linked document's comments. Reference card and return screenshots inspected.
