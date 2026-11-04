import { test, expect } from '@playwright/test';

test('real shell, Chinese output, manual reconnect and clear', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await page.goto('/app/fnterminal/');
  await expect(page.locator('#connection-text')).toHaveText('已连接');
  await expect(page.getByRole('tab', { selected: true })).toContainText('Shell');
  await page.locator('.xterm-helper-textarea').focus();
  await page.keyboard.type("printf '\\nE2E_'; printf '中文_OK\\n'");
  await page.keyboard.press('Enter');
  await expect(page.locator('.xterm-rows')).toContainText('E2E_中文_OK');
  await page.screenshot({ path: '../build/terminal-desktop.png', fullPage: true });
  await page.getByRole('button', { name: '重新连接', exact: false }).click();
  await expect(page.locator('#connection-text')).toHaveText('已连接');
  await expect(page.locator('.xterm-rows')).not.toContainText('E2E_中文_OK');
  await page.getByRole('button', { name: '清屏', exact: true }).click();
  expect(errors).toEqual([]);
});

test('narrow screen keeps one compact toolbar without a footer', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/app/fnterminal/');
  await expect(page.locator('#connection-text')).toHaveText('已连接');
  await expect(page.getByRole('button', { name: '新建终端', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: '清屏', exact: true })).toBeVisible();
  const layout = await page.evaluate(() => {
    const bar = document.querySelector('.terminal-topline')!.getBoundingClientRect();
    const terminal = document.querySelector('#terminal-deck')!.getBoundingClientRect();
    return {
      barHeight: bar.height,
      top: bar.top,
      left: terminal.left,
      bottom: terminal.bottom,
      viewport: window.innerHeight,
    };
  });
  expect(layout.barHeight).toBe(30);
  expect(layout.top).toBe(0);
  expect(layout.left).toBe(0);
  expect(layout.bottom).toBe(layout.viewport);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  );
  await page.screenshot({ path: '../build/terminal-mobile.png', fullPage: true });
});

test('tabs keep independent shells and output while inactive', async ({ page }) => {
  await page.goto('/app/fnterminal/');
  await expect(page.locator('#connection-text')).toHaveText('已连接');
  const first = page.getByRole('tab').first();
  await page.locator('.terminal-session:not([hidden]) .xterm-helper-textarea').focus();
  await page.keyboard.type(
    "TAB_VALUE=FIRST; printf '\\nFIRST_'; printf 'SESSION\\n'; (sleep 0.3; printf '\\nBACKGROUND_'; printf 'DONE\\n') &",
  );
  await page.keyboard.press('Enter');
  await expect(page.locator('.terminal-session:not([hidden]) .xterm-rows')).toContainText(
    'FIRST_SESSION',
  );
  await page.getByRole('button', { name: '新建终端', exact: true }).click();
  await expect(page.getByRole('tab')).toHaveCount(2);
  await expect(page.locator('#connection-text')).toHaveText('已连接');
  await page.locator('.terminal-session:not([hidden]) .xterm-helper-textarea').focus();
  await page.keyboard.type('printf \'\\nSECOND_%s\\n\' "${TAB_VALUE:-EMPTY}"');
  await page.keyboard.press('Enter');
  await expect(page.locator('.terminal-session:not([hidden]) .xterm-rows')).toContainText(
    'SECOND_EMPTY',
  );
  await expect(page.locator('.terminal-session:not([hidden]) .xterm-rows')).not.toContainText(
    'FIRST_SESSION',
  );
  await first.click();
  await expect(page.locator('.terminal-session:not([hidden]) .xterm-rows')).toContainText(
    'BACKGROUND_DONE',
  );
  await page.locator('.terminal-session:not([hidden]) .xterm-helper-textarea').focus();
  await page.keyboard.type('printf \'\\nKEPT_%s\\n\' "$TAB_VALUE"');
  await page.keyboard.press('Enter');
  await expect(page.locator('.terminal-session:not([hidden]) .xterm-rows')).toContainText(
    'KEPT_FIRST',
  );
  await page.getByRole('button', { name: '关闭终端 2', exact: true }).click();
  await expect(page.getByRole('tab')).toHaveCount(1);
  await page.getByRole('button', { name: '关闭终端 1', exact: true }).click();
  await expect(page.getByRole('tab')).toHaveCount(0);
  await expect(page.getByText('点击 + 新建终端', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: '新建终端', exact: true }).click();
  await expect(page.getByRole('tab')).toHaveCount(1);
  await expect(page.locator('#connection-text')).toHaveText('已连接');
});

