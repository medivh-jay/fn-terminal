import type { TerminalSession } from './session';

export function copySelection(session: TerminalSession, notice: (text: string) => void): void {
  if (session.disposed) return;
  const selection = session.terminal.getSelection();
  if (!selection) {
    notice('先在终端中选中要复制的文字。');
    return;
  }
  if (!navigator.clipboard) {
    notice('请使用浏览器的复制功能；剪贴板按钮需要 HTTPS。');
    return;
  }
  void navigator.clipboard
    .writeText(selection)
    .then(() => notice('已复制选中文字。'))
    .catch(() => notice('无法访问剪贴板，请使用浏览器的复制功能。'));
}

export function pasteInto(session: TerminalSession, notice: (text: string) => void): void {
  if (session.disposed || session.state !== 'connected') {
    notice('请先连接终端。');
    return;
  }
  if (!navigator.clipboard) {
    notice('请使用键盘粘贴；剪贴板按钮需要 HTTPS。');
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
    .catch(() => notice('无法读取剪贴板，请使用键盘粘贴。'));
}
