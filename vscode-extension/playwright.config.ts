import { defineConfig } from '@playwright/test';
export default defineConfig({
  testDir: './test/browser', fullyParallel: false, workers: 1,
  reporter: 'list', outputDir: '.vscode-test/browser-results',
  use: { baseURL: 'http://127.0.0.1:4187', viewport: { width: 440, height: 950 }, reducedMotion: 'reduce', screenshot: 'only-on-failure' },
  webServer: { command: 'node dist/browser-server.js', port: 4187, reuseExistingServer: false },
});
