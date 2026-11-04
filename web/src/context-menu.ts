import type { SplitDirection } from './layout';

interface MenuActions {
  valid(): boolean;
  canCopy(): boolean;
  canPaste(): boolean;
  canSplit(): boolean;
  copy(): void;
  paste(): void;
  split(direction: SplitDirection): void;
  close(): void;
  focus(): void;
}

export class TerminalContextMenu {
  private readonly root = document.createElement('div');
  private readonly submenu = document.createElement('div');
  private readonly copy = this.button('复制');
  private readonly paste = this.button('粘贴');
  private readonly split = this.button('拆分视图');
  private readonly close = this.button('关闭');
  private readonly rows = this.button('上下');
  private readonly columns = this.button('左右');
  private actions?: MenuActions;
  get isOpen(): boolean {
    return !this.root.hidden;
  }

  constructor(host: HTMLElement) {
    this.root.className = 'terminal-context-menu';
    this.root.hidden = true;
    this.root.setAttribute('role', 'menu');
    this.root.setAttribute('aria-label', '终端菜单');
    this.submenu.id = 'split-submenu';
    this.submenu.className = 'context-submenu';
    this.submenu.hidden = true;
    this.submenu.setAttribute('role', 'menu');
    this.submenu.setAttribute('aria-label', '拆分方向');
    this.split.setAttribute('aria-haspopup', 'menu');
    this.split.setAttribute('aria-controls', this.submenu.id);
    this.split.setAttribute('aria-expanded', 'false');
    const arrow = document.createElement('span');
    arrow.className = 'menu-arrow';
    arrow.textContent = '›';
    arrow.setAttribute('aria-hidden', 'true');
    this.split.append(arrow);
    this.submenu.append(this.rows, this.columns);
    this.root.append(this.copy, this.paste, this.split, this.close, this.submenu);
    host.append(this.root);
    this.copy.addEventListener('click', () =>
      this.run((actions) => {
        actions.copy();
        actions.focus();
      }),
    );
    this.paste.addEventListener('click', () =>
      this.run((actions) => {
        actions.paste();
        actions.focus();
      }),
    );
    this.close.addEventListener('click', () => this.run((actions) => actions.close()));
    this.rows.addEventListener('click', () => this.run((actions) => actions.split('rows')));
    this.columns.addEventListener('click', () => this.run((actions) => actions.split('columns')));
    this.split.addEventListener('pointerenter', () => {
      this.split.focus();
      this.openSubmenu();
    });
    this.split.addEventListener('click', () => {
      this.openSubmenu();
      this.rows.focus();
    });
    for (const item of [this.copy, this.paste, this.close])
      item.addEventListener('pointerenter', () => {
        item.focus();
        this.hideSubmenu();
      });
    for (const item of [this.rows, this.columns])
      item.addEventListener('pointerenter', () => item.focus());
    this.root.addEventListener('contextmenu', (event) => event.preventDefault());
    this.root.addEventListener('keydown', (event) => this.keydown(event));
    document.addEventListener(
      'pointerdown',
      (event) => {
        if (!this.root.contains(event.target as Node)) this.hide(false);
      },
      true,
    );
    window.addEventListener('resize', () => this.hide(false));
    window.addEventListener('blur', () => this.hide(false));
    document.addEventListener(
      'wheel',
      (event) => {
        if (!this.root.contains(event.target as Node)) this.hide(false);
      },
      { capture: true, passive: true },
    );
  }

  private button(label: string): HTMLButtonElement {
    const button = document.createElement('button');
    button.type = 'button';
    button.textContent = label;
    button.setAttribute('role', 'menuitem');
    return button;
  }

  show(actions: MenuActions, x: number, y: number): void {
    this.hide(false);
    this.actions = actions;
    this.root.hidden = false;
    this.root.style.visibility = 'hidden';
    this.submenu.hidden = false;
    this.refresh();
    const width = this.root.getBoundingClientRect().width;
    const subWidth = this.submenu.getBoundingClientRect().width;
    const subHeight = this.submenu.getBoundingClientRect().height;
    const height = Math.max(
      this.root.getBoundingClientRect().height,
      this.split.offsetTop + subHeight + 2,
    );
    const left = Math.max(6, Math.min(x, innerWidth - width - subWidth - 6));
    const top = Math.max(6, Math.min(y, innerHeight - height - 6));
    this.root.style.left = `${left}px`;
    this.root.style.top = `${top}px`;
    this.submenu.style.top = `${Math.max(2, Math.min(this.split.offsetTop, innerHeight - top - subHeight - 6))}px`;
    this.hideSubmenu();
    this.root.style.visibility = '';
    this.items(this.root)[0]?.focus();
  }

  refresh(): void {
    if (!this.actions) return;
    if (!this.actions.valid()) {
      this.hide(false);
      return;
    }
    this.copy.disabled = !this.actions.canCopy();
    this.paste.disabled = !this.actions.canPaste();
    this.split.disabled = !this.actions.canSplit();
    this.rows.disabled = this.split.disabled;
    this.columns.disabled = this.split.disabled;
    if (this.split.disabled) this.hideSubmenu();
  }

  hide(focus = true): void {
    const actions = this.actions;
    this.actions = undefined;
    this.root.hidden = true;
    this.hideSubmenu();
    if (focus && actions?.valid()) actions.focus();
  }

  private run(action: (actions: MenuActions) => void): void {
    const actions = this.actions;
    this.hide(false);
    if (actions?.valid()) action(actions);
  }
  private openSubmenu(): void {
    if (!this.split.disabled && this.actions) {
      this.submenu.hidden = false;
      this.split.setAttribute('aria-expanded', 'true');
    }
  }
  private hideSubmenu(): void {
    this.submenu.hidden = true;
    this.split.setAttribute('aria-expanded', 'false');
  }
  private items(menu: HTMLElement): HTMLButtonElement[] {
    return [...menu.querySelectorAll<HTMLButtonElement>(':scope > button:not(:disabled)')];
  }

  private keydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      this.hide();
      return;
    }
    if (event.key === 'Tab') {
      this.hide(false);
      return;
    }
    const insideSubmenu = this.submenu.contains(event.target as Node);
    if (event.key === 'ArrowRight' && event.target === this.split) {
      event.preventDefault();
      this.openSubmenu();
      this.rows.focus();
      return;
    }
    if (event.key === 'ArrowLeft' && insideSubmenu) {
      event.preventDefault();
      this.hideSubmenu();
      this.split.focus();
      return;
    }
    if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return;
    event.preventDefault();
    const items = this.items(insideSubmenu ? this.submenu : this.root);
    const index = items.indexOf(event.target as HTMLButtonElement);
    const next =
      event.key === 'Home'
        ? 0
        : event.key === 'End'
          ? items.length - 1
          : (index + (event.key === 'ArrowDown' ? 1 : -1) + items.length) % items.length;
    items[next]?.focus();
  }
}