test('reconnecting the active tab at capacity preserves other sessions', async ({ page }) => {
  await page.goto('/app/fnterminal/');
  for (let count = 1; count < 4; count++) {
    await expect(page.locator('#connection-text')).toHaveText('已连接');
    await page.getByRole('button', { name: '新建终端', exact: true }).click();
  }
  await expect(page.locator('#connection-text')).toHaveText('已连接');
  await expect(page.getByRole('tab')).toHaveCount(4);
  await expect(page.getByRole('button', { name: '新建终端', exact: true })).toBeDisabled();
  await page.getByRole('button', { name: '重新连接', exact: true }).click();
  await expect(page.locator('#connection-text')).toHaveText('已连接');
  await page.getByRole('tab').first().click();
  await expect(page.locator('#connection-text')).toHaveText('已连接');
  await page.screenshot({ path: '../build/terminal-tabs.png', fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole('tab').last().click();
  await expect(page.getByRole('button', { name: '新建终端', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: '清屏', exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.screenshot({ path: '../build/terminal-mobile-tabs.png', fullPage: true });
});

test('delayed paste stays with its original tab after switching', async ({ page }) => {
  await page.goto('/app/fnterminal/');
  await expect(page.locator('#connection-text')).toHaveText('已连接');
  await page.getByRole('button', { name: '新建终端', exact: true }).click();
  await expect(page.locator('#connection-text')).toHaveText('已连接');
  await page.getByRole('tab').first().click();
  await page.evaluate(() => {
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: {
        readText: () =>
          new Promise<string>((resolve) => {
            (window as any).finishClipboard = resolve;
          }),
      },
    });
  });
  await page.getByRole('button', { name: '粘贴', exact: true }).click();
  await page.getByRole('tab').nth(1).click();
  await page.evaluate(() => {
    (window as any).finishClipboard("printf '\\nPASTE_'; printf 'ORIGINAL\\n'\n");
  });
  await expect(page.locator('.terminal-session:not([hidden]) .xterm-rows')).not.toContainText(
    'PASTE_ORIGINAL',
  );
  await page.getByRole('tab').first().click();
  await expect(page.locator('.terminal-session:not([hidden]) .xterm-rows')).toContainText('PASTE_');
  await page.locator('.terminal-session:not([hidden]) .xterm-helper-textarea').focus();
  await page.keyboard.press('Enter'); // Bash bracketed paste deliberately waits for confirmation.
  await expect(page.locator('.terminal-session:not([hidden]) .xterm-rows')).toContainText(
    'PASTE_ORIGINAL',
  );
});

test('delayed clipboard read is cancelled if its shell reconnects', async ({ page }) => {
  await page.goto('/app/fnterminal/');
  await expect(page.locator('#connection-text')).toHaveText('已连接');
  await page.evaluate(() => {
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: {
        readText: () =>
          new Promise<string>((resolve) => {
            (window as any).finishClipboard = resolve;
          }),
      },
    });
  });
  await page.getByRole('button', { name: '粘贴', exact: true }).click();
  await page.getByRole('button', { name: '重新连接', exact: true }).click();
  await expect(page.locator('#connection-text')).toHaveText('已连接');
  await page.evaluate(() => {
    (window as any).finishClipboard('STALE_CLIPBOARD_COMMAND');
  });
  await expect(page.locator('#notice')).toContainText('再次粘贴');
  await expect(page.locator('.xterm-rows')).not.toContainText('STALE_CLIPBOARD_COMMAND');
});

test('inactive tab continues consuming output beyond the transport window', async ({ page }) => {
  let firstSocket = true;
  let acknowledgements = 0;
  page.on('websocket', (socket) => {
    if (!firstSocket) return;
    firstSocket = false;
    socket.on('framesent', (event) => {
      if (event.payload.toString() === '{"type":"ack"}') acknowledgements++;
    });
  });
  await page.goto('/app/fnterminal/');
  await expect(page.locator('#connection-text')).toHaveText('已连接');
  await page.locator('.xterm-helper-textarea').focus();
  await page.keyboard.type(
    "(sleep 0.4; i=0; while [ $i -lt 4000 ]; do printf '%080d\\n' $i; i=$((i+1)); done; printf '\\nDRAIN_'; printf 'DONE\\n') &",
  );
  await page.keyboard.press('Enter');
  await page.getByRole('button', { name: '新建终端', exact: true }).click();
  await expect(page.locator('#connection-text')).toHaveText('已连接');
  const baseline = acknowledgements;
  // More than eight rendered chunks proves the hidden session drains its window.
  await expect.poll(() => acknowledgements - baseline).toBeGreaterThan(8);
  await page.getByRole('tab').first().click();
  await expect(page.locator('.terminal-session:not([hidden]) .xterm-rows')).toContainText(
    'DRAIN_DONE',
  );
});

test('authentication failure is visible and does not show a fake prompt', async ({ page }) => {
  await page.route('**/api/me', (route) => route.fulfill({ status: 401, body: 'unauthorized' }));
  await page.goto('/app/fnterminal/');
  await expect(page.locator('#empty-message')).toHaveText('无法连接终端');
  await expect(page.locator('#empty-detail')).toContainText('已登录的飞牛桌面');
  await expect(page.locator('#connection-text')).toHaveText('连接失败');
});

test('connection rejected before ready remains a failure rather than an ended session', async ({
  page,
}) => {
  await page.routeWebSocket('**/app/fnterminal/ws', (socket) => {
    socket.close({ code: 1008, reason: '连接校验失败' });
  });
  await page.goto('/app/fnterminal/');
  await expect(page.locator('#connection-text')).toHaveText('连接失败');
  await expect(page.locator('#empty-message')).toHaveText('无法连接终端');
});

test('ticket request failure is visible before opening a WebSocket', async ({ page }) => {
  let sockets = 0;
  page.on('websocket', () => sockets++);
  await page.route('**/api/ws-ticket', (route) =>
    route.fulfill({ status: 503, body: 'unavailable' }),
  );
  await page.goto('/app/fnterminal/');
  await expect(page.locator('#connection-text')).toHaveText('连接失败');
  await expect(page.locator('#empty-detail')).toContainText('无法验证本次连接');
  expect(sockets).toBe(0);
});

test('gateway Host rewrite connects and reconnects without an origin list', async ({ page }) => {
  await page.goto('http://127.0.0.1:18089/app/fnterminal/');
  await expect(page.locator('#connection-text')).toHaveText('已连接');
  await page.locator('.xterm-helper-textarea').focus();
  await page.keyboard.type("printf '\\nGATEWAY_'; printf 'OK\\n'");
  await page.keyboard.press('Enter');
  await expect(page.locator('.xterm-rows')).toContainText('GATEWAY_OK');
  await page.getByRole('button', { name: '重新连接', exact: false }).click();
  await expect(page.locator('#connection-text')).toHaveText('已连接');
});

test('foreign browser page cannot obtain a ticket or open a terminal', async ({ page }) => {
  await page.goto('http://127.0.0.1:18089/foreign-page');
  const result = await page.evaluate(async () => {
    const destination = 'http://127.0.0.1:18088/app/fnterminal';
    let readable = false;
    try {
      await fetch(`${destination}/api/ws-ticket`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json', 'X-Fnterminal-Request': '1' },
        body: JSON.stringify({ origin: location.origin }),
      });
      readable = true;
    } catch {
      /* Preflight must fail. */
    }
    const connected = await new Promise<boolean>((resolve) => {
      const ws = new WebSocket(destination.replace('http:', 'ws:') + '/ws');
      ws.onopen = () => {
        ws.close();
        resolve(true);
      };
      ws.onerror = () => resolve(false);
    });
    return { readable, connected };
  });
  expect(result).toEqual({ readable: false, connected: false });
});

