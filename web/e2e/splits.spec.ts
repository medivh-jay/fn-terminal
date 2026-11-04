import { test, expect, type Page, type Locator } from '@playwright/test';

const panes = (page: Page) => page.locator('.tab-panel:not([hidden]) .terminal-pane');
async function command(page: Page, pane: Locator, text: string): Promise<void> {
  await pane.locator('.xterm-helper-textarea').focus();
  await page.keyboard.type(text);
  await page.keyboard.press('Enter');
}
async function split(page: Page, pane: Locator, direction: '上下' | '左右'): Promise<void> {
  await pane.click({ button: 'right', position: { x: 35, y: 70 } });
  await expect(page.getByRole('menu', { name: '终端菜单', exact: true })).toBeVisible();
  await page.getByRole('menuitem', { name: '拆分视图', exact: true }).hover();
  await page.getByRole('menuitem', { name: direction, exact: true }).click();
}

test('right-click split submenu opens to the right and splits can be nested', async ({ page }) => {
  await page.goto('/app/fnterminal/');
  await expect(page.locator('#connection-text')).toHaveText('已连接');
  await command(page, panes(page).first(), 'SPLIT_VALUE=ORIGINAL');
  await panes(page)
    .first()
    .click({ button: 'right', position: { x: 40, y: 80 } });
  const menu = page.getByRole('menu', { name: '终端菜单', exact: true });
  await expect(menu).toBeVisible();
  await expect(page.getByRole('menuitem', { name: '关闭', exact: true })).toBeVisible();
  await page.getByRole('menuitem', { name: '拆分视图', exact: true }).hover();
  const sub = page.getByRole('menu', { name: '拆分方向', exact: true });
  const mainBox = await menu.boundingBox();
  const subBox = await sub.boundingBox();
  expect(subBox!.x).toBeGreaterThan(mainBox!.x + mainBox!.width - 10);
  await page.screenshot({ path: '../build/terminal-context-menu.png', fullPage: true });
  await page.getByRole('menuitem', { name: '左右', exact: true }).click();
  await expect(panes(page)).toHaveCount(2);
  await expect(page.locator('#connection-text')).toHaveText('已连接');
  await command(
    page,
    panes(page).nth(1),
    'printf \'\\nINDEPENDENT_%s\\n\' "${SPLIT_VALUE:-EMPTY}"',
  );
  await expect(panes(page).nth(1).locator('.xterm-rows')).toContainText('INDEPENDENT_EMPTY');
  await split(page, panes(page).nth(1), '上下');
  await expect(panes(page)).toHaveCount(3);
  await expect(page.locator('#connection-text')).toHaveText('已连接');
  const boxes = await panes(page).evaluateAll((nodes) =>
    nodes.map((node) => {
      const r = node.getBoundingClientRect();
      return { x: r.x, y: r.y, width: r.width, height: r.height };
    }),
  );
  expect(boxes[1].x).toBeGreaterThan(boxes[0].x + boxes[0].width - 2);
  expect(boxes[2].x).toBe(boxes[1].x);
  expect(boxes[2].y).toBeGreaterThan(boxes[1].y + boxes[1].height - 2);
  await command(page, panes(page).first(), 'printf \'\\nORIGINAL_%s\\n\' "$SPLIT_VALUE"');
  await expect(panes(page).first().locator('.xterm-rows')).toContainText('ORIGINAL_ORIGINAL');
  await page.screenshot({ path: '../build/terminal-splits.png', fullPage: true });
  await panes(page)
    .nth(1)
    .click({ button: 'right', position: { x: 40, y: 60 } });
  await page.getByRole('menuitem', { name: '关闭', exact: true }).click();
  await expect(panes(page)).toHaveCount(2);
  const right = await panes(page).nth(1).boundingBox();
  const left = await panes(page).first().boundingBox();
  expect(right!.height).toBe(left!.height);
  await page.getByRole('button', { name: '关闭终端 1', exact: true }).click();
  await expect(panes(page)).toHaveCount(0);
  await expect(page.getByText('点击 + 新建终端', { exact: true })).toBeVisible();
});

