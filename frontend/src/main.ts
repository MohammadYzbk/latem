// The app shell: file tree, editor, preview.
//
// The document that gets compiled is the project's root file, not whichever file
// is open — editing chapter3.tex has to compile the root, or the preview would
// show a fragment with no preamble.
//
// SyncTeX ties the two panes together in both directions: the marker follows the
// cursor into the PDF, and clicking the PDF moves the cursor.

import './style.css';
import { mountEditor, type EditorHandle } from './editor';
import { mountPreview } from './preview';
import { FileTree } from './tree';
import {
  Compile,
  ForwardSearch,
  InverseSearch,
  ConfirmDelete,
  CreateEntry,
  CurrentProject,
  DeleteEntry,
  EngineVersion,
  OpenFile,
  OpenProjectDialog,
  RenameEntry,
  SaveAndCompile,
  SetRootFile,
} from '../wailsjs/go/main/App';
import type { main, synctex, texlog } from '../wailsjs/go/models';

// How long to wait after the last keystroke before compiling.
//
// A warm compile takes 80–250 ms, so the delay — not the engine — is what the
// pause feels like. Too short and it fires mid-word, wasting runs and flashing
// errors for half-typed commands; too long and it stops feeling live. 600 ms
// sits just past a normal inter-word pause.
const IDLE_COMPILE_MS = 600;

document.querySelector('#app')!.innerHTML = `
  <header class="bar">
    <button class="btn btn-quiet" id="open-project" title="Open a different folder">
      <span id="project-name">…</span>
    </button>
    <span class="sep">/</span>
    <span class="path"><span class="dir" id="path-dir"></span><span class="file" id="path-file"></span></span>
    <span class="spacer"></span>
    <span class="status" id="status">loading…</span>
    <button class="btn" id="compile" title="Compile now (Cmd-S)">Compile</button>
    <button class="btn btn-quiet" id="sync" title="Show this line in the PDF (Cmd-J)">Find in PDF</button>
    <span class="engine" id="engine"></span>
  </header>
  <main class="panes">
    <aside class="sidebar">
      <div class="sidebar-head">
        <span>Files</span>
        <span class="spacer"></span>
        <button class="icon-btn" id="new-file" title="New file">＋</button>
        <button class="icon-btn" id="new-folder" title="New folder">＋▸</button>
      </div>
      <div class="tree" id="tree"></div>
      <div class="root-note" id="root-note"></div>
    </aside>
    <section class="pane" id="editor-pane"></section>
    <section class="pane preview">
      <div class="pdf-scroll" id="pdf"><p class="empty">Compiling…</p></div>
    </section>
  </main>
  <footer class="drawer">
    <div class="drawer-head">
      <span id="drawer-title">No problems</span>
      <span class="spacer"></span>
      <button class="btn btn-quiet" id="toggle-log">Show raw log</button>
    </div>
    <div class="drawer-body" id="drawer-body" hidden>
      <ul class="problems" id="problems"></ul>
      <pre class="rawlog" id="log" hidden></pre>
    </div>
  </footer>
`;

const el = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;
const statusEl = el('status');
const problemsEl = el('problems');
const logEl = el('log');
const pdfEl = el('pdf');
const editorPane = el('editor-pane');

// SyncTeX, both directions. A click on the page resolves to a source line; the
// cursor's line resolves to a place on the page.
const preview = mountPreview(pdfEl, {
  onPointClicked: (page, x, y) => void jumpToSource(page, x, y),
});

let syncAvailable = false;
let syncTimer: number | undefined;

type Status = 'clean' | 'dirty' | 'compiling' | 'failed' | 'broken';

function setStatus(kind: Status, text: string) {
  statusEl.className = `status ${kind}`;
  statusEl.textContent = text;
}

// --- state -------------------------------------------------------------------

let editor: EditorHandle | null = null;
let openFile = '';
// Diagnostics from the last compile, across the whole project. The editor only
// ever shows the ones belonging to the file on screen.
let diagnostics: texlog.Diagnostic[] = [];

let compiling = false;
let pending = false;
let idleTimer: number | undefined;

const tree = new FileTree(el('tree'), {
  onOpen: (path) => void openPath(path),
  onCreate: (parent, name, isDir) => void applyProject(CreateEntry(parent, name, isDir), { reopen: !isDir }),
  onRename: (path, name) => void applyProject(RenameEntry(path, name), { reopen: true }),
  onDelete: (path, isDir) => void confirmAndDelete(path, isDir),
  onSetRoot: (path) => void applyProject(SetRootFile(path), { recompile: true }),
});