test('theme changes recolor existing and new tabs without replacing their shells', async ({
  page,
}) => {
  let sockets = 0;
  page.on('websocket', () => sockets++);
  await page.goto('/app/fnterminal/');
  await expect(page.locator('#connection-text')).toHaveText('已连接');
  await page.locator('.xterm-helper-textarea').focus();
  await page.keyboard.type('THEME_SESSION=KEPT');
  await page.keyboard.press('Enter');
  await page.getByRole('button', { name: '新建终端', exact: true }).click();
  await expect(page.locator('#connection-text')).toHaveText('已连接');
  await page.getByRole('combobox', { name: '主题', exact: true }).selectOption('light');
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  await expect(page.locator('.terminal-topline')).toHaveCSS(
    'background-color',
    'rgb(234, 238, 242)',
  );
  await expect(page.locator('.xterm-viewport').first()).toHaveCSS(
    'background-color',
    'rgb(246, 248, 250)',
  );
  await expect(page.locator('.xterm-viewport').last()).toHaveCSS(
    'background-color',
    'rgb(246, 248, 250)',
  );
  await page.getByRole('tab').first().click();
  await page.locator('.terminal-session:not([hidden]) .xterm-helper-textarea').focus();
  await page.keyboard.type('printf \'\\nTHEME_%s\\n\' "$THEME_SESSION"');
  await page.keyboard.press('Enter');
  await expect(page.locator('.terminal-session:not([hidden]) .xterm-rows')).toContainText(
    'THEME_KEPT',
  );
  expect(sockets).toBe(2);
  const inset = await page.evaluate(
    () =>
      document
        .querySelector('.terminal-session:not([hidden]) .xterm-screen')!
        .getBoundingClientRect().left -
      document.querySelector('#terminal-deck')!.getBoundingClientRect().left,
  );
  expect(inset).toBe(8);
  await page.screenshot({ path: '../build/terminal-theme-light.png', fullPage: true });
  await page.getByRole('button', { name: '新建终端', exact: true }).click();
  await expect(page.locator('#connection-text')).toHaveText('已连接');
  await expect(page.locator('.xterm-viewport').last()).toHaveCSS(
    'background-color',
    'rgb(246, 248, 250)',
  );
  await page.getByRole('combobox', { name: '主题', exact: true }).selectOption('midnight');
  await page.screenshot({ path: '../build/terminal-theme-midnight.png', fullPage: true });
  await page.reload();
  await expect(page.locator('#connection-text')).toHaveText('已连接');
  await expect(page.getByRole('combobox', { name: '主题', exact: true })).toHaveValue('midnight');
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'midnight');
});

test('themes remain usable when browser preference storage is unavailable', async ({ page }) => {
  await page.addInitScript(() =>
    Object.defineProperty(window, 'localStorage', {
      get: () => {
        throw new DOMException('blocked', 'SecurityError');
      },
    }),
  );
  await page.goto('/app/fnterminal/');
  await expect(page.locator('#connection-text')).toHaveText('已连接');
  await expect(page.getByRole('combobox', { name: '主题', exact: true })).toHaveValue('pine');
  await page.getByRole('combobox', { name: '主题', exact: true }).selectOption('graphite');
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'graphite');
  await expect(page.locator('#connection-text')).toHaveText('已连接');
});

test('an invalid stored theme falls back to a usable palette', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('fnterminal.theme', 'constructor'));
  await page.goto('/app/fnterminal/');
  await expect(page.locator('#connection-text')).toHaveText('已连接');
  await expect(page.getByRole('combobox', { name: '主题', exact: true })).toHaveValue('pine');
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'pine');
});
