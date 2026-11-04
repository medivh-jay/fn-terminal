import { defineConfig } from '@playwright/test';
export default defineConfig({
  testDir: './e2e',
  timeout: 20000,
  fullyParallel: false,
  workers: 1,
  use: {
    baseURL: 'http://127.0.0.1:18088',
    viewport: { width: 1365, height: 900 },
    headless: true,
  },
  outputDir: '../build/browser-test-results',
  webServer: [
    {
      command: '../build/fn-terminal-linux-amd64 dev --listen 127.0.0.1:18088',
      url: 'http://127.0.0.1:18088/app/fnterminal/api/me',
      reuseExistingServer: false,
      timeout: 10000,
    },
    {
      command: 'node e2e/gateway-proxy.mjs',
      url: 'http://127.0.0.1:18089/foreign-page',
      reuseExistingServer: false,
      timeout: 10000,
    },
  ],
});
