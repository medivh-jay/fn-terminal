import { Terminal, type ITheme } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { chunkInput, parseServerMessage } from './protocol';

export type ConnectionState = 'connecting' | 'connected' | 'closed' | 'error';
export interface SessionEvents {
  canFocus(): boolean;
  changed(session: TerminalSession): void;
  account(maxPerUser: number): void;
  notice(message: string): void;
  exited(session: TerminalSession): void;
}

const prefix = '/app/fnterminal';

const terminalNotice = (text: string) => `\r\n\x1b[33m[飞牛终端] ${text}\x1b[0m\r\n`;
// The toolbar button acts on the focused pane, so in a split the pane must be
// selected first; otherwise the button replaces the shell of a healthy neighbour.
const reconnectAdvice = '请点击此窗格，再点击右上角“重新连接”打开新的 Shell。';
// Keystrokes closer together than this count as one attempt, so typing a whole
// command into a closed session prints one reminder rather than one per key.
const reminderQuietMs = 3000;
// A shell can die inside a full-screen program. CAN aborts an escape sequence cut
// off mid-stream, soft reset restores the cursor, colours and scroll region, and
// the rest stop mouse and focus reports from counting as input to a dead session.
const releaseTerminal =
  '\x18\x1b[!p\x1b[?9l\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1004l\x1b[?2004l';

export class TerminalSession {
  readonly element = document.createElement('div');
  readonly terminal: Terminal;
  private readonly fit = new FitAddon();
  private socket: WebSocket | null = null;
  private generation = 0;
  private request: AbortController | undefined;
  private timeout: ReturnType<typeof setTimeout> | undefined;
  private focused = false;
  private closureAnnounced = false;
  private lastClosedInputAt = 0;
  disposed = false;
  hadSession = false;
  username = '';
  state: ConnectionState = 'closed';
  statusText = '准备连接';
  emptyMessage = '正在验证当前用户…';
  emptyDetail = '随后将打开你的 Shell';

  constructor(
    readonly number: number,
    host: HTMLElement,
    private readonly events: SessionEvents,
    theme: ITheme,
  ) {
    this.element.id = `terminal-${number}`;
    this.element.className = 'terminal-session';
    this.element.setAttribute('role', 'region');
    this.element.setAttribute('aria-label', `终端窗格 ${number}`);
    this.element.hidden = true;
    host.append(this.element);
    this.terminal = new Terminal({
      cursorBlink: true,
      cursorStyle: 'bar',
      fontSize: 14,
      lineHeight: 1.35,
      fontFamily:
        '"SFMono-Regular", Consolas, "Liberation Mono", "Noto Sans Mono CJK SC", monospace',
      scrollback: 5000,
      allowTransparency: false,
      theme,
    });
    this.terminal.loadAddon(this.fit);
    this.terminal.open(this.element);
    this.terminal.onData((text) => this.sendInput(text));
    this.terminal.onBinary((data) => {
      this.inputSocket()?.send(Uint8Array.from(data, (ch) => ch.charCodeAt(0)));
    });
  }

  private update(state: ConnectionState, text: string, message?: string, detail?: string): void {
    this.state = state;
    this.statusText = text;
    if (message !== undefined) this.emptyMessage = message;
    if (detail !== undefined) this.emptyDetail = detail;
    this.events.changed(this);
  }

  resize(): void {
    if (this.disposed || this.element.hidden || this.element.getClientRects().length === 0) return;
    this.fit.fit();
    if (this.socket?.readyState === WebSocket.OPEN)
      this.socket.send(
        JSON.stringify({
          type: 'resize',
          rows: Math.max(2, Math.min(500, this.terminal.rows)),
          cols: Math.max(2, Math.min(1000, this.terminal.cols)),
        }),
      );
  }

  setFocused(focused: boolean): void {
    this.focused = focused;
  }
  focus(): void {
    if (
      this.focused &&
      this.events.canFocus() &&
      !this.disposed &&
      !this.element.hidden &&
      this.element.getClientRects().length > 0
    )
      this.terminal.focus();
  }
  get connectionVersion(): number {
    return this.generation;
  }

  connect(): Promise<void> {
    return this.startConnection(0);
  }

