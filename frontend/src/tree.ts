// The file tree.
//
// Renaming and creating use an inline input in the row rather than window.prompt,
// and deleting goes through a native dialog on the Go side: the webview's own
// prompt/confirm are not dependable here, and losing a file to a dialog that
// never appeared would be unforgivable.

import type { project } from '../wailsjs/go/models';

export interface TreeCallbacks {
  onOpen(path: string): void;
  onCreate(parent: string, name: string, isDir: boolean): void;
  onRename(path: string, newName: string): void;
  onDelete(path: string, isDir: boolean): void;
  onSetRoot(path: string): void;
}

export interface TreeState {
  tree: project.Node;
  rootFile: string;
  openFile: string;
}

const ICONS: Record<string, string> = {
  tex: '𝑻',
  bib: '❝',
  image: '▣',
  text: '≡',
  binary: '·',
};

export class FileTree {
  private expanded = new Set<string>();
  private state: TreeState | null = null;
  // A pending create, shown as an inline input inside its parent folder.
  private creating: { parent: string; isDir: boolean } | null = null;
  private renaming: string | null = null;

  constructor(
    private container: HTMLElement,
    private callbacks: TreeCallbacks,
  ) {}

  /** The folder new entries should go into: the open file's folder. */
  get activeFolder(): string {
    const open = this.state?.openFile ?? '';
    const cut = open.lastIndexOf('/');
    return cut < 0 ? '' : open.slice(0, cut);
  }

  render(state: TreeState) {
    this.state = state;
    // Keep the open file's ancestors visible, so selecting a nested file does
    // not leave it hidden inside a collapsed folder.
    for (const part of ancestors(state.openFile)) this.expanded.add(part);
    this.draw();
  }

  beginCreate(isDir: boolean) {
    this.creating = { parent: this.activeFolder, isDir };
    if (this.creating.parent) this.expanded.add(this.creating.parent);
    this.draw();
  }

  beginRename(path: string) {
    this.renaming = path;
    this.draw();
  }

  private draw() {
    if (!this.state) return;
    this.container.replaceChildren(this.drawLevel(this.state.tree.children ?? [], '', 0));
    // A create at the top level has no folder row to attach to.
    if (this.creating && this.creating.parent === '') {
      this.container.append(this.inputRow(0, this.creating.isDir));
    }
  }

  private drawLevel(nodes: project.Node[], parent: string, depth: number): DocumentFragment {
    const frag = document.createDocumentFragment();
    for (const node of nodes) {
      frag.append(this.drawNode(node, depth));

      if (node.isDir && this.expanded.has(node.path)) {
        frag.append(this.drawLevel(node.children ?? [], node.path, depth + 1));
        if (this.creating && this.creating.parent === node.path) {
          frag.append(this.inputRow(depth + 1, this.creating.isDir));
        }
      }
    }
    if (parent === '' && depth > 0) return frag;
    return frag;
  }

  private drawNode(node: project.Node, depth: number): HTMLElement {
    if (this.renaming === node.path) {
      return this.inputRow(depth, node.isDir, node.name, node.path);
    }

    const row = document.createElement('div');
    row.className = 'row';
    if (node.path === this.state?.openFile) row.classList.add('selected');
    row.style.paddingLeft = `${8 + depth * 14}px`;

    const label = document.createElement('button');
    label.className = 'row-label';
    const icon = node.isDir
      ? this.expanded.has(node.path)
        ? '▾'
        : '▸'
      : (ICONS[node.kind] ?? '·');
    label.innerHTML = `<span class="icon">${icon}</span>`;
    label.append(node.name);
    label.addEventListener('click', () => {
      if (node.isDir) {
        this.expanded.has(node.path) ? this.expanded.delete(node.path) : this.expanded.add(node.path);
        this.draw();
      } else {
        this.callbacks.onOpen(node.path);
      }
    });
    row.append(label);

    // The compile entry point, marked because it is usually *not* the file being
    // edited and that is otherwise invisible.
    if (!node.isDir && node.path === this.state?.rootFile) {
      const badge = document.createElement('span');
      badge.className = 'badge';
      badge.textContent = 'root';
      badge.title = 'This is the document that gets compiled';
      row.append(badge);
    }

    row.append(this.actions(node));
    return row;
  }

  private actions(node: project.Node): HTMLElement {
    const wrap = document.createElement('span');
    wrap.className = 'row-actions';

    const button = (text: string, title: string, run: () => void) => {
      const b = document.createElement('button');
      b.className = 'icon-btn';
      b.textContent = text;
      b.title = title;
      b.addEventListener('click', (event) => {
        event.stopPropagation();
        run();
      });
      wrap.append(b);
    };

    if (node.kind === 'tex' && !node.isDir && node.path !== this.state?.rootFile) {
      button('★', 'Compile this document instead', () => this.callbacks.onSetRoot(node.path));
    }
    button('✎', 'Rename', () => this.beginRename(node.path));
    button('␡', 'Delete', () => this.callbacks.onDelete(node.path, node.isDir));
    return wrap;
  }

  /**
   * An inline text input, used for both creating and renaming. Enter commits,
   * Escape abandons, and losing focus abandons too — a half-typed name should
   * never turn into a file by accident.
   */
  private inputRow(depth: number, isDir: boolean, initial = '', renamePath?: string): HTMLElement {
    const row = document.createElement('div');
    row.className = 'row editing';
    row.style.paddingLeft = `${8 + depth * 14}px`;

    const icon = document.createElement('span');
    icon.className = 'icon';
    icon.textContent = isDir ? '▸' : '·';

    const input = document.createElement('input');
    input.className = 'row-input';
    input.value = initial;
    input.placeholder = isDir ? 'folder name' : 'file name.tex';
    input.spellcheck = false;

    let settled = false;
    const cancel = () => {
      if (settled) return;
      settled = true;
      this.creating = null;
      this.renaming = null;
      this.draw();
    };
    const commit = () => {
      if (settled) return;
      const name = input.value.trim();
      if (!name || name === initial) {
        cancel();
        return;
      }
      settled = true;
      const creating = this.creating;
      this.creating = null;
      this.renaming = null;
      if (renamePath) this.callbacks.onRename(renamePath, name);
      else if (creating) this.callbacks.onCreate(creating.parent, name, creating.isDir);
    };

    input.addEventListener('keydown', (event) => {
      if (event.key === 'Enter') {
        event.preventDefault();
        commit();
      } else if (event.key === 'Escape') {
        event.preventDefault();
        cancel();
      }
    });
    input.addEventListener('blur', cancel);

    row.append(icon, input);
    // Focus after the row is in the document.
    queueMicrotask(() => {
      input.focus();
      input.select();
    });
    return row;
  }
}

/** All ancestor folder paths of a file, so they can be expanded. */
function ancestors(path: string): string[] {
  const parts = path.split('/');
  parts.pop();
  const out: string[] = [];
  let acc = '';
  for (const part of parts) {
    acc = acc ? `${acc}/${part}` : part;
    out.push(acc);
  }
  return out;
}
