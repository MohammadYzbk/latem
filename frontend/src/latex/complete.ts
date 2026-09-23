// LaTeX completion.
//
// The hard part is not the word list, it is knowing what the cursor is asking
// for. Inside \ref{ you want labels and nothing else; inside \cite{ you want
// bib keys; after a bare backslash you want commands. Offering all three
// everywhere produces a list nobody reads, so each context answers on its own.

import {
  snippet,
  snippetCompletion,
  type Completion,
  type CompletionContext,
  type CompletionResult,
} from '@codemirror/autocomplete';
import type { EditorState } from '@codemirror/state';
import type { project } from '../../wailsjs/go/models';
import {
  CITATION_COMMANDS,
  COMMANDS,
  ENVIRONMENTS,
  FILE_COMMANDS,
  REFERENCE_COMMANDS,
} from './data';

/** What completion needs to know about the project around the buffer. */
export interface CompletionSources {
  symbols(): project.Symbols | null;
  /** Project-relative paths of every file, for \input and \includegraphics. */
  files(): string[];
}

// Boosts. CodeMirror sorts by boost first, then by match quality, so these only
// break ties between things that already matched what was typed.
const BOOST_PROJECT = 3; // your own labels, citations and macros
const BOOST_COMMON = 1; // the handful of commands typed constantly
const BOOST_MATH_OUT = -1; // \alpha is real, just rarely what you meant in prose

/**
 * Matches a partially typed argument: a command, optional [options], an open
 * brace, and whatever has been typed since. The inner group forbids braces so
 * a complete `\ref{a}` earlier on the line cannot be mistaken for an open one.
 */