// --- project and files -------------------------------------------------------

function showPath(path: string) {
  const cut = path.lastIndexOf('/');
  el('path-dir').textContent = cut < 0 ? '' : path.slice(0, cut + 1);
  el('path-file').textContent = cut < 0 ? path : path.slice(cut + 1);
}

function renderProject(info: main.ProjectInfo) {
  el('project-name').textContent = info.name || 'No project';
  tree.render({ tree: info.tree, rootFile: info.rootFile, openFile });

  // Say plainly which document is compiled, since it is usually not this one.
  const note = el('root-note');
  if (!info.rootFile) {
    note.textContent = 'No \\documentclass found — nothing to compile yet.';
    note.className = 'root-note warn';
  } else if (info.rootFile !== openFile) {
    note.textContent = `Compiling ${info.rootFile}`;
    note.className = 'root-note';
  } else {
    note.textContent = '';
    note.className = 'root-note';
  }

  if (info.error) setStatus('broken', info.error);
}

/** Runs a project-mutating call, then refreshes the tree. */
async function applyProject(
  call: Promise<main.ProjectInfo>,
  opts: { reopen?: boolean; recompile?: boolean } = {},
) {
  const info = await call;
  if (info.error) {
    setStatus('broken', info.error);
  }
  // The backend decides what is open now: a create selects the new file, a
  // delete falls back to the root document.
  if (opts.reopen && info.openFile && info.openFile !== openFile) {
    await openPath(info.openFile, info);
    return;
  }
  renderProject(info);
  if (opts.recompile) await compileNow();
}

async function confirmAndDelete(path: string, isDir: boolean) {
  if (!(await ConfirmDelete(path, isDir))) return;
  await applyProject(DeleteEntry(path), { reopen: true, recompile: true });
}

/** Opens a file in the editor, or shows a figure in place of it. */
async function openPath(path: string, info?: main.ProjectInfo) {
  const file = await OpenFile(path);
  if (file.error) {
    setStatus('broken', file.error);
    return;
  }

  openFile = path;
  showPath(path);

  if (file.kind === 'image') {
    showImage(file.url, path);
  } else if (file.kind === 'binary') {
    showMessage(`${path} is not a text file, so there is nothing to edit.`);
  } else {
    showEditor(file.content);
  }

  renderProject(info ?? (await CurrentProject()));
  applyDiagnosticsToEditor();
}

function showEditor(content: string) {
  if (editor) {
    editor.setDoc(content);
    editor.focus();
    return;
  }
  editorPane.replaceChildren();
  editor = mountEditor(editorPane, {
    doc: content,
    onSave: () => void compileNow(),
    onChange: () => {
      if (!compiling) setStatus('dirty', 'editing…');
      scheduleCompile();
    },
    onCursorLine: (line) => scheduleSync(line),
  });
  editor.focus();
}

/** Replaces the editor with a non-editable view; the editor is rebuilt on the
 * next text file, which also discards its history for the old buffer. */
function replaceEditorWith(node: HTMLElement) {
  editor = null;
  editorPane.replaceChildren(node);
}

function showImage(url: string, path: string) {
  const wrap = document.createElement('div');
  wrap.className = 'figure-view';
  const img = document.createElement('img');
  img.src = url;
  img.alt = path;
  const caption = document.createElement('p');
  caption.className = 'empty';
  caption.textContent = path;
  wrap.append(img, caption);
  replaceEditorWith(wrap);
}

function showMessage(text: string) {
  const p = document.createElement('p');
  p.className = 'empty';
  p.textContent = text;
  replaceEditorWith(p);
}

// --- the compile loop --------------------------------------------------------

function scheduleCompile() {
  window.clearTimeout(idleTimer);
  idleTimer = window.setTimeout(() => void compileNow(), IDLE_COMPILE_MS);
}

