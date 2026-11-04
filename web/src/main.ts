import '@xterm/xterm/css/xterm.css';
import './style.css';
import { TerminalSession } from './session';
import { PaneLayout, type Pane, type SplitDirection } from './layout';
import { TerminalContextMenu } from './context-menu';
import { copySelection, pasteInto } from './clipboard';
import { closeApplication } from './application';
import {
  themes,
  themeStorageKey,
  loadTheme,
  saveTheme,
  applyTheme,
  resolveTheme,
  type Theme,
} from './themes';

const el = <T extends HTMLElement = HTMLElement>(id: string): T => {
  const node = document.getElementById(id);
  if (!node) throw new Error(`Missing element: ${id}`);
  return node as T;
};
interface PaneView {
  session: TerminalSession;
  empty: HTMLDivElement;
  message: HTMLParagraphElement;
  detail: HTMLSpanElement;
}
interface Tab {
  number: number;
  layout: PaneLayout<PaneView>;
  focused: Pane<PaneView>;
  panel: HTMLDivElement;
  element: HTMLDivElement;
  select: HTMLButtonElement;
  title: HTMLSpanElement;
}
const tabs: Tab[] = [];
let active: Tab | undefined;
let nextTabNumber = 1;
let nextSessionNumber = 1;
let maxSessions = 1;
let noticeTimer: ReturnType<typeof setTimeout> | undefined;
const menu = new TerminalContextMenu(el('app'));
const observer = new ResizeObserver(() => resizeVisible());
observer.observe(el('terminal-deck'));
let currentTheme = loadTheme();
applyTheme(currentTheme);
const themeSelect = el<HTMLSelectElement>('theme-select');
for (const theme of themes) {
  const option = document.createElement('option');
  option.value = theme.id;
  option.textContent = theme.label;
  themeSelect.append(option);
}
themeSelect.value = currentTheme.id;
const allPanes = () => tabs.flatMap((tab) => tab.layout.panes());
const activeSession = () => active?.focused.value.session;
function changeTheme(theme: Theme): void {
  currentTheme = theme;
  applyTheme(theme);
  themeSelect.value = theme.id;
  for (const pane of allPanes()) pane.value.session.terminal.options.theme = theme.terminal;
}
themeSelect.addEventListener('change', () => {
  const theme = resolveTheme(themeSelect.value);
  changeTheme(theme);
  saveTheme(theme);
});
window.addEventListener('storage', (event) => {
  if (event.key === themeStorageKey || event.key === null)
    changeTheme(resolveTheme(event.newValue));
});