const ARGUMENT_RE = /\\([A-Za-z@]+)\s*(?:\[[^\]]*\])?\{([^{}]*)$/;

/** Matches a bare command being typed. */
const COMMAND_RE = /\\([A-Za-z@]*)$/;

export function latexCompletions(sources: CompletionSources) {
  return (context: CompletionContext): CompletionResult | null => {
    const line = context.state.doc.lineAt(context.pos);
    const before = line.text.slice(0, context.pos - line.from);

    // A comment is prose about the document, not part of it.
    if (isCommented(before)) return null;

    const argument = ARGUMENT_RE.exec(before);
    if (argument) {
      const [, command, typed] = argument;
      const result = completeArgument(context, command, typed, sources);
      if (result) return result;
    }

    const bare = COMMAND_RE.exec(before);
    if (bare) {
      // An explicit request on a lone "\" should still open the list; an
      // automatic one should wait for a letter rather than firing on every
      // escape character typed.
      if (!context.explicit && bare[1].length === 0) return null;
      return {
        from: context.pos - bare[1].length,
        options: commandOptions(context.state, context.pos, sources),
        validFor: /^[A-Za-z@]*$/,
      };
    }

    return null;
  };
}

/**
 * Extends a replacement range over an auto-inserted `}`.
 *
 * Typing `\begin{` leaves the buffer as `\begin{|}` because closeBrackets adds
 * the closing brace. A \begin completion inserts the whole block including its
 * own `}`, so it has to swallow that one or the block ends `\end{itemize}}`.
 *
 * This happens when the completion is applied rather than in the returned
 * range: a range that spans the brace makes `validFor` see a `}`, fail its own
 * pattern, and drop the open completion on the next keystroke.
 */
export function consumeClosingBrace(after: string, to: number): number {
  return after === '}' ? to + 1 : to;
}

/** Completes inside the braces of a command that takes a known kind of value. */
function completeArgument(
  context: CompletionContext,
  command: string,
  typed: string,
  sources: CompletionSources,
): CompletionResult | null {
  // \cite takes a comma-separated list, so only the segment after the last
  // comma is being typed.
  const comma = typed.lastIndexOf(',');
  const segment = comma < 0 ? typed : typed.slice(comma + 1);
  const from = context.pos - segment.trimStart().length;

  if (command === 'begin' || command === 'end') {
    return { from, options: environmentOptions(context.state, context.pos, command, sources), validFor: /^[^{}]*$/ };
  }
  if (REFERENCE_COMMANDS.includes(command)) {
    return { from, options: labelOptions(sources), validFor: /^[^{},]*$/ };
  }
  if (CITATION_COMMANDS.includes(command)) {
    return { from, options: citationOptions(sources), validFor: /^[^{},]*$/ };
  }
  if (FILE_COMMANDS.includes(command)) {
    return { from, options: fileOptions(command, sources), validFor: /^[^{},]*$/ };
  }
  return null;
}

// --- project-derived options -------------------------------------------------

function labelOptions(sources: CompletionSources): Completion[] {
  const symbols = sources.symbols();
  if (!symbols?.labels) return [];

  return symbols.labels.map((label) => ({
    label: label.key,
    type: 'variable',
    // The key is often opaque; the heading it sits under is what identifies it.
    detail: label.section || shortName(label.file),
    info: `${label.file}:${label.line}`,
    boost: BOOST_PROJECT,
  }));
}

function citationOptions(sources: CompletionSources): Completion[] {
  const symbols = sources.symbols();
  if (!symbols?.citations) return [];

  return symbols.citations.map((citation) => {
    const who = citation.author ? firstAuthor(citation.author) : '';
    const when = citation.year ? ` ${citation.year}` : '';
    return {
      label: citation.key,
      type: 'constant',
      detail: who ? `${who}${when}` : citation.type,
      info: citation.title || undefined,
      boost: BOOST_PROJECT,
    };
  });
}

function fileOptions(command: string, sources: CompletionSources): Completion[] {
  // \input and friends resolve .tex without the extension, which is how
  // everyone writes them; \includegraphics wants the real filename.
  const wantsTeX = command !== 'includegraphics';

  return sources
    .files()
    .filter((path) => (wantsTeX ? path.endsWith('.tex') || path.endsWith('.bib') : !path.endsWith('.tex')))
    .map((path) => {
      const label = wantsTeX ? path.replace(/\.(tex|bib)$/, '') : path;
      return { label, type: 'text', detail: shortName(path), boost: BOOST_PROJECT };
    });
}

// --- environments ------------------------------------------------------------

function environmentOptions(
  state: EditorState,
  pos: number,
  command: string,
  sources: CompletionSources,
): Completion[] {
  const userDefined = sources.symbols()?.environments ?? [];
  const options: Completion[] = [];

  // \end{ almost always means "close the thing I am inside". Putting that first
  // turns a lookup into a keystroke.
  if (command === 'end') {
    const open = enclosingEnvironment(state, pos);
    if (open) {
      options.push({ label: open, type: 'keyword', detail: 'close this block', boost: BOOST_PROJECT + 2 });
    }
  }

  for (const env of userDefined) {
    options.push({ label: env.name, type: 'type', detail: `defined in ${shortName(env.file)}`, boost: BOOST_PROJECT });
  }

  for (const env of ENVIRONMENTS) {
    if (command === 'end') {
      options.push({ label: env.name, type: 'type', detail: env.detail });
      continue;
    }
    // On \begin, complete the whole block — matching \end included — because
    // the closing tag is pure bookkeeping nobody wants to type.
    const template = blockSnippet(env.name, env.body);
    options.push({
      label: env.name,
      type: 'type',
      detail: env.detail,
      boost: env.common ? BOOST_COMMON : 0,
      apply: (view, completion, from, to) =>
        snippet(template)(view, completion, from, consumeClosingBrace(view.state.sliceDoc(to, to + 1), to)),
    });
  }

  return options;
}

/**
 * Builds the snippet for a whole environment.
 *
 * The leading `}` closes the brace the user already opened by typing `\begin{`,
 * which is why this starts mid-construct rather than at the backslash.
 */
function blockSnippet(name: string, body?: string): string {
  return `${name}}\n${body ?? '  #{}'}\n\\end{${name}}`;
}

/** The innermost environment still open at pos, if any. */
function enclosingEnvironment(state: EditorState, pos: number): string | null {
  const text = state.doc.sliceString(0, pos);
  const open: string[] = [];
  const re = /\\(begin|end)\s*\{([^}]*)\}/g;

  for (let m = re.exec(text); m; m = re.exec(text)) {
    if (m[1] === 'begin') open.push(m[2]);
    else if (open[open.length - 1] === m[2]) open.pop();
  }
  return open.length ? open[open.length - 1] : null;
}

