// The command palette.
//
// One overlay, four modes, chosen by a prefix character: `>` runs a command,
// `@` jumps to a heading, `:` jumps to a line, and a bare query opens a file.
// This is the shape VS Code, Sublime and every editor since have used, which
// means most of its discoverability is already in the user's hands.

import type { OutlineItem } from './latex/outline';

export interface PaletteAction {
  id: string;
  title: string;
  /** Shown right-aligned: a keyboard shortcut, or where the action leads. */
  hint?: string;
  run(): void;
}

export interface PaletteHost {
  actions(): PaletteAction[];
  files(): string[];
  sections(): OutlineItem[];
  openFile(path: string): void;
  goToLine(line: number): void;
}

interface Row {
  title: string;
  detail?: string;
  hint?: string;
  /** Indices of title characters that matched, for highlighting. */
  matched: number[];
  indent?: number;
  run(): void;
}

export interface Palette {
  open(prefix?: string): void;
  close(): void;
  isOpen(): boolean;
}

const MODES = [
  { prefix: '>', placeholder: 'Run a command…' },
  { prefix: '@', placeholder: 'Go to a heading…' },
  { prefix: ':', placeholder: 'Go to a line…' },
  { prefix: '', placeholder: 'Open a file… (> commands, @ headings, : line)' },
];

export function mountPalette(host: PaletteHost): Palette {
  const overlay = document.createElement('div');
  overlay.className = 'palette-overlay';
  overlay.hidden = true;
  overlay.innerHTML = `
    <div class="palette" role="dialog" aria-modal="true" aria-label="Command palette">
      <input class="palette-input" type="text" spellcheck="false" autocomplete="off" />
      <ul class="palette-list" role="listbox"></ul>
      <p class="palette-empty" hidden>Nothing matches.</p>
    </div>
  `;
  document.body.append(overlay);

  const input = overlay.querySelector<HTMLInputElement>('.palette-input')!;
  const list = overlay.querySelector<HTMLUListElement>('.palette-list')!;
  const empty = overlay.querySelector<HTMLParagraphElement>('.palette-empty')!;

  let rows: Row[] = [];
  let active = 0;

  function isOpen() {
    return !overlay.hidden;
  }

  function open(prefix = '') {
    overlay.hidden = false;
    input.value = prefix;
    input.focus();
    // Put the caret after the prefix so typing continues the query rather than
    // replacing the mode character.
    input.setSelectionRange(prefix.length, prefix.length);
    refresh();
  }

  function close() {
    overlay.hidden = true;
    input.value = '';
    list.replaceChildren();
  }

  function refresh() {
    const raw = input.value;
    const mode = MODES.find((m) => m.prefix && raw.startsWith(m.prefix)) ?? MODES[MODES.length - 1];
    const query = raw.slice(mode.prefix.length).trim();
    input.placeholder = mode.placeholder;

    rows = build(mode.prefix, query, host);
    active = 0;
    render();
  }

  function render() {
    list.replaceChildren();
    empty.hidden = rows.length > 0;

    rows.forEach((row, index) => {
      const item = document.createElement('li');
      item.className = 'palette-row' + (index === active ? ' active' : '');
      item.setAttribute('role', 'option');
      item.setAttribute('aria-selected', String(index === active));
      if (row.indent) item.style.paddingLeft = `${12 + row.indent * 14}px`;

      const title = document.createElement('span');
      title.className = 'palette-title';
      title.append(...highlight(row.title, row.matched));
      item.append(title);

      if (row.detail) {
        const detail = document.createElement('span');
        detail.className = 'palette-detail';
        detail.textContent = row.detail;
        item.append(detail);
      }
      if (row.hint) {
        const hint = document.createElement('span');
        hint.className = 'palette-hint';
        hint.textContent = row.hint;
        item.append(hint);
      }

      // mousedown, not click: the input must not lose focus before we run.
      item.addEventListener('mousedown', (event) => {
        event.preventDefault();
        choose(index);
      });
      list.append(item);
    });

    list.children[active]?.scrollIntoView({ block: 'nearest' });
  }

  function move(delta: number) {
    if (rows.length === 0) return;
    active = (active + delta + rows.length) % rows.length;
    render();
  }

  function choose(index: number) {
    const row = rows[index];
    if (!row) return;
    // Close first: an action that opens a dialog of its own must not fight the
    // palette for focus.
    close();
    row.run();
  }

  input.addEventListener('input', refresh);
  input.addEventListener('keydown', (event) => {
    switch (event.key) {
      case 'ArrowDown':
        event.preventDefault();
        move(1);
        break;
      case 'ArrowUp':
        event.preventDefault();
        move(-1);
        break;
      case 'Enter':
        event.preventDefault();
        choose(active);
        break;
      case 'Escape':
        event.preventDefault();
        close();
        break;
      // Ctrl-N / Ctrl-P, for hands that never leave the home row.
      case 'n':
        if (event.ctrlKey) {
          event.preventDefault();
          move(1);
        }
        break;
      case 'p':
        if (event.ctrlKey) {
          event.preventDefault();
          move(-1);
        }
        break;
    }
  });

  // A click outside is a dismissal; a click on the panel is not.
  overlay.addEventListener('mousedown', (event) => {
    if (event.target === overlay) close();
  });

  return { open, close, isOpen };
}

