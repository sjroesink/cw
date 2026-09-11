import { test, expect } from '@playwright/test';
test.beforeEach(async ({ page }) => {
  await page.goto('/'); await expect(page.locator('body')).toHaveAttribute('data-ready', 'true');
});
test('diagram SVG, keyboard code link, source and full-size zoom work offline', async ({ page }) => {
  const diagram = page.locator('[data-diagram]');
  await expect(diagram).toHaveAttribute('data-rendered', 'true');
  await expect(diagram.locator('svg')).toBeVisible();
  const node = diagram.getByRole('button', { name: 'Show code: handler' });
  await node.focus(); await page.keyboard.press('Enter');
  expect(await page.evaluate(() => (window as any).messages.some((m: any) => m.action === 'block' && m.value === 'handler'))).toBe(true);
  await diagram.locator('summary').click();
  await expect(diagram.locator('.diagram-source pre')).toBeVisible();
  await diagram.getByRole('button', { name: 'Full size' }).click();
  await expect(page.getByRole('dialog')).toBeVisible();
  const before = await page.locator('.zoom-percent').textContent();
  await page.getByRole('button', { name: 'Zoom in' }).click();
  expect(await page.locator('.zoom-percent').textContent()).not.toBe(before);
  await page.keyboard.press('Escape');
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect(diagram.locator('svg')).toBeVisible();
  await page.screenshot({ path: '.vscode-test/browser-results/reader.png', fullPage: true });
});
test('timeline preserves full snapshots, supports frame selection, playback and resume', async ({ page }) => {
  const timeline = page.locator('[data-timeline]');
  await expect(timeline).toHaveAttribute('data-frame-index', '0');
  await timeline.getByRole('button', { name: 'Frame 3: Complete', exact: true }).click();
  await expect(timeline.locator('.timeline-node[data-state="done"]')).toHaveCount(2);
  await page.reload(); await expect(page.locator('body')).toHaveAttribute('data-ready', 'true');
  await expect(timeline).toHaveAttribute('data-frame-index', '2');
  await timeline.getByRole('button', { name: 'Frame 1: Waiting', exact: true }).click();
  await timeline.getByRole('button', { name: 'Play timeline', exact: true }).click();
  await expect(timeline).toHaveAttribute('data-frame-index', '1');
  await timeline.getByRole('button', { name: 'Pause timeline', exact: true }).click();
  const paused = await timeline.getAttribute('data-frame-index');
  await page.waitForTimeout(1000);
  await expect(timeline).toHaveAttribute('data-frame-index', paused!);
});
test('syntax tokens, annotations, source line navigation, clipboard and diff gutters', async ({ page }) => {
  const snippet = page.locator('.snippet');
  await expect(snippet.locator('.hljs-keyword').first()).toBeVisible();
  const note = page.locator('[data-annotation]');
  await expect(note).toBeHidden();
  await snippet.getByRole('button', { name: 'Read annotation 1' }).click();
  await expect(note).toBeVisible();
  await expect(note).toContainText('The second value');
  await snippet.getByRole('button', { name: '3', exact: true }).click();
  await page.getByRole('button', { name: 'Copy code', exact: true }).click();
  const messages = await page.evaluate(() => (window as any).messages);
  expect(messages).toEqual(expect.arrayContaining([expect.objectContaining({ action: 'line', value: '2:2' }), expect.objectContaining({ action: 'copy', value: '2' })]));
  const old = await page.locator('.diff .old').allTextContents();
  const current = await page.locator('.diff .new').allTextContents();
  expect(old).toEqual(['2', '3', '']); expect(current).toEqual(['2', '', '3']);
});
test('malformed diagrams retain source and description', async ({ page }) => {
  await page.goto('/?invalid'); await expect(page.locator('body')).toHaveAttribute('data-ready', 'true');
  await expect(page.locator('.diagram-status')).toContainText('could not be drawn');
  await expect(page.locator('.diagram-source pre')).toBeVisible();
  await expect(page.locator('[data-zoom]')).toBeDisabled();
});
test('hostile diagram markup cannot execute scripts or load external media', async ({ page }) => {
  let external = 0, dialogs = 0;
  page.on('request', request => { if (!request.url().startsWith('http://127.0.0.1:4187/')) external++; });
  page.on('dialog', dialog => { dialogs++; void dialog.dismiss(); });
  await page.goto('/?hostile'); await expect(page.locator('body')).toHaveAttribute('data-ready', 'true');
  await expect(page.locator('.diagram-canvas image, .diagram-canvas script, .diagram-canvas iframe, .diagram-canvas foreignObject, .diagram-canvas a')).toHaveCount(0);
  expect(external).toBe(0); expect(dialogs).toBe(0);
});
test('overview and part pages expose the full contents with navigation', async ({ page }) => {
  await page.goto('/?view=overview'); await expect(page.locator('body')).toHaveAttribute('data-ready', 'true');
  await expect(page.locator('h1')).toHaveText('Interactive walkthrough');
  await page.getByRole('button', { name: 'The whole reader', exact: true }).click();
  expect(await page.evaluate(() => (window as any).messages.some((m: any) => m.action === 'part' && m.value === 'chapter'))).toBe(true);
  await page.goto('/?view=part'); await expect(page.locator('body')).toHaveAttribute('data-ready', 'true');
  await expect(page.locator('h1')).toHaveText('The whole reader');
  await page.getByRole('button', { name: 'Trace a request through the code', exact: true }).click();
  expect(await page.evaluate(() => (window as any).messages.some((m: any) => m.action === 'step' && m.value === 'chapter/features/rich'))).toBe(true);
});