test('split menus respect viewport edges and all panes share the session limit', async ({
  page,
}) => {
  await page.setViewportSize({ width: 640, height: 480 });
  await page.goto('/app/fnterminal/');
  await expect(page.locator('#connection-text')).toHaveText('已连接');
  await panes(page)
    .first()
    .click({ button: 'right', position: { x: 620, y: 420 } });
  await page.getByRole('menuitem', { name: '拆分视图', exact: true }).hover();
  const bounds = await page.getByRole('menu', { name: '拆分方向', exact: true }).boundingBox();
  expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(640);
  expect(bounds!.y + bounds!.height).toBeLessThanOrEqual(480);
  await page.keyboard.press('Escape');
  await expect(page.getByRole('menu', { name: '终端菜单', exact: true })).toBeHidden();
  for (let i = 1; i < 4; i++) {
    await split(page, panes(page).last(), '上下');
    await expect(page.locator('#connection-text')).toHaveText('已连接');
  }
  await expect(panes(page)).toHaveCount(4);
  await expect(page.getByRole('button', { name: '新建终端', exact: true })).toBeDisabled();
  await panes(page)
    .last()
    .click({ button: 'right', position: { x: 25, y: 15 } });
  await expect(page.getByRole('menuitem', { name: '拆分视图', exact: true })).toBeDisabled();
  await page.getByRole('menuitem', { name: '关闭', exact: true }).click();
  await expect(panes(page)).toHaveCount(3);
  await expect(page.getByRole('button', { name: '新建终端', exact: true })).toBeEnabled();
});

test('context menu supports keyboard selection and closes on outside click', async ({ page }) => {
  await page.goto('/app/fnterminal/');
  await expect(page.locator('#connection-text')).toHaveText('已连接');
  await page.locator('.xterm-helper-textarea').focus();
  await page.keyboard.press('Shift+F10');
  await expect(page.getByRole('menu', { name: '终端菜单', exact: true })).toBeVisible();
  await page.getByRole('menuitem', { name: '拆分视图', exact: true }).focus();
  await page.keyboard.press('ArrowRight');
  await expect(page.getByRole('menuitem', { name: '上下', exact: true })).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(panes(page)).toHaveCount(2);
  await expect(page.locator('#connection-text')).toHaveText('已连接');
  await panes(page)
    .first()
    .click({ button: 'right', position: { x: 40, y: 60 } });
  await page.getByRole('button', { name: '清屏', exact: true }).click();
  await expect(page.getByRole('menu', { name: '终端菜单', exact: true })).toBeHidden();
});

test('context copy preserves the selection and paste uses the right-clicked pane', async ({
  page,
}) => {
  await page.goto('/app/fnterminal/');
  await expect(page.locator('#connection-text')).toHaveText('已连接');
  await split(page, panes(page).first(), '左右');
  await expect(page.locator('#connection-text')).toHaveText('已连接');
  await page.evaluate(() =>
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: {
        writeText: async (text: string) => {
          (window as any).copied = text;
        },
        readText: async () => 'MENU_PASTE_TARGET',
      },
    }),
  );
  await command(page, panes(page).first(), "printf '\\nCOPY_'; printf 'TARGET\\n'");
  const row = panes(page)
    .first()
    .locator('.xterm-rows > div')
    .filter({ hasText: /^COPY_TARGET\s*$/ });
  await expect(row).toBeVisible();
  const box = (await row.boundingBox())!;
  await page.mouse.move(box.x + 1, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(box.x + 95, box.y + box.height / 2, { steps: 8 });
  await page.mouse.up();
  await page.mouse.click(box.x + 30, box.y + box.height / 2, { button: 'right' });
  await expect(page.getByRole('menuitem', { name: '复制', exact: true })).toBeEnabled();
  await page.getByRole('menuitem', { name: '复制', exact: true }).click();
  expect(await page.evaluate(() => (window as any).copied.trim())).toBe('COPY_TARGET');
  await panes(page)
    .nth(1)
    .click({ button: 'right', position: { x: 35, y: 70 } });
  await page.getByRole('menuitem', { name: '粘贴', exact: true }).click();
  await expect(panes(page).nth(1).locator('.xterm-rows')).toContainText('MENU_PASTE_TARGET');
  await expect(panes(page).first().locator('.xterm-rows')).not.toContainText('MENU_PASTE_TARGET');
});

