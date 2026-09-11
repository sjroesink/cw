import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { body, html, Reading } from '../src/render';
import { parseWalkthrough, flatten } from '../src/model';
import { richFixture } from './rich-fixture';

const theme = `<style>:root{--vscode-foreground:#d4d4d4;--vscode-sideBar-background:#252526;--vscode-editor-background:#1e1e1e;--vscode-panel-border:#555;--vscode-font-family:system-ui;--vscode-font-size:14px;--vscode-editor-font-family:Consolas,monospace;--vscode-descriptionForeground:#aaa;--vscode-button-background:#0e639c;--vscode-button-foreground:white;--vscode-button-secondaryBackground:#3a3d41;--vscode-button-secondaryForeground:#fff;--vscode-focusBorder:#007fd4;--vscode-textLink-foreground:#4daafc;--vscode-textCodeBlock-background:#1e1e1e;--vscode-textBlockQuote-background:#303033;--vscode-editor-findMatchHighlightBackground:#665511;--vscode-diffEditor-insertedTextBackground:#164922;--vscode-diffEditor-removedTextBackground:#652121;--vscode-editorWarning-foreground:#cca700;--vscode-progressBar-background:#0078d4}</style>`;
const mock = `<script nonce="browser-test">window.messages=[];window.acquireVsCodeApi=()=>({postMessage:m=>window.messages.push(m),getState:()=>JSON.parse(sessionStorage.getItem('cw-ui')||'null'),setState:s=>sessionStorage.setItem('cw-ui',JSON.stringify(s))});</script>`;
const server = createServer(async (req, res) => {
  try {
    const url = new URL(req.url ?? '/', 'http://localhost');
    if (url.pathname === '/sidebar.js' || url.pathname === '/sidebar.css') {
      res.setHeader('Content-Type', url.pathname.endsWith('.js') ? 'application/javascript' : 'text/css');
      res.end(await readFile(url.pathname.endsWith('.js') ? 'dist/webview/sidebar.js' : 'media/sidebar.css')); return;
    }
    const raw: any = structuredClone(richFixture);
    if (url.searchParams.has('invalid')) raw.parts[0].sections[0].steps[0].blocks[3].text = 'this is not mermaid';
    if (url.searchParams.has('hostile')) {
      raw.parts[0].sections[0].steps[0].blocks[3].text = 'flowchart LR\nA["<img src=https://example.invalid/exfil onerror=alert(1)>"] --> B[Safe]\nclick B "javascript:alert(1)"';
    }
    const doc = parseWalkthrough(JSON.stringify(raw));
    const view = url.searchParams.get('view');
    const reading: Reading = { doc, entries: flatten(doc), index: 0, codeIndex: 0, done: new Set(), identity: 'browser-fixture', view: view === 'overview' || view === 'part' ? view : 'step', partId: 'chapter' };
    if (url.searchParams.has('comments')) {
      reading.commentsOpen = true;
      reading.comments = { rev: 1, watching: true, prompt: 'Watch these comments', threads: [
        { id: '1', at: '2026-09-11T08:00:00Z', status: 'answered', where: { step: 'chapter/features/rich', title: 'Request handler', quote: 'Why does the handler do this?' }, messages: [
          { from: 'reader', at: '2026-09-11T08:00:00Z', text: 'Explain the request.' },
          { from: 'agent', at: '2026-09-11T08:01:00Z', blocks: doc.parts[0].sections[0].steps[0].blocks },
        ] },
        { id: '2', at: '2026-09-11T08:00:00Z', archived: true, status: 'open', where: { step: '', title: 'Overview' }, messages: [{ from: 'reader', at: '2026-09-11T08:00:00Z', text: 'Old question' }] },
      ] };
    }
    let output = html(body(reading), '/sidebar.css', '/sidebar.js', "'self'", 'browser-test', 'test-token');
    output = output.replace('</head>', theme + mock + '</head>').replace('<body ', '<body class="vscode-dark" ');
    if (url.searchParams.get('theme') === 'light') output = output.replace('data-theme="auto"', 'data-theme="light"');
    if (url.searchParams.get('theme') === 'contrast') output = output.replace('class="vscode-dark"', 'class="vscode-high-contrast"');
    res.setHeader('Content-Type', 'text/html'); res.end(output);
  } catch (error) { res.statusCode = 500; res.end(String(error)); }
});
server.listen(Number(process.env.CW_BROWSER_PORT ?? 4187), '127.0.0.1');
