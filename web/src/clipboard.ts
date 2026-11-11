import type { TerminalSession } from './session';

const pasteShortcut = /Mac|iPhone|iPad/.test(navigator.platform) ? '⌘V' : 'Ctrl+Shift+V';

// The async Clipboard API only exists in secure contexts (HTTPS or localhost),
// and fnOS is commonly opened over plain HTTP on a LAN address. Copying from a
// hidden textarea inside the user's click keeps the toolbar and menu working there.
function copyThroughSelection(text: string): boolean {
  const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
  const area = document.createElement('textarea');
  area.className = 'clipboard-buffer';
  area.value = text;
  area.readOnly = true;
  area.tabIndex = -1;
  area.setAttribute('aria-hidden', 'true');
  document.body.append(area);
  let copied = false;
  try {
    area.focus({ preventScroll: true });
    area.select();
    area.setSelectionRange(0, text.length);
    copied = document.execCommand('copy');
  } catch {
    copied = false;
  } finally {
    area.remove();
    previous?.focus({ preventScroll: true });
  }
  return copied;
}

export function copySelection(session: TerminalSession, notice: (text: string) => void): void {
  if (session.disposed) return;
  const selection = session.terminal.getSelection();
  if (!selection) {
    notice('先在终端中选中要复制的文字。');
    return;
  }
  const done = (copied: boolean) =>
    notice(copied ? '已复制选中文字。' : '无法写入剪贴板，请使用浏览器的复制功能。');
  if (!navigator.clipboard?.writeText) {
    done(copyThroughSelection(selection));
    return;
  }
  // Embedding pages or browser policy can still reject the API in a secure context.
  void navigator.clipboard.writeText(selection).then(
    () => done(true),
    // Relies on the click's transient user activation still being valid when the promise rejects.
    () => done(copyThroughSelection(selection)),
  );
}

export function pasteInto(session: TerminalSession, notice: (text: string) => void): void {
  if (session.disposed || session.state !== 'connected') {
    notice('请先连接终端。');
    return;
  }
  // Browsers never let a page read the clipboard without the async API, but the
  // keyboard paste event still reaches the terminal over HTTP.
  if (!navigator.clipboard?.readText) {
    session.focus();
    notice(`浏览器不允许按钮读取剪贴板，请按 ${pasteShortcut} 粘贴。`);
    return;
  }
  const connection = session.connectionVersion;
  void navigator.clipboard
    .readText()
    .then((text) => {
      if (session.disposed) return;
      if (session.connectionVersion !== connection || session.state !== 'connected') {
        notice('终端连接已改变，请再次粘贴。');
        return;
      }
      session.terminal.paste(text);
      session.focus();
    })
    .catch(() => {
      session.focus();
      notice(`无法读取剪贴板，请按 ${pasteShortcut} 粘贴。`);
    });
}