/** Builds the candidate rows for the active mode. */
function build(prefix: string, query: string, host: PaletteHost): Row[] {
  if (prefix === ':') {
    const line = Number.parseInt(query, 10);
    if (!Number.isFinite(line) || line < 1) return [];
    return [{ title: `Go to line ${line}`, matched: [], run: () => host.goToLine(line) }];
  }

  if (prefix === '@') {
    return rank(
      host.sections().map((section) => ({
        key: section.title,
        row: {
          title: section.title,
          hint: `line ${section.line}`,
          indent: section.level,
          matched: [],
          run: () => host.goToLine(section.line),
        },
      })),
      query,
    );
  }

  if (prefix === '>') {
    return rank(
      host.actions().map((action) => ({
        key: action.title,
        row: { title: action.title, hint: action.hint, matched: [], run: action.run },
      })),
      query,
    );
  }

  return rank(
    host.files().map((path) => {
      const cut = path.lastIndexOf('/');
      return {
        key: path,
        row: {
          title: cut < 0 ? path : path.slice(cut + 1),
          detail: cut < 0 ? undefined : path.slice(0, cut),
          matched: [],
          run: () => host.openFile(path),
        },
        // Match against the full path so "ch/int" finds chapters/intro.tex,
        // but highlight only the filename, which is what the row shows.
        titleOffset: cut < 0 ? 0 : cut + 1,
      };
    }),
    query,
  );
}

interface Candidate {
  key: string;
  row: Row;
  titleOffset?: number;
}

function rank(candidates: Candidate[], query: string): Row[] {
  if (!query) return candidates.slice(0, 200).map((c) => c.row);

  const scored: { score: number; row: Row }[] = [];
  for (const candidate of candidates) {
    const match = fuzzy(candidate.key, query);
    if (!match) continue;
    const offset = candidate.titleOffset ?? 0;
    scored.push({
      score: match.score,
      row: { ...candidate.row, matched: match.positions.map((p) => p - offset).filter((p) => p >= 0) },
    });
  }
  scored.sort((a, b) => b.score - a.score);
  return scored.slice(0, 200).map((s) => s.row);
}

/**
 * Subsequence match, scored so that better matches win.
 *
 * Consecutive characters and matches at a word boundary score higher, which is
 * what makes "gtl" prefer "Go To Line" over a file that merely contains those
 * letters scattered through it.
 */
export function fuzzy(text: string, query: string): { score: number; positions: number[] } | null {
  const haystack = text.toLowerCase();
  const needle = query.toLowerCase();

  const positions: number[] = [];
  let score = 0;
  let at = 0;
  let previous = -2;

  for (const char of needle) {
    if (char === ' ') continue;
    const found = haystack.indexOf(char, at);
    if (found < 0) return null;

    if (found === previous + 1) score += 8;
    if (found === 0 || /[^a-z0-9]/.test(haystack[found - 1])) score += 6;
    // Earlier matches are usually the ones meant.
    score += Math.max(0, 4 - found / 12);

    positions.push(found);
    previous = found;
    at = found + 1;
  }

  // A short candidate that used most of its characters is a better hit than a
  // long one that happened to contain them.
  score += Math.max(0, 20 - (text.length - needle.length) / 3);
  return { score, positions };
}

/** Splits a title into plain and matched runs. */
export function highlight(text: string, matched: number[]): Node[] {
  if (matched.length === 0) return [document.createTextNode(text)];

  const hits = new Set(matched);
  const nodes: Node[] = [];
  let buffer = '';
  let bufferHit = hits.has(0);

  const flush = () => {
    if (!buffer) return;
    if (bufferHit) {
      const mark = document.createElement('b');
      mark.textContent = buffer;
      nodes.push(mark);
    } else {
      nodes.push(document.createTextNode(buffer));
    }
    buffer = '';
  };

  for (let i = 0; i < text.length; i++) {
    const hit = hits.has(i);
    if (hit !== bufferHit) {
      flush();
      bufferHit = hit;
    }
    buffer += text[i];
  }
  flush();
  return nodes;
}