test('reader theme overrides host colors and keyboard shortcuts dispatch without stealing control keys', async ({ page }) => {
  await page.goto('/?theme=light'); await expect(page.locator('body')).toHaveAttribute('data-ready', 'true');
  await expect(page.locator('body')).toHaveClass('vscode-light');
  await expect(page.locator('body')).toHaveCSS('background-color', 'rgb(250, 250, 250)');
  await page.locator('h1').click();
  for (const key of ['Escape', 'o', 's', 't']) await page.keyboard.press(key);
  expect(await page.evaluate(() => (window as any).messages.map((m: any) => m.action))).toEqual(expect.arrayContaining(['up', 'openCode', 'settings', 'theme']));
  await page.getByRole('button', { name: 'Copy page link', exact: true }).click();
  const count = await page.evaluate(() => (window as any).messages.length);
  await page.keyboard.press('ArrowRight');
  expect(await page.evaluate(() => (window as any).messages.length)).toBe(count);
});

test('comments preserve selected code anchors and render interactive agent replies', async ({ page }) => {
  await page.goto('/?comments'); await expect(page.locator('body')).toHaveAttribute('data-ready', 'true');
  const comments = page.getByRole('region', { name: 'Comments', exact: true });
  await expect(comments).toContainText('An agent is watching');
  await expect(comments.locator('[data-thread="2"]')).toBeHidden();
  await comments.getByLabel('Show archived').check();
  await expect(comments.locator('[data-thread="2"]')).toBeVisible();
  await expect(comments.locator('.diagram-canvas svg')).toHaveCount(1);
  await expect(comments.locator('.timeline')).toHaveAttribute('data-frame-index', '0');
  await comments.locator('.diagram-canvas .code-link').click();
  expect(await page.evaluate(() => (window as any).messages.at(-1))).toMatchObject({ action: 'block', value: 'comment-1-1-handler' });
  await page.locator('article .snippet .code-text').first().evaluate(element => {
    const range = document.createRange(); range.selectNodeContents(element);
    const selection = window.getSelection()!; selection.removeAllRanges(); selection.addRange(range);
  });
  await page.waitForTimeout(50);
  await comments.getByRole('button', { name: 'Comment on selection', exact: true }).click();
  expect(await page.evaluate(() => (window as any).messages.at(-1))).toMatchObject({ action: 'commentSelection', value: { block: 2, first: 1, last: 1, quote: 'export const a = 1;' } });
  await comments.locator('[data-thread="1"]').getByRole('button', { name: 'Archive', exact: true }).click();
  expect(await page.evaluate(() => (window as any).messages.at(-1))).toMatchObject({ action: 'commentArchive', value: '1' });
  await comments.locator('h2').scrollIntoViewIfNeeded();
  await page.screenshot({ path: '.vscode-test/browser-results/comments.png' });
});

test('automatic appearance preserves high contrast and the comments keyboard shortcut', async ({ page }) => {
  await page.goto('/?theme=contrast'); await expect(page.locator('body')).toHaveAttribute('data-ready', 'true');
  await expect(page.locator('body')).toHaveClass('vscode-high-contrast');
  await page.locator('h1').click(); await page.keyboard.press('c');
  expect(await page.evaluate(() => (window as any).messages.at(-1))).toMatchObject({ action: 'comments' });
  await page.screenshot({ path: '.vscode-test/browser-results/contrast.png' });
});