async function compileNow(): Promise<void> {
  window.clearTimeout(idleTimer);
  if (compiling) {
    pending = true;
    return;
  }
  compiling = true;
  setStatus('compiling', 'compiling…');

  try {
    // Only a text buffer has anything to save; a figure on screen must not
    // overwrite the file it came from.
    const res: main.CompileResult = editor
      ? await SaveAndCompile(openFile, editor.getDoc())
      : await Compile();

    if (res.error) {
      setStatus('broken', res.error);
      diagnostics = res.diagnostics ?? [];
      renderDrawer(diagnostics, res.log);
      applyDiagnosticsToEditor();
      pinnedView = 'log';
      showView('log', true);
      return;
    }

    diagnostics = res.diagnostics ?? [];
    applyDiagnosticsToEditor();
    renderDrawer(diagnostics, res.log);

    const counts = tally(diagnostics);
    if (res.compiled) {
      setStatus('clean', `compiled ${res.rootFile} in ${res.durationMs} ms${counts ? ` · ${counts}` : ''}`);
    } else {
      const stale = res.pdfStale ? ' · preview is from the last good compile' : '';
      const explained = diagnostics.some((d) => d.severity === 'error');
      if (explained) {
        setStatus('failed', `${counts} · ${res.durationMs} ms${stale}`);
      } else {
        // The parser covers the common cases, and this is not one of them — a
        // broken image, a bad font, something from a tool downstream of TeX.
        // Reporting "1 warning" here would imply the document was fine, so say
        // it failed and open the log, which is the only thing that can explain
        // why.
        setStatus('failed', `compile failed for a reason we could not parse · ${res.durationMs} ms${stale}`);
        pinnedView = 'log';
        showView('log', true);
      }
    }

    syncAvailable = res.hasSynctex;
    if (res.pdfUrl) {
      try {
        await preview.load(res.pdfUrl);
        // Positions from the previous compile are gone with the old render, so
        // put the marker back where the cursor now is.
        if (editor) syncToCursor(editor.cursorLine(), false);
      } catch (err) {
        setStatus('broken', `preview failed: ${err}`);
        console.error(err);
      }
    } else {
      // Never compiled successfully yet, so there is nothing to show. Saying so
      // beats leaving "Compiling…" on screen as though it were still working.
      preview.showMessage(
        res.compiled
          ? 'The engine produced no pages.'
          : 'No preview yet — fix the errors below and it will appear.',
      );
    }
  } catch (err) {
    setStatus('broken', String(err));
    console.error(err);
  } finally {
    compiling = false;
    if (pending) {
      pending = false;
      void compileNow();
    }
  }
}

/** Markers belong to one file; showing another file's errors on these lines
 * would point at innocent code. */
function applyDiagnosticsToEditor() {
  editor?.setProblems(diagnostics.filter((d) => d.file === openFile));
}

// --- SyncTeX: source <-> PDF -------------------------------------------------

// Long enough that moving through a document with the arrow keys does not fire a
// search per line, short enough that it feels like the marker is following along.
const SYNC_IDLE_MS = 180;

function scheduleSync(line: number) {
  window.clearTimeout(syncTimer);
  syncTimer = window.setTimeout(() => syncToCursor(line, false), SYNC_IDLE_MS);
}

/**
 * Forward search: mark where the cursor's line ended up in the PDF.
 *
 * `center` distinguishes following along from being asked to go there. While
 * typing, the marker moves but the page only scrolls if the spot is off-screen —
 * a preview that re-centres itself on every cursor movement is exhausting to
 * write against. Cmd-J means "take me there", so it always scrolls.
 */
function syncToCursor(line: number, center: boolean) {
  if (!syncAvailable || !editor || !openFile) return;

  void (async () => {
    let rects: synctex.Rect[];
    try {
      rects = await ForwardSearch(openFile, line);
    } catch (err) {
      console.error(err);
      return;
    }
    if (!rects || rects.length === 0) {
      // Perfectly normal: comments, blank preamble lines and macro definitions
      // never appear on a page. Clear the marker rather than leaving a stale one
      // implying this line is somewhere it is not.
      preview.clearHighlight();
      if (center) setStatus('clean', `line ${line} produces no output in the PDF`);
      return;
    }
    preview.highlight(rects);
    preview.reveal(rects[0], center);
  })();
}

/** Inverse search: a click on the page takes the cursor to the line behind it. */
async function jumpToSource(page: number, x: number, y: number) {
  if (!syncAvailable) return;

  let loc: main.SourceLocation;
  try {
    loc = await InverseSearch(page, x, y);
  } catch (err) {
    console.error(err);
    return;
  }
  if (loc.error) {
    setStatus('broken', loc.error);
    return;
  }

  // Show what was matched, so a surprising jump is explainable rather than
  // just wrong-feeling.
  preview.highlight([loc.rect]);

  if (loc.file !== openFile) {
    await openPath(loc.file);
  }
  editor?.goToLine(loc.line);
}

// --- the problems drawer -----------------------------------------------------

type View = 'problems' | 'log';

let pinnedView: View | null = null;
let currentView: View = 'log';
let bodyOpen = false;