function notice(message: string): void {
  el('notice').textContent = message;
  el('notice').hidden = false;
  clearTimeout(noticeTimer);
  noticeTimer = setTimeout(() => {
    el('notice').hidden = true;
  }, 5000);
}
function refresh(): void {
  const session = activeSession();
  for (const tab of tabs) {
    const selected = tab === active;
    const panes = tab.layout.panes();
    tab.element.classList.toggle('active', selected);
    tab.panel.hidden = !selected;
    tab.element.dataset.state = tab.focused.value.session.state;
    tab.panel.classList.toggle('has-splits', panes.length > 1);
    tab.select.setAttribute('aria-selected', String(selected));
    tab.select.tabIndex = selected ? 0 : -1;
    tab.title.textContent = `${tab.focused.value.session.username || '终端'} — Shell ${tab.number}${panes.length > 1 ? ` · ${panes.length}` : ''}`;
    tab.select.title = `${tab.title.textContent} · ${tab.focused.value.session.statusText}`;
    for (const pane of panes) {
      const focused = selected && pane === tab.focused;
      pane.element.classList.toggle('is-focused', focused);
      pane.value.session.setFocused(focused);
      pane.value.session.element.hidden = !selected;
      pane.value.empty.hidden = pane.value.session.hadSession;
      pane.value.message.textContent = pane.value.session.emptyMessage;
      pane.value.detail.textContent = pane.value.session.emptyDetail;
      pane.value.message.removeAttribute('id');
      pane.value.detail.removeAttribute('id');
    }
  }
  if (active) {
    active.focused.value.message.id = 'empty-message';
    active.focused.value.detail.id = 'empty-detail';
  }
  el('workspace-empty').hidden = !!active;
  el('connection').className = `connection ${session?.state ?? 'closed'}`;
  el('connection').title = session?.statusText ?? '未打开会话';
  el('connection-text').textContent = session?.statusText ?? '未打开会话';
  for (const id of ['copy', 'paste', 'clear']) el<HTMLButtonElement>(id).disabled = !session;
  el<HTMLButtonElement>('reconnect').disabled = !session || session.state === 'connecting';
  el<HTMLButtonElement>('new-tab').disabled = allPanes().length >= maxSessions;
  el('new-tab').title =
    allPanes().length >= maxSessions
      ? `最多同时打开 ${maxSessions} 个终端（含拆分窗格）`
      : '新建终端';
  menu.refresh();
}
function resizeVisible(): void {
  requestAnimationFrame(() => {
    if (active) for (const pane of active.layout.panes()) pane.value.session.resize();
  });
}
function activate(tab: Tab, pane = tab.focused, focusTerminal = true): void {
  menu.hide(false);
  active = tab;
  tab.focused = pane;
  refresh();
  tab.select.scrollIntoView({ block: 'nearest', inline: 'nearest' });
  resizeVisible();
  if (focusTerminal)
    requestAnimationFrame(() => {
      if (active === tab && tab.focused === pane) pane.value.session.focus();
    });
}
function locate(session: TerminalSession): { tab: Tab; pane: Pane<PaneView> } | undefined {
  for (const tab of tabs) {
    const pane = tab.layout.panes().find((candidate) => candidate.value.session === session);
    if (pane) return { tab, pane };
  }
}
function fromTarget(target: EventTarget | null): { tab: Tab; pane: Pane<PaneView> } | undefined {
  if (!(target instanceof Element)) return;
  const element = target.closest('.terminal-pane');
  for (const tab of tabs) {
    const pane = tab.layout.panes().find((candidate) => candidate.element === element);
    if (pane) return { tab, pane };
  }
}
function createPane(): { value: PaneView; element: HTMLDivElement } {
  const number = nextSessionNumber++;
  const element = document.createElement('div');
  element.className = 'terminal-pane';
  element.dataset.paneId = String(number);
  const session = new TerminalSession(
    number,
    element,
    {
      canFocus: () => !menu.isOpen,
      changed: () => refresh(),
      account: (limit) => {
        if (Number.isInteger(limit) && limit >= 1 && limit <= 32) maxSessions = limit;
        refresh();
      },
      notice,
      exited: (session) => {
        const owner = locate(session);
        if (!owner) return;
        const focusTerminal = owner.pane.element.contains(document.activeElement);
        closePane(owner.tab, owner.pane, focusTerminal);
        if (allPanes().length === 0) {
          const endedGeneration = nextSessionNumber;
          void closeApplication(
            () => allPanes().length === 0 && nextSessionNumber === endedGeneration,
            () => {
              el('workspace-empty').querySelector('p')!.textContent = '终端已退出';
              el('workspace-empty').querySelector('span:last-child')!.textContent =
                '可关闭此窗口，或点击 + 新建终端';
            },
          );
        }
      },
    },
    currentTheme.terminal,
  );
  const empty = document.createElement('div');
  empty.className = 'empty-state pane-empty';
  const symbol = document.createElement('span');
  symbol.className = 'empty-symbol';
  symbol.textContent = '>_';
  symbol.setAttribute('aria-hidden', 'true');
  const message = document.createElement('p');
  const detail = document.createElement('span');
  empty.append(symbol, message, detail);
  element.append(empty);
  observer.observe(element);
  element.addEventListener('pointerdown', (event) => {
    if (event.button === 0) {
      const owner = locate(session);
      if (owner) activate(owner.tab, owner.pane, false);
    }
  });
  element.addEventListener('focusin', () => {
    const owner = locate(session);
    if (owner && (active !== owner.tab || owner.tab.focused !== owner.pane))
      activate(owner.tab, owner.pane, false);
  });
  return { value: { session, empty, message, detail }, element };
}
function closeTab(tab: Tab, focusTerminal = true): void {
  const index = tabs.indexOf(tab);
  if (index < 0) return;
  menu.hide(false);
  tabs.splice(index, 1);
  for (const pane of tab.layout.panes()) {
    observer.unobserve(pane.element);
    pane.value.session.close();
  }
  tab.panel.remove();
  tab.element.remove();
  if (active === tab) {
    active = undefined;
    const neighbor = tabs[Math.min(index, tabs.length - 1)];
    if (neighbor) activate(neighbor, neighbor.focused, focusTerminal);
    else {
      refresh();
      if (focusTerminal) el('new-tab').focus();
    }
  } else refresh();
}
function closePane(tab: Tab, pane: Pane<PaneView>, focusTerminal = true): void {
  if (!tabs.includes(tab) || !tab.layout.panes().includes(pane)) return;
  menu.hide(false);
  observer.unobserve(pane.element);
  pane.value.session.close();
  const next = tab.layout.remove(pane);
  if (!next) {
    closeTab(tab, focusTerminal);
    return;
  }
  if (tab.focused === pane) tab.focused = next;
  if (active === tab) activate(tab, tab.focused, focusTerminal);
  else refresh();
}
function splitPane(tab: Tab, pane: Pane<PaneView>, direction: SplitDirection): void {
  if (
    !tabs.includes(tab) ||
    !tab.layout.panes().includes(pane) ||
    pane.value.session.disposed ||
    allPanes().length >= maxSessions
  )
    return;
  const created = createPane();
  const next = tab.layout.split(pane, created.value, created.element, direction);
  activate(tab, next);
  void next.value.session.connect();
}
function newTab(): void {
  if (allPanes().length >= maxSessions) return;
  el('workspace-empty').querySelector('p')!.textContent = '点击 + 新建终端';
  el('workspace-empty').querySelector('span:last-child')!.textContent = '以当前登录用户打开 Shell';
  const number = nextTabNumber++;
  const element = document.createElement('div');
  element.className = 'session-tab';
  const select = document.createElement('button');
  select.id = `tab-${number}`;
  select.className = 'tab-select';
  select.setAttribute('role', 'tab');
  select.setAttribute('aria-controls', `workspace-${number}`);
  const icon = document.createElement('span');
  icon.className = 'tab-icon';
  icon.textContent = '>_';
  icon.setAttribute('aria-hidden', 'true');
  const title = document.createElement('span');
  title.className = 'tab-title';
  const status = document.createElement('span');
  status.className = 'tab-status';
  status.setAttribute('aria-hidden', 'true');
  select.append(icon, title, status);
  const close = document.createElement('button');
  close.className = 'tab-close';
  close.textContent = '×';
  close.setAttribute('aria-label', `关闭终端 ${number}`);
  close.title = '关闭标签内所有终端会话';
  element.append(select, close);
  el('tabs').append(element);
  const panel = document.createElement('div');
  panel.id = `workspace-${number}`;
  panel.className = 'tab-panel';
  panel.hidden = true;
  panel.setAttribute('role', 'tabpanel');
  panel.setAttribute('aria-labelledby', select.id);
  el('terminal-deck').append(panel);
  const created = createPane();
  const layout = new PaneLayout(created.value, created.element, panel, resizeVisible);
  const tab: Tab = { number, element, select, title, panel, layout, focused: layout.panes()[0] };
  tabs.push(tab);
  select.addEventListener('click', () => activate(tab));
  close.addEventListener('click', () => closeTab(tab));
  select.addEventListener('keydown', (event) => {
    const index = tabs.indexOf(tab);
    const target =
      event.key === 'ArrowRight'
        ? tabs[(index + 1) % tabs.length]
        : event.key === 'ArrowLeft'
          ? tabs[(index + tabs.length - 1) % tabs.length]
          : event.key === 'Home'
            ? tabs[0]
            : event.key === 'End'
              ? tabs[tabs.length - 1]
              : undefined;
    if (target) {
      event.preventDefault();
      activate(target, target.focused, false);
      target.select.focus();
    }
    if (event.key === 'Delete') {
      event.preventDefault();
      closeTab(tab);
    }
  });
  activate(tab);
  void created.value.session.connect();
}
function showMenu(tab: Tab, pane: Pane<PaneView>, x: number, y: number): void {
  activate(tab, pane, false);
  const session = pane.value.session;
  menu.show(
    {
      valid: () => !session.disposed && tabs.includes(tab) && tab.layout.panes().includes(pane),
      canCopy: () => !!session.terminal.getSelection(),
      canPaste: () => session.state === 'connected',
      canSplit: () => allPanes().length < maxSessions,
      copy: () => copySelection(session, notice),
      paste: () => pasteInto(session, notice),
      split: (direction) => splitPane(tab, pane, direction),
      close: () => closePane(tab, pane),
      focus: () => session.focus(),
    },
    x,
    y,
  );
}
const deck = el('terminal-deck');
function splitShortcut(event: KeyboardEvent): SplitDirection | undefined {
  if (!event.ctrlKey || event.altKey || event.metaKey || event.isComposing) return;
  // Prefer the typed symbol so alternate keyboard layouts work as well.
  if (event.key === '(') return 'columns';
  if (event.key === ')') return 'rows';
  // Some platforms report the digit while Control is held with Shift.
  if (!event.shiftKey) return;
  if (event.code === 'Digit9' && event.key === '9') return 'columns';
  if (event.code === 'Digit0' && event.key === '0') return 'rows';
}
for (const name of ['pointerdown', 'mousedown', 'mouseup'] as const) {
  deck.addEventListener(
    name,
    (event) => {
      if (event.button === 2 && fromTarget(event.target)) {
        event.preventDefault();
        event.stopPropagation();
      }
    },
    true,
  );
}
deck.addEventListener(
  'contextmenu',
  (event) => {
    const owner = fromTarget(event.target);
    if (!owner) return;
    event.preventDefault();
    event.stopPropagation();
    const rect = owner.pane.element.getBoundingClientRect();
    showMenu(
      owner.tab,
      owner.pane,
      event.clientX || rect.left + 16,
      event.clientY || rect.top + 16,
    );
  },
  true,
);
deck.addEventListener(
  'keydown',
  (event) => {
    const direction = splitShortcut(event);
    if (direction) {
      const owner = fromTarget(event.target);
      if (!owner) return;
      event.preventDefault();
      event.stopImmediatePropagation();
      if (!event.repeat) splitPane(owner.tab, owner.pane, direction);
      return;
    }
    if (event.key !== 'ContextMenu' && !(event.shiftKey && event.key === 'F10')) return;
    const owner = fromTarget(event.target);
    if (!owner) return;
    event.preventDefault();
    event.stopPropagation();
    const rect = owner.pane.element.getBoundingClientRect();
    showMenu(owner.tab, owner.pane, rect.left + 16, rect.top + 16);
  },
  true,
);

el('new-tab').addEventListener('click', newTab);
el('reconnect').addEventListener('click', () => {
  void activeSession()?.connect();
});
el('clear').addEventListener('click', () => {
  activeSession()?.terminal.clear();
  activeSession()?.focus();
});
el('copy').addEventListener('click', () => {
  const session = activeSession();
  if (session) copySelection(session, notice);
});
el('paste').addEventListener('click', () => {
  const session = activeSession();
  if (session) pasteInto(session, notice);
});
el('fullscreen').addEventListener('click', () => {
  const change = document.fullscreenElement
    ? document.exitFullscreen()
    : el('app').requestFullscreen();
  void change.catch(() => notice('当前浏览器未允许全屏。'));
});
document.addEventListener('fullscreenchange', resizeVisible);
window.addEventListener('pagehide', () => {
  menu.hide(false);
  observer.disconnect();
  for (const pane of allPanes()) pane.value.session.close();
});
newTab();
