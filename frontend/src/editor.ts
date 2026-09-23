// The editor pane.
//
// The language support is the legacy `stex` stream mode, which is enough for
// highlighting; completion is supplied separately, from the project index and
// the built-in LaTeX vocabulary, rather than from a grammar. A real Lezer
// grammar would buy structural selection and smarter indentation, and is the
// natural next step if either is wanted.

import { EditorView, keymap, lineNumbers, highlightActiveLine, tooltips } from '@codemirror/view';
import { EditorState } from '@codemirror/state';
import { StreamLanguage, indentUnit, bracketMatching, foldGutter } from '@codemirror/language';
import { stex } from '@codemirror/legacy-modes/mode/stex';
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands';
import {
  autocompletion,
  closeBrackets,
  closeBracketsKeymap,
  completionKeymap,
  startCompletion,
} from '@codemirror/autocomplete';
import { lintGutter, setDiagnostics, type Diagnostic as LintDiagnostic } from '@codemirror/lint';
import type { texlog } from '../wailsjs/go/models';
import { latexCompletions, type CompletionSources } from './latex/complete';
import { editorTheme, isDark, themeCompartment } from './theme';

export interface EditorOptions {
  doc: string;
  /** Called on Cmd-S / Ctrl-S. */
  onSave: () => void;
  /** Called whenever the document text changes. */
  onChange?: () => void;
  /** Called when the cursor moves onto a different line (1-based). */
  onCursorLine?: (line: number) => void;
  /** Where completion gets the project's labels, citations and files. */
  sources: CompletionSources;
}

export interface EditorHandle {
  view: EditorView;
  getDoc(): string;
  focus(): void;
  /** Replaces the inline markers with the results of the latest compile. */
  setProblems(diagnostics: texlog.Diagnostic[]): void;
  /** Scrolls to a 1-based line and puts the cursor on it. */
  goToLine(line: number): void;
  /** Swaps in a different file's contents, discarding history and markers. */
  setDoc(content: string): void;
  /** The 1-based line the cursor is on. */
  cursorLine(): number;
  /** Re-themes in place, so switching light/dark does not lose the buffer. */
  setDark(dark: boolean): void;
}

// The LaTeX-aware pairs. `$` is here because unbalanced maths delimiters are one
// of the easiest ways to produce a baffling compile error.
const texLanguage = StreamLanguage.define(stex);
const texBrackets = texLanguage.data.of({
  closeBrackets: { brackets: ['(', '[', '{', '$'] },
  commentTokens: { line: '%' },
});

export function mountEditor(parent: HTMLElement, opts: EditorOptions): EditorHandle {
  let lastCursorLine = 0;

  const view = new EditorView({
    parent,
    state: EditorState.create({
      doc: opts.doc,
      extensions: [
        lineNumbers(),
        foldGutter(),
        lintGutter(),
        highlightActiveLine(),
        history(),
        bracketMatching(),
        closeBrackets(),
        indentUnit.of('  '),
        // LaTeX prose is written as long lines with paragraph breaks, so
        // horizontal scrolling would hide most of a paragraph.
        EditorView.lineWrapping,
        texLanguage,
        texBrackets,
        // Render popups into the body. Inside the editor they are clipped by
        // the pane, which truncates long citation keys and hides the detail
        // column entirely.
        tooltips({ parent: document.body }),
        autocompletion({
          override: [latexCompletions(opts.sources)],
          // The list is LaTeX-specific and every entry is a backslash command,
          // an environment or a key; a column of identical icons would be pure
          // noise next to them.
          icons: false,
          activateOnTyping: true,
          closeOnBlur: true,
        }),
        // Before defaultKeymap, so Mod-s is ours. Returning true stops the
        // webview from opening its own "save page" dialog.
        keymap.of([
          {
            key: 'Mod-s',
            preventDefault: true,
            run: () => {
              opts.onSave();
              return true;
            },
          },
          // The conventional "complete this, now" key.
          { key: 'Mod-Space', preventDefault: true, run: startCompletion },
        ]),
        // Completion and bracket keys outrank the defaults, so Enter accepts a
        // completion instead of breaking the line, and Tab walks snippet fields
        // instead of indenting.
        keymap.of([...closeBracketsKeymap, ...completionKeymap]),
        keymap.of([...defaultKeymap, ...historyKeymap, indentWithTab]),
        EditorView.updateListener.of((update) => {
          if (update.docChanged) opts.onChange?.();
          // Only on a change of line: firing per keystroke would re-run a
          // forward search for every character typed.
          if (update.docChanged || update.selectionSet) {
            const line = update.state.doc.lineAt(update.state.selection.main.head).number;
            if (line !== lastCursorLine) {
              lastCursorLine = line;
              opts.onCursorLine?.(line);
            }
          }
        }),
        themeCompartment.of(editorTheme(isDark())),
      ],
    }),
  });

  return {
    view,
    getDoc: () => view.state.doc.toString(),
    focus: () => view.focus(),
    cursorLine: () => view.state.doc.lineAt(view.state.selection.main.head).number,
    setProblems: (diagnostics) => view.dispatch(setDiagnostics(view.state, toLint(view, diagnostics))),
    setDark: (dark) => view.dispatch({ effects: themeCompartment.reconfigure(editorTheme(dark)) }),
    setDoc: (content) => {
      view.dispatch({
        changes: { from: 0, to: view.state.doc.length, insert: content },
        // Markers belong to the file that was open a moment ago; keeping them
        // would put another file's errors on these lines.
        effects: [],
        selection: { anchor: 0 },
        scrollIntoView: true,
      });
      view.dispatch(setDiagnostics(view.state, []));
    },
    goToLine: (line) => {
      const pos = view.state.doc.line(clampLine(view, line)).from;
      view.dispatch({ selection: { anchor: pos }, scrollIntoView: true });
      view.focus();
    },
  };
}

function clampLine(view: EditorView, line: number): number {
  return Math.min(Math.max(line, 1), view.state.doc.lines);
}

/**
 * Converts engine diagnostics into CodeMirror markers.
 *
 * Only errors and warnings become markers. Overfull-box notices (severity
 * "info") are real but fire constantly while drafting, and a gutter that is
 * always lit is a gutter nobody reads — they stay in the problems list instead.
 */
function toLint(view: EditorView, diagnostics: texlog.Diagnostic[]): LintDiagnostic[] {
  const out: LintDiagnostic[] = [];

  for (const d of diagnostics) {
    if (d.severity !== 'error' && d.severity !== 'warning') continue;
    // A diagnostic the engine could not place anywhere is still shown in the
    // problems list; it just has no line to attach to.
    if (!d.line) continue;

    const line = view.state.doc.line(clampLine(view, d.line));

    // Underline the offending command when we know it, rather than lighting up
    // the whole line — the difference between "something here is wrong" and
    // "this is wrong".
    let { from, to } = { from: line.from, to: line.to };
    if (d.token) {
      const at = line.text.indexOf(d.token);
      if (at >= 0) {
        from = line.from + at;
        to = from + d.token.length;
      }
    }
    // An empty line has from === to, which CodeMirror will not render.
    if (from === to) to = Math.min(from + 1, view.state.doc.length);

    out.push({
      from,
      to,
      severity: d.severity,
      message: describe(d),
    });
  }
  return out;
}

/** Builds the hover text: what the engine said, then what it means. */
function describe(d: texlog.Diagnostic): string {
  const parts = [d.message];
  if (d.hint) parts.push(d.approximate ? `Probably: ${d.hint}` : d.hint);
  return parts.join('\n\n');
}
