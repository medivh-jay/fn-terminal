import { test, expect, type Page, type Locator } from '@playwright/test';

const panes = (page: Page) => page.locator('.tab-panel:not([hidden]) .terminal-pane');
async function command(page: Page, pane: Locator, text: string): Promise<void> {
  await pane.locator('.xterm-helper-textarea').focus();
  await page.keyboard.type(text);
  await page.keyboard.press('Enter');
}
async function connected(page: Page): Promise<void> {
  await expect(page.locator('#connection-text')).toHaveText('已连接');
}

test('parenthesis shortcuts split the focused pane without sending input and respect quota', async ({
  page,
}) => {
  const inputs: string[] = [];
  page.on('websocket', (socket) =>
    socket.on('framesent', ({ payload }) => {
      if (typeof payload !== 'string') inputs.push(payload.toString());
    }),
  );
  await page.goto('/app/fnterminal/');
  await connected(page);
  await panes(page).first().locator('.xterm-helper-textarea').focus();
  await page.keyboard.press('Control+Shift+Digit9');
  await expect(panes(page)).toHaveCount(2);
  await connected(page);
  const left = (await panes(page).first().boundingBox())!;
  const right = (await panes(page).nth(1).boundingBox())!;
  expect(right.x).toBeGreaterThan(left.x + left.width - 2);
  expect(right.y).toBe(left.y);
  await page.keyboard.press('Control+Shift+Digit0');
  await expect(panes(page)).toHaveCount(3);
  await connected(page);
  const bottom = (await panes(page).nth(2).boundingBox())!;
  expect(bottom.x).toBe(right.x);
  expect(bottom.y).toBeGreaterThan(right.y);
  await page.keyboard.press('Control+Shift+Digit9');
  await expect(panes(page)).toHaveCount(4);
  await connected(page);
  await page.keyboard.press('Control+Shift+Digit0');
  await expect(panes(page)).toHaveCount(4);
  expect(inputs).toEqual([]);
});

test('shell exit removes only its pane and collapses the split', async ({ page }) => {
  await page.goto('/app/fnterminal/');
  await connected(page);
  await panes(page)
    .first()
    .click({ button: 'right', position: { x: 30, y: 60 } });
  await page.getByRole('menuitem', { name: '拆分视图', exact: true }).hover();
  await page.getByRole('menuitem', { name: '左右', exact: true }).click();
  await connected(page);
  await command(page, panes(page).nth(1), 'exit 7');
  await expect(panes(page)).toHaveCount(1);
  await expect(page.getByRole('separator')).toHaveCount(0);
  await command(page, panes(page).first(), "printf '\\nSTILL_'; printf 'ALIVE\\n'");
  await expect(panes(page).first().locator('.xterm-rows')).toContainText('STILL_ALIVE');
});

test('transport interruption keeps the pane available for reconnect', async ({ page }) => {
  let disconnect: (() => void) | undefined;
  await page.routeWebSocket('**/app/fnterminal/ws', (socket) => {
    socket.connectToServer();
    disconnect = () => socket.close({ code: 1011, reason: 'network interrupted' });
  });
  await page.goto('/app/fnterminal/');
  await connected(page);
  disconnect!();
  await expect(page.locator('#connection-text')).toHaveText('会话已结束');
  await expect(panes(page)).toHaveCount(1);
  await page.getByRole('button', { name: '重新连接', exact: true }).click();
  await connected(page);
});

test('last shell exit closes its script-opened app window', async ({ page }) => {
  await page.goto('/foreign-page');
  const popupPromise = page.waitForEvent('popup');
  await page.evaluate(() => window.open('/app/fnterminal/', '_blank'));
  const popup = await popupPromise;
  await connected(popup);
  const closed = popup.waitForEvent('close');
  await command(popup, panes(popup).first(), 'exit');
  await closed;
  expect(page.isClosed()).toBe(false);
});