  private async startConnection(attempt: number): Promise<void> {
    if (this.disposed || (this.state === 'connecting' && attempt === 0)) return;
    const current = ++this.generation;
    const live = () => !this.disposed && current === this.generation;
    this.request?.abort();
    clearTimeout(this.timeout);
    this.socket?.close();
    this.socket = null;
    this.hadSession = false;
    this.closureAnnounced = false;
    this.terminal.reset();
    this.update('connecting', '正在连接', '正在验证当前用户…', '随后将打开你的 Shell');
    this.request = new AbortController();
    const signal = AbortSignal.any([this.request.signal, AbortSignal.timeout(10000)]);
    try {
      const response = await fetch(`${prefix}/api/me`, {
        credentials: 'same-origin',
        cache: 'no-store',
        signal,
      });
      if (!response.ok)
        throw new Error(
          response.status === 401
            ? '请从已登录的飞牛桌面打开此应用。'
            : '此账号暂时无法使用终端，请检查应用授权和系统 Shell 设置。',
        );
      const me = (await response.json()) as {
        username: string;
        enabled: boolean;
        max_per_user?: number;
      };
      if (!live()) return;
      this.username = me.username;
      this.events.account(me.max_per_user ?? 4);
      if (!me.enabled) {
        this.update('closed', '尚未启用', '终端尚未启用', '请管理员在应用设置中启用终端访问。');
        return;
      }
      this.update('connecting', '正在连接', '正在打开 Shell…', `以 ${me.username} 的身份连接`);
      const ticketResponse = await fetch(`${prefix}/api/ws-ticket`, {
        method: 'POST',
        mode: 'same-origin',
        credentials: 'same-origin',
        redirect: 'error',
        cache: 'no-store',
        signal,
        headers: { 'Content-Type': 'application/json', 'X-Fnterminal-Request': '1' },
        body: JSON.stringify({ origin: location.origin }),
      });
      if (!ticketResponse.ok)
        throw new Error(
          ticketResponse.status === 429
            ? '连接请求过多，请稍后重新连接。'
            : '无法验证本次连接，请重新打开应用后重试。',
        );
      const { ticket } = (await ticketResponse.json()) as { ticket: string };
      if (!live()) return;
      if (typeof ticket !== 'string' || !/^[A-Za-z0-9_-]{43}$/.test(ticket))
        throw new Error('无法验证本次连接，请重新打开应用后重试。');
      const url = new URL(`${prefix}/ws`, window.location.href);
      url.protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
      const ws = new WebSocket(url, ['fnterminal.v1', `fnterminal-ticket.${ticket}`]);
      this.socket = ws;
      ws.binaryType = 'arraybuffer';
      let failed = false;
      let timedOut = false;
      this.timeout = setTimeout(() => {
        if (live() && !this.hadSession) {
          failed = true;
          timedOut = true;
          ws.close();
          this.update('error', '连接超时', '连接超时', '请检查网络或应用运行状态，再重新连接。');
        }
      }, 10000);
      ws.onopen = () => {
        if (live()) this.resize();
      };
      ws.onmessage = (event: MessageEvent<string | ArrayBuffer>) => {
        if (!live()) return;
        if (event.data instanceof ArrayBuffer) {
          this.terminal.write(new Uint8Array(event.data), () => {
            if (live() && ws.readyState === WebSocket.OPEN)
              ws.send(JSON.stringify({ type: 'ack' }));
          });
          return;
        }
        const message = parseServerMessage(event.data);
        if (message?.type === 'ready') {
          clearTimeout(this.timeout);
          this.hadSession = true;
          this.update('connected', '已连接');
          this.resize();
          this.focus();
        }
        if (message?.type === 'exit') {
          this.update('closed', '会话已结束');
          if (this.hadSession && message.reason === 'shell_exit') this.events.exited(this);
          else this.announceClosure(message.notified ? '' : `会话已结束。${reconnectAdvice}`);
        }
      };
      ws.onclose = () => {
        if (!live()) return;
        clearTimeout(this.timeout);
        if (!this.hadSession) {
          // At capacity the previous PTY may still be releasing its quota.
          // Retry once with a fresh ticket; never relax the broker's limit.
          if (attempt === 0 && !timedOut) {
            this.update('connecting', '正在连接', '正在重新连接…', '正在等待上一会话结束');
            this.timeout = setTimeout(() => {
              if (live()) void this.startConnection(1);
            }, 350);
            return;
          }
          if (!failed)
            this.update(
              'error',
              '连接失败',
              '无法连接终端',
              '终端连接未能建立，请检查网络或已打开的终端数量后重试。',
            );
        } else {
          this.update('closed', '会话已结束');
          this.announceClosure(`连接已断开，无法继续输入。${reconnectAdvice}`);
        }
      };
      ws.onerror = () => {
        if (live()) {
          failed = true;
          if (attempt > 0)
            this.update(
              'error',
              '连接失败',
              '无法连接终端',
              'WebSocket 连接失败，请检查应用服务日志或访问地址。',
            );
        }
      };
    } catch (error) {
      if (live())
        this.update(
          'error',
          '连接失败',
          '无法连接终端',
          error instanceof Error ? error.message : '请检查网络后重新连接。',
        );
    }
  }

  /** Runs once per connection; an empty message means the server already explained. */
  private announceClosure(message: string): void {
    if (!this.hadSession || this.closureAnnounced) return;
    this.closureAnnounced = true;
    this.lastClosedInputAt = performance.now();
    this.terminal.write(releaseTerminal + (message && terminalNotice(message)));
  }

  private remindClosed(): void {
    const now = performance.now();
    const quiet = now - this.lastClosedInputAt >= reminderQuietMs;
    this.lastClosedInputAt = now;
    if (quiet) this.terminal.write(terminalNotice(`连接已断开，输入未发送。${reconnectAdvice}`));
  }

  /** The socket input may go to, or null after telling the user why it cannot. */
  private inputSocket(): WebSocket | null {
    if (this.disposed) return null;
    const open = this.socket?.readyState === WebSocket.OPEN;
    if (this.hadSession && (!open || this.state !== 'connected')) {
      this.remindClosed();
      return null;
    }
    return open ? this.socket : null;
  }

  sendInput(text: string): void {
    const socket = this.inputSocket();
    if (!socket) return;
    if (socket.bufferedAmount > 1024 * 1024) {
      this.events.notice('连接发送缓慢，请稍后再输入。');
      return;
    }
    for (const bytes of chunkInput(text)) socket.send(bytes);
  }

  close(): void {
    if (this.disposed) return;
    this.disposed = true;
    ++this.generation;
    this.request?.abort();
    clearTimeout(this.timeout);
    this.socket?.close();
    this.terminal.dispose();
    this.element.remove();
  }
}
