export const richFixture = {
  version: 'cw/2', title: 'Interactive walkthrough', language: 'typescript',
  source: { repositoryUrl: 'https://github.com/example/project', revision: 'head' },
  parts: [{ id: 'chapter', title: 'The whole reader', sections: [{ id: 'features', title: 'Interactive content', steps: [{
    id: 'rich', title: 'Trace a request through the code', blocks: [
      { type: 'markdown', text: 'Follow the **request** from the diagram to the highlighted code.\n\n```typescript\nconst answer = 42;\n```' },
      { type: 'callout', severity: 'tip', text: 'Select a diagram node or an annotated code line.' },
      { type: 'code', id: 'handler', snippet: { text: 'export const a = 1;\nexport const b = 2;\n', source: { file: 'src/a.ts', startLine: 2 }, annotations: [{ lines: { start: 2 }, text: '**The second value** is used by the caller.' }], highlights: [{ lines: { start: 2 } }] } },
      { type: 'diagram', id: 'flow', format: 'mermaid', text: 'flowchart LR\n  request[Request] --> handler[Handler]\n  handler --> response[Response]', alt: 'A request passes through the handler to produce a response.', links: [{ nodeId: 'handler', blockId: 'handler' }] },
      { type: 'timeline', id: 'execution', nodes: [{ id: 'client', label: 'Client' }, { id: 'service', label: 'Service' }], frames: [
        { label: 'Waiting', note: 'The **client** is ready.', durationMs: 120, states: [{ nodeId: 'client', state: 'active', detail: 'Send request' }, { nodeId: 'service', state: 'idle' }] },
        { label: 'Processing', note: 'The service handles the request.', durationMs: 800, states: [{ nodeId: 'client', state: 'idle' }, { nodeId: 'service', state: 'active', detail: 'Handle request' }] },
        { label: 'Complete', note: 'Both are done.', durationMs: 800, states: [{ nodeId: 'client', state: 'done' }, { nodeId: 'service', state: 'done' }] },
      ] },
      { type: 'diff', before: { file: 'src/a.ts' }, after: { file: 'src/a.ts' }, hunks: [{ oldStart: 2, oldLines: 2, newStart: 2, newLines: 2, lines: [{ kind: 'context', text: 'export const a = 1;' }, { kind: 'delete', text: 'export const b = 1;' }, { kind: 'add', text: 'export const b = 2;' }] }] },
      { type: 'extension', name: 'example/extra', version: 1, data: {}, fallback: 'This explanation is available in every reader.' },
    ],
  }] }] }],
};
