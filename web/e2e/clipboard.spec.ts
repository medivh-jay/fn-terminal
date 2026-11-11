import { test, expect, type Page } from '@playwright/test';

const pane = (page: Page) => page.locator('.tab-panel:not([hidden]) .terminal-pane').first();

// Reads what actually reached the system clipboard, independent of how the page wrote it.
const systemClipboard = (page: Page) =>
  page.evaluate(() => (window as any).realClipboard.readText() as Promise<string>);

async function selectMarker(page: Page, marker: string): Promise<void> {
  await pane(page).locator('.xterm-helper-textarea').focus();
  await page.keyboard.type(`printf '\\n${marker.slice(0, 5)}'; printf '${marker.slice(5)}\\n'`);
  await page.keyboard.press('Enter');
  const row = pane(page)
    .locator('.xterm-rows > div')
    .filter({ hasText: new RegExp(`^${marker}\\s*$`) });
  await expect(row).toBeVisible();
  // xterm's screen layer sits above the rows, so select through raw mouse input.
  const box = (await row.boundingBox())!;
  await page.mouse.dblclick(box.x + 20, box.y + box.height / 2);
}

test.beforeEach(async ({ context, baseURL }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write'], { origin: baseURL });
});

test.describe('plain HTTP (no async Clipboard API)', () => {
  test.beforeEach(async ({ page }) => {
    // Loopback is a secure context, so remove the API exactly as a LAN HTTP origin would.
    await page.addInitScript(() => {
      (window as any).realClipboard = navigator.clipboard;
      Object.defineProperty(Navigator.prototype, 'clipboard', {
        configurable: true,
        get: () => undefined,
      });
    });
    await page.goto('/app/fnterminal/');
    await expect(page.locator('#connection-text')).toHaveText('已连接');
    await page.evaluate(() => (window as any).realClipboard.writeText('BEFORE'));
  });

  test('toolbar copy writes the selection to the clipboard', async ({ page }) => {
    await selectMarker(page, 'HTTP_TOOLBAR');
    await page.getByRole('button', { name: '复制', exact: true }).click();
    await expect(page.locator('#notice')).toHaveText('已复制选中文字。');
    expect((await systemClipboard(page)).trim()).toBe('HTTP_TOOLBAR');
    await expect(page.locator('.clipboard-buffer')).toHaveCount(0);
    // Keyboard paste needs no Clipboard API, so the copied text round-trips over HTTP.
    await pane(page).locator('.xterm-helper-textarea').focus();
    await page.keyboard.type('echo PASTED_');
    await page.keyboard.press('Control+Shift+V');
    await expect(pane(page).locator('.xterm-rows')).toContainText('echo PASTED_HTTP_TOOLBAR');
  });

  test('context menu copy writes the selection and returns focus', async ({ page }) => {
    await selectMarker(page, 'HTTP_MENU');
    await pane(page).click({ button: 'right', position: { x: 30, y: 30 } });
    await page.getByRole('menuitem', { name: '复制', exact: true }).click();
    await expect(page.locator('#notice')).toHaveText('已复制选中文字。');
    expect((await systemClipboard(page)).trim()).toBe('HTTP_MENU');
    await expect(pane(page).locator('.xterm-helper-textarea')).toBeFocused();
  });

  test('paste button explains the keyboard shortcut without asking for HTTPS', async ({ page }) => {
    await page.getByRole('button', { name: '粘贴', exact: true }).click();
    await expect(page.locator('#notice')).toContainText('Ctrl+Shift+V');
    await expect(page.locator('#notice')).not.toContainText('HTTPS');
    await expect(pane(page).locator('.xterm-helper-textarea')).toBeFocused();
  });
});

test('copy falls back when a secure context rejects the Clipboard API', async ({ page }) => {
  await page.addInitScript(() => {
    (window as any).realClipboard = navigator.clipboard;
    Object.defineProperty(Navigator.prototype, 'clipboard', {
      configurable: true,
      get: () => ({
        writeText: () => Promise.reject(new DOMException('blocked', 'NotAllowedError')),
      }),
    });
  });
  await page.goto('/app/fnterminal/');
  await expect(page.locator('#connection-text')).toHaveText('已连接');
  await selectMarker(page, 'DENIED_COPY');
  await page.getByRole('button', { name: '复制', exact: true }).click();
  await expect(page.locator('#notice')).toHaveText('已复制选中文字。');
  expect((await systemClipboard(page)).trim()).toBe('DENIED_COPY');
});

test.describe('Ctrl+Shift+C copy shortcut', () => {
  test.beforeEach(async ({ page }) => {
    await page.addInitScript(() => {
      (window as any).realClipboard = navigator.clipboard;
      Object.defineProperty(Navigator.prototype, 'clipboard', {
        configurable: true,
        get: () => undefined,
      });
      // Read after dispatch whether anything cancelled the browser's default action.
      document.addEventListener(
        'keydown',
        (event) => {
          if (event.code === 'KeyC' && event.ctrlKey && event.shiftKey)
            setTimeout(() => ((window as any).copyKeyPrevented = event.defaultPrevented));
        },
        true,
      );
    });
  });

  test('copies over HTTP without sending the key to the shell', async ({ page }) => {
    await page.goto('/app/fnterminal/');
    await expect(page.locator('#connection-text')).toHaveText('已连接');
    await page.evaluate(() => (window as any).realClipboard.writeText('BEFORE'));
    await selectMarker(page, 'KEY_COPY');
    // Leave a half-typed command so a leaked ^C or "C" would change what runs.
    await pane(page).locator('.xterm-helper-textarea').focus();
    await page.keyboard.type("printf '%s\\n' KEEP_");
    const row = pane(page)
      .locator('.xterm-rows > div')
      .filter({ hasText: /^KEY_COPY\s*$/ });
    const box = (await row.boundingBox())!;
    await page.mouse.dblclick(box.x + 20, box.y + box.height / 2);
    await page.keyboard.press('Control+Shift+C');
    await expect(page.locator('#notice')).toHaveText('已复制选中文字。');
    expect((await systemClipboard(page)).trim()).toBe('KEY_COPY');
    await expect.poll(() => page.evaluate(() => (window as any).copyKeyPrevented)).toBe(true);
    await page.keyboard.type('LINE');
    await page.keyboard.press('Enter');
    await expect(
      pane(page)
        .locator('.xterm-rows > div')
        .filter({ hasText: /^KEEP_LINE\s*$/ }),
    ).toBeVisible();
  });

  test('without a selection only explains what to do', async ({ page }) => {
    await page.goto('/app/fnterminal/');
    await expect(page.locator('#connection-text')).toHaveText('已连接');
    await pane(page).locator('.xterm-helper-textarea').focus();
    await page.keyboard.press('Control+Shift+C');
    await expect(page.locator('#notice')).toHaveText('先在终端中选中要复制的文字。');
    await expect.poll(() => page.evaluate(() => (window as any).copyKeyPrevented)).toBe(true);
  });

  test('is left to the browser on macOS', async ({ page }) => {
    await page.addInitScript(() =>
      Object.defineProperty(Navigator.prototype, 'platform', {
        configurable: true,
        get: () => 'MacIntel',
      }),
    );
    await page.goto('/app/fnterminal/');
    await expect(page.locator('#connection-text')).toHaveText('已连接');
    await pane(page).locator('.xterm-helper-textarea').focus();
    await page.keyboard.press('Control+Shift+C');
    await expect.poll(() => page.evaluate(() => (window as any).copyKeyPrevented)).toBe(false);
    await expect(page.locator('#notice')).toBeHidden();
  });
});
