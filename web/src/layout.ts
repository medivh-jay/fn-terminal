export type SplitDirection = 'rows' | 'columns';
export interface Pane<T> {
  kind: 'pane';
  value: T;
  element: HTMLElement;
  parent?: Split<T>;
}
interface Split<T> {
  kind: 'split';
  element: HTMLDivElement;
  first: LayoutNode<T>;
  second: LayoutNode<T>;
  direction: SplitDirection;
  ratio: number;
  parent?: Split<T>;
}
type LayoutNode<T> = Pane<T> | Split<T>;

export class PaneLayout<T> {
  private root: LayoutNode<T> | undefined;

  constructor(
    value: T,
    element: HTMLElement,
    host: HTMLElement,
    private readonly resized: () => void,
  ) {
    this.root = { kind: 'pane', value, element };
    host.append(element);
  }

  panes(): Pane<T>[] {
    const collect = (node: LayoutNode<T>): Pane<T>[] =>
      node.kind === 'pane' ? [node] : [...collect(node.first), ...collect(node.second)];
    return this.root ? collect(this.root) : [];
  }

  split(target: Pane<T>, value: T, element: HTMLElement, direction: SplitDirection): Pane<T> {
    if (!this.panes().includes(target)) throw new Error('pane no longer exists');
    const next: Pane<T> = { kind: 'pane', value, element };
    const container = document.createElement('div');
    container.className = 'pane-split';
    container.dataset.direction = direction;
    const branch: Split<T> = {
      kind: 'split',
      element: container,
      first: target,
      second: next,
      direction,
      ratio: 0.5,
    };
    this.replace(target, branch);
    target.parent = branch;
    next.parent = branch;
    const divider = this.divider(branch);
    container.append(target.element, divider, next.element);
    this.updateRatio(branch, divider);
    this.resized();
    return next;
  }

  remove(target: Pane<T>): Pane<T> | undefined {
    if (!this.panes().includes(target)) return undefined;
    const parent = target.parent;
    if (!parent) {
      target.element.remove();
      this.root = undefined;
      return undefined;
    }
    const sibling = parent.first === target ? parent.second : parent.first;
    this.replace(parent, sibling);
    target.parent = undefined;
    this.resized();
    return sibling.kind === 'pane' ? sibling : this.first(sibling);
  }

  private first(node: LayoutNode<T>): Pane<T> {
    return node.kind === 'pane' ? node : this.first(node.first);
  }

  private replace(old: LayoutNode<T>, next: LayoutNode<T>): void {
    const parent = old.parent;
    next.parent = parent;
    next.element.style.flex = old.element.style.flex;
    old.element.parentElement?.replaceChild(next.element, old.element);
    if (!parent) this.root = next;
    else if (parent.first === old) parent.first = next;
    else parent.second = next;
  }

  private updateRatio(branch: Split<T>, divider: HTMLElement): void {
    branch.first.element.style.flex = `${branch.ratio} 1 0`;
    branch.second.element.style.flex = `${1 - branch.ratio} 1 0`;
    divider.setAttribute('aria-valuenow', String(Math.round(branch.ratio * 100)));
  }

  private divider(branch: Split<T>): HTMLDivElement {
    const divider = document.createElement('div');
    divider.className = 'pane-divider';
    divider.tabIndex = 0;
    divider.setAttribute('role', 'separator');
    divider.setAttribute('aria-label', '调整窗格大小');
    divider.setAttribute(
      'aria-orientation',
      branch.direction === 'columns' ? 'vertical' : 'horizontal',
    );
    divider.setAttribute('aria-valuemin', '10');
    divider.setAttribute('aria-valuemax', '90');
    let pointer: number | undefined;
    const adjust = (ratio: number) => {
      branch.ratio = Math.max(0.1, Math.min(0.9, ratio));
      this.updateRatio(branch, divider);
      this.resized();
    };
    divider.addEventListener('pointerdown', (event) => {
      if (event.button !== 0) return;
      event.preventDefault();
      event.stopPropagation();
      pointer = event.pointerId;
      divider.setPointerCapture(pointer);
    });
    divider.addEventListener('pointermove', (event) => {
      if (event.pointerId !== pointer) return;
      const rect = branch.element.getBoundingClientRect();
      const total = branch.direction === 'columns' ? rect.width : rect.height;
      if (total > 0)
        adjust(
          (branch.direction === 'columns' ? event.clientX - rect.left : event.clientY - rect.top) /
            total,
        );
    });
    const release = (event: PointerEvent) => {
      if (event.pointerId !== pointer) return;
      if (divider.hasPointerCapture(event.pointerId))
        divider.releasePointerCapture(event.pointerId);
      pointer = undefined;
    };
    divider.addEventListener('pointerup', release);
    divider.addEventListener('pointercancel', release);
    divider.addEventListener('keydown', (event) => {
      const forward = branch.direction === 'columns' ? 'ArrowRight' : 'ArrowDown';
      const backward = branch.direction === 'columns' ? 'ArrowLeft' : 'ArrowUp';
      if (
        event.key !== forward &&
        event.key !== backward &&
        event.key !== 'Home' &&
        event.key !== 'End'
      )
        return;
      event.preventDefault();
      adjust(
        event.key === 'Home'
          ? 0.1
          : event.key === 'End'
            ? 0.9
            : branch.ratio + (event.key === forward ? 0.05 : -0.05),
      );
    });
    return divider;
  }
}
