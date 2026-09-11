export function fixture() {
  return {
    version: 'cw/2', title: 'A tour in the editor', summary: 'Read the **local source** alongside the explanation.',
    parts: [{ id: 'part', title: 'A part', sections: [{ id: 'section', title: 'A section', steps: [
      { id: 'first', title: 'Two code locations', blocks: [
        { type: 'markdown', text: 'The first location has an annotation. The second has moved.' },
        { id: 'code-a', type: 'code', snippet: { text: 'export const a = 1;\nexport const b = 2;\n', source: { file: 'src/a.ts', startLine: 2, endLine: 3 }, highlights: [{ lines: { start: 2 } }], annotations: [{ lines: { start: 2 }, text: 'This is **b**.' }] } },
        { id: 'code-b', type: 'code', snippet: { text: 'export const moved = true;\n', source: { file: 'src/b.ts', startLine: 1 } } },
      ] },
      { id: 'second', title: 'A missing file', blocks: [
        { type: 'code', snippet: { text: 'missing\n', source: { file: 'missing.ts', startLine: 1 } } },
      ] },
      { id: 'third', title: 'An explanation without a location', blocks: [
        { type: 'markdown', text: 'A prose-only step still belongs in the tour.' },
        { type: 'extension', name: 'example/custom', version: 1, data: {}, fallback: 'Readable fallback.' },
      ] },
    ] }] }],
  };
}