test('divider resizing updates the original shell PTY dimensions', async ({ page }) => {
  await page.goto('/app/fnterminal/');
  await expect(page.locator('#connection-text')).toHaveText('已连接');
  await split(page, panes(page).first(), '左右');
  await expect(page.locator('#connection-text')).toHaveText('已连接');
  await command(page, panes(page).first(), "printf '\\nBEFORE:'; stty size");
  await expect(panes(page).first().locator('.xterm-rows')).toContainText(/BEFORE:\d+ \d+/);
  const before = Number(
    (await panes(page).first().locator('.xterm-rows').textContent())!.match(/BEFORE:\d+ (\d+)/)![1],
  );
  const divider = page.getByRole('separator', { name: '调整窗格大小' });
  const box = (await divider.boundingBox())!;
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(box.x + 150, box.y + box.height / 2, { steps: 10 });
  await page.mouse.up();
  await command(page, panes(page).first(), "printf '\\nAFTER:'; stty size");
  await expect(panes(page).first().locator('.xterm-rows')).toContainText(/AFTER:\d+ \d+/);
  const after = Number(
    (await panes(page).first().locator('.xterm-rows').textContent())!.match(/AFTER:\d+ (\d+)/)![1],
  );
  expect(after).toBeGreaterThan(before + 5);
});

test('right mouse events do not reach a terminal application reporting mouse input', async ({
  page,
}) => {
  const reports: string[] = [];
  page.on('websocket', (socket) =>
    socket.on('framesent', (event) => {
      const data = event.payload.toString();
      if (data.includes('\u001b[<')) reports.push(data);
    }),
  );
  await page.goto('/app/fnterminal/');
  await expect(page.locator('#connection-text')).toHaveText('已连接');
  await command(
    page,
    panes(page).first(),
    "printf '\\033[?1000h\\033[?1006h\\nMOUSE_'; printf 'READY\\n'",
  );
  await expect(page.locator('.xterm-rows')).toContainText('MOUSE_READY');
  await panes(page)
    .first()
    .click({ position: { x: 35, y: 80 } });
  await expect.poll(() => reports.length).toBeGreaterThan(0);
  const count = reports.length;
  await panes(page)
    .first()
    .click({ button: 'right', position: { x: 35, y: 80 } });
  await expect(page.getByRole('menu', { name: '终端菜单', exact: true })).toBeVisible();
  expect(reports.length).toBe(count);
  await page.keyboard.press('Escape');
});

test('a late shell ready message does not steal focus from the open context menu', async ({
  page,
}) => {
  let ready: (() => void) | undefined;
  await page.routeWebSocket('**/app/fnterminal/ws', (socket) => {
    const upstream = socket.connectToServer();
    upstream.onMessage((message) => {
      if (typeof message === 'string' && JSON.parse(message).type === 'ready')
        ready = () => socket.send(message);
      else socket.send(message);
    });
    socket.onMessage((message) => upstream.send(message));
  });
  await page.goto('/app/fnterminal/');
  await expect.poll(() => !!ready).toBe(true);
  await panes(page)
    .first()
    .click({ button: 'right', position: { x: 35, y: 70 } });
  const splitItem = page.getByRole('menuitem', { name: '拆分视图', exact: true });
  await splitItem.focus();
  ready!();
  await expect(page.locator('#connection-text')).toHaveText('已连接');
  await expect(splitItem).toBeFocused();
  await page.keyboard.press('ArrowRight');
  await expect(page.getByRole('menuitem', { name: '上下', exact: true })).toBeFocused();
  await page.keyboard.press('Escape');
});