test('last exit closes the fnOS host page through the real official SDK', async ({ page }) => {
  await page.route('**/test-host', (route) =>
    route.fulfill({
      contentType: 'text/html',
      body: `
    <!doctype html><title>fnOS test host</title>
    <script>
      window.closeCalls = [];
      addEventListener('message', event => {
        if (event.source !== document.querySelector('iframe')?.contentWindow || event.origin !== location.origin) return;
        const data = event.data;
        if (data?.penpal === 'syn') event.source.postMessage({penpal:'synAck', id:'test-host', appName:'fnterminal', methodNames:['close'], config:{}}, event.origin);
        if (data?.penpal === 'call' && data.methodName === 'close') {
          window.closeCalls.push({methodName:data.methodName,args:data.args});
          event.source.postMessage({penpal:'reply',id:data.id,resolution:'fulfilled',returnValue:null},event.origin);
          document.querySelector('iframe').remove();
        }
      });
    </script>
    <iframe title="Terminal" src="/app/fnterminal/" style="width:1100px;height:650px"></iframe>
  `,
    }),
  );
  await page.goto('/test-host');
  const terminal = page.frameLocator('iframe');
  await expect(terminal.locator('#connection-text')).toHaveText('已连接');
  await terminal.locator('.xterm-helper-textarea').focus();
  await page.keyboard.type('exit');
  await page.keyboard.press('Enter');
  await expect(page.locator('iframe')).toHaveCount(0);
  expect(await page.evaluate(() => (window as any).closeCalls)).toEqual([
    { methodName: 'close', args: [] },
  ]);
  expect(page.isClosed()).toBe(false);
});

test('exiting a background tab preserves the active shell and focus', async ({ page }) => {
  let release: (() => void) | undefined;
  await page.routeWebSocket('**/app/fnterminal/ws', (socket) => {
    const upstream = socket.connectToServer();
    upstream.onMessage((message) => {
      if (typeof message === 'string' && JSON.parse(message).type === 'exit')
        release = () => socket.send(message);
      else socket.send(message);
    });
    // Keep the browser channel open until the held exit control frame is delivered.
    upstream.onClose(() => {});
    socket.onMessage((message) => upstream.send(message));
  });
  await page.goto('/app/fnterminal/');
  await connected(page);
  await command(page, panes(page).first(), 'exit');
  await expect.poll(() => !!release).toBe(true);
  await page.getByRole('button', { name: '新建终端', exact: true }).click();
  await connected(page);
  await panes(page).first().locator('.xterm-helper-textarea').focus();
  release!();
  await expect(page.getByRole('tab')).toHaveCount(1);
  await expect(page.getByRole('tab', { selected: true })).toContainText('Shell 2');
  await expect(panes(page).first().locator('.xterm-helper-textarea')).toBeFocused();
});