// --- commands ----------------------------------------------------------------

function commandOptions(state: EditorState, pos: number, sources: CompletionSources): Completion[] {
  const math = inMath(state, pos);
  const options: Completion[] = [];

  for (const macro of sources.symbols()?.commands ?? []) {
    options.push(
      snippetCompletion(macro.args > 0 ? `${macro.name}${'{#{}}'.repeat(macro.args)}` : macro.name, {
        label: macro.name,
        type: 'macro',
        detail: `yours · ${shortName(macro.file)}`,
        boost: BOOST_PROJECT,
      }),
    );
  }

  for (const command of COMMANDS) {
    // Math commands stay available outside maths rather than vanishing: the
    // context guess is a heuristic, and a wrong guess that hides \alpha while
    // you are defining a macro is worse than one that merely ranks it lower.
    let boost = 0;
    if (command.common) boost = BOOST_COMMON;
    if (command.math && !math) boost = BOOST_MATH_OUT;
    if (command.math && math) boost = BOOST_COMMON;

    options.push(
      snippetCompletion(command.snippet ?? command.name, {
        label: command.name,
        type: 'function',
        detail: command.detail,
        boost,
      }),
    );
  }

  return options;
}

/**
 * Whether pos sits inside maths.
 *
 * Counts unescaped `$` from the start of the document and checks for an
 * enclosing display-maths environment. Both are approximations — `$` inside a
 * verbatim block will fool the first — and both only affect ranking.
 */
function inMath(state: EditorState, pos: number): boolean {
  const env = enclosingEnvironment(state, pos);
  if (env && /^(equation|align|gather|multline|split|cases|[pbvBV]?matrix|displaymath|eqnarray)\*?$/.test(env)) {
    return true;
  }

  const text = state.doc.sliceString(0, pos);
  let dollars = 0;
  for (let i = 0; i < text.length; i++) {
    if (text[i] !== '$') continue;
    if (i > 0 && text[i - 1] === '\\' && !isEscapedBackslash(text, i - 1)) continue;
    dollars++;
  }
  if (dollars % 2 === 1) return true;

  // \[ ... \] with no closing bracket yet.
  const openDisplay = text.lastIndexOf('\\[');
  return openDisplay >= 0 && openDisplay > text.lastIndexOf('\\]');
}

/** True when the backslash at index is itself escaped (`\\$` is a literal). */
function isEscapedBackslash(text: string, index: number): boolean {
  let slashes = 0;
  for (let i = index; i >= 0 && text[i] === '\\'; i--) slashes++;
  return slashes % 2 === 0;
}

/** Whether the cursor sits after an unescaped `%`. */
function isCommented(before: string): boolean {
  for (let i = 0; i < before.length; i++) {
    if (before[i] !== '%') continue;
    let slashes = 0;
    for (let j = i - 1; j >= 0 && before[j] === '\\'; j--) slashes++;
    if (slashes % 2 === 0) return true;
  }
  return false;
}

function shortName(path: string): string {
  const cut = path.lastIndexOf('/');
  return cut < 0 ? path : path.slice(cut + 1);
}

/** "Knuth, Donald and Lamport, Leslie" -> "Knuth et al." */
function firstAuthor(author: string): string {
  const names = author.split(/\s+and\s+/);
  const first = names[0].split(',')[0].trim();
  return names.length > 1 ? `${first} et al.` : first;
}