function renderDrawer(diags: texlog.Diagnostic[], rawLog: string) {
  problemsEl.replaceChildren();

  for (const d of diags) {
    const item = document.createElement('li');
    item.className = `problem ${d.severity}`;

    const where = document.createElement('button');
    where.className = 'where';
    // The file matters now that a project spans many: an error in chapter 3 is
    // not the same as one in the root.
    const place = d.file && d.file !== openFile ? shortName(d.file) : '';
    where.textContent = d.line
      ? `${place ? `${place}:` : 'line '}${d.line}${d.approximate ? '?' : ''}`
      : place || '—';
    if (d.line) {
      where.title = d.approximate
        ? 'The engine gave no location; this is our best guess'
        : `Jump to ${d.file || openFile} line ${d.line}`;
      where.addEventListener('click', () => void jumpTo(d));
    } else {
      where.disabled = true;
      where.title = 'The engine reported no location for this problem';
    }

    const text = document.createElement('span');
    text.className = 'problem-text';
    text.append(d.message);
    if (d.hint) {
      const hint = document.createElement('span');
      hint.className = 'hint';
      hint.textContent = d.approximate ? `Probably: ${d.hint}` : d.hint;
      text.append(hint);
    }

    item.append(where, text);
    problemsEl.append(item);
  }

  logEl.textContent = rawLog.trim();
  el('drawer-title').textContent = tally(diags) || 'No problems';

  const view = pinnedView ?? (diags.length > 0 ? 'problems' : 'log');
  showView(view, diags.length > 0 || pinnedView === 'log');
}

/** Jumping to a problem in another file has to open that file first. */
async function jumpTo(d: texlog.Diagnostic) {
  if (d.file && d.file !== openFile) {
    await openPath(d.file);
  }
  editor?.goToLine(d.line);
}

function shortName(path: string): string {
  const cut = path.lastIndexOf('/');
  return cut < 0 ? path : path.slice(cut + 1);
}

function tally(diags: texlog.Diagnostic[]): string {
  const n = (s: string) => diags.filter((d) => d.severity === s).length;
  const plural = (count: number, word: string) => `${count} ${word}${count === 1 ? '' : 's'}`;
  const parts: string[] = [];
  if (n('error')) parts.push(plural(n('error'), 'error'));
  if (n('warning')) parts.push(plural(n('warning'), 'warning'));
  if (n('info')) parts.push(plural(n('info'), 'note'));
  return parts.join(' · ');
}

function showView(view: View, open: boolean) {
  currentView = view;
  bodyOpen = open;
  el('drawer-body').hidden = !open;
  logEl.hidden = view !== 'log';
  problemsEl.hidden = view !== 'problems';

  // The button says what clicking it will do. While the drawer is shut that is
  // "open this view"; once open, it is "switch to the other one".
  const target: View = open ? (view === 'log' ? 'problems' : 'log') : view;
  el('toggle-log').textContent = target === 'log' ? 'Show raw log' : 'Show problems';
}

el('toggle-log').addEventListener('click', () => {
  pinnedView = bodyOpen ? (currentView === 'log' ? 'problems' : 'log') : currentView;
  showView(pinnedView, true);
});

// --- wiring ------------------------------------------------------------------

el('compile').addEventListener('click', () => void compileNow());
el('sync').addEventListener('click', () => {
  if (editor) syncToCursor(editor.cursorLine(), true);
});
el('new-file').addEventListener('click', () => tree.beginCreate(false));
el('new-folder').addEventListener('click', () => tree.beginCreate(true));
el('open-project').addEventListener('click', () => {
  void (async () => {
    const info = await OpenProjectDialog();
    openFile = '';
    await afterProjectChange(info);
  })();
});

// Cmd-S works even when focus is in the tree, not the editor.
window.addEventListener('keydown', (event) => {
  if (!(event.metaKey || event.ctrlKey)) return;
  if (event.key === 's') {
    event.preventDefault();
    void compileNow();
  }
  // Cmd-J: take me to this line in the PDF.
  if (event.key === 'j') {
    event.preventDefault();
    if (editor) syncToCursor(editor.cursorLine(), true);
  }
});

async function afterProjectChange(info: main.ProjectInfo) {
  renderProject(info);
  const toOpen = info.openFile || info.rootFile;
  if (toOpen) {
    await openPath(toOpen, info);
  } else {
    showMessage('This folder has no document yet. Use ＋ to create one.');
  }
  await compileNow();
}

async function init() {
  EngineVersion()
    .then((v) => (el('engine').textContent = v))
    .catch((err) => console.error(err));

  await afterProjectChange(await CurrentProject());
}

void init();