test('an unfocused pane exiting does not steal toolbar focus', async ({ page }) => {
  let release: (() => void) | undefined;
  await page.routeWebSocket('**/app/fnterminal/ws', (socket) => {
    const upstream = socket.connectToServer();
    upstream.onMessage((message) => {
      if (typeof message === 'string' && JSON.parse(message).type === 'exit')
        release = () => socket.send(message);
      else socket.send(message);
    });
    upstream.onClose(() => {});
    socket.onMessage((message) => upstream.send(message));
  });
  await page.goto('/app/fnterminal/');
  await connected(page);
  await panes(page).first().locator('.xterm-helper-textarea').focus();
  await page.keyboard.press('Control+Shift+Digit9');
  await connected(page);
  await command(page, panes(page).first(), 'exit');
  await expect.poll(() => !!release).toBe(true);
  await panes(page).nth(1).locator('.xterm-helper-textarea').focus();
  await page.getByRole('combobox', { name: '主题', exact: true }).focus();
  release!();
  await expect(panes(page)).toHaveCount(1);
  await page.evaluate(
    () =>
      new Promise<void>((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
      ),
  );
  await expect(page.getByRole('combobox', { name: '主题', exact: true })).toBeFocused();
});

test('session end without a confirmed shell exit keeps the UI reconnectable', async ({ page }) => {
  let end: (() => void) | undefined;
  await page.routeWebSocket('**/app/fnterminal/ws', (socket) => {
    socket.connectToServer();
    end = () => {
      socket.send(JSON.stringify({ type: 'exit', message: '会话已结束', reason: 'session_end' }));
      socket.close({ code: 1000 });
    };
  });
  await page.goto('/app/fnterminal/');
  await connected(page);
  end!();
  await expect(page.locator('#connection-text')).toHaveText('会话已结束');
  await expect(panes(page)).toHaveCount(1);
  await expect(page.getByRole('button', { name: '重新连接', exact: true })).toBeEnabled();
});

test('parenthesis symbols work on other layouts and held keys create only one pane', async ({
  page,
}) => {
  await page.goto('/app/fnterminal/');
  await connected(page);
  const keyboard = {
    key: '(',
    code: 'BracketLeft',
    ctrlKey: true,
    bubbles: true,
    cancelable: true,
  };
  await panes(page)
    .first()
    .locator('.xterm-helper-textarea')
    .dispatchEvent('keydown', { ...keyboard, repeat: true });
  await expect(panes(page)).toHaveCount(1);
  await panes(page).first().locator('.xterm-helper-textarea').dispatchEvent('keydown', keyboard);
  await expect(panes(page)).toHaveCount(2);
  await connected(page);
});

test('a delayed host bridge cannot close sessions created after the last exit', async ({
  page,
}) => {
  await page.route('**/delayed-host', (route) =>
    route.fulfill({
      contentType: 'text/html',
      body: `
    <!doctype html><title>Delayed host</title>
    <script>
      window.closeCalls = 0;
      window.acknowledged = false;
      addEventListener('message', event => {
        if (event.source !== document.querySelector('iframe')?.contentWindow || event.origin !== location.origin) return;
        const data = event.data;
        if (data?.penpal === 'syn') window.releaseBridge = () => event.source.postMessage({penpal:'synAck',id:'test-host',appName:'fnterminal',methodNames:['close'],config:{}}, event.origin);
        if (data?.penpal === 'ack') window.acknowledged = true;
        if (data?.penpal === 'call' && data.methodName === 'close') {
          window.closeCalls++;
          event.source.postMessage({penpal:'reply',id:data.id,resolution:'fulfilled',returnValue:null}, event.origin);
        }
      });
    </script>
    <iframe src="/app/fnterminal/" style="width:1100px;height:650px"></iframe>
  `,
    }),
  );
  await page.goto('/delayed-host');
  const terminal = page.frameLocator('iframe');
  const framePanes = terminal.locator('.terminal-pane');
  await expect(terminal.locator('#connection-text')).toHaveText('已连接');
  await expect
    .poll(() => page.evaluate(() => typeof (window as any).releaseBridge))
    .toBe('function');
  await terminal.locator('.xterm-helper-textarea').focus();
  await page.keyboard.type('exit');
  await page.keyboard.press('Enter');
  await expect(framePanes).toHaveCount(0);
  await terminal.getByRole('button', { name: '新建终端', exact: true }).click();
  await expect(terminal.locator('#connection-text')).toHaveText('已连接');
  await terminal.getByRole('button', { name: '关闭终端 2', exact: true }).click();
  await expect(framePanes).toHaveCount(0);
  await page.evaluate(() => (window as any).releaseBridge());
  await expect.poll(() => page.evaluate(() => (window as any).acknowledged)).toBe(true);
  await terminal.getByRole('button', { name: '新建终端', exact: true }).click();
  await expect(terminal.locator('#connection-text')).toHaveText('已连接');
  expect(await page.evaluate(() => (window as any).closeCalls)).toBe(0);
  await terminal.locator('.xterm-helper-textarea').focus();
  await page.keyboard.type('exit');
  await page.keyboard.press('Enter');
  await expect.poll(() => page.evaluate(() => (window as any).closeCalls)).toBe(1);
});
