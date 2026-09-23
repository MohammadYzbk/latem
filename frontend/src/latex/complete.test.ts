import { describe, expect, it } from 'vitest';
import { EditorState } from '@codemirror/state';
import { CompletionContext, type CompletionResult } from '@codemirror/autocomplete';
import { consumeClosingBrace, latexCompletions, type CompletionSources } from './complete';
import type { project } from '../../wailsjs/go/models';

/** Builds a project index; `|` in the doc marks the cursor. */
function sourcesFrom(partial: Partial<project.Symbols> = {}): CompletionSources {
  return {
    symbols: () =>
      ({
        labels: [],
        citations: [],
        commands: [],
        environments: [],
        error: '',
        ...partial,
      }) as project.Symbols,
    files: () => ['main.tex', 'chapters/intro.tex', 'refs.bib', 'figures/plot.png'],
  };
}

function complete(doc: string, sources = sourcesFrom()): CompletionResult | null {
  const pos = doc.indexOf('|');
  if (pos < 0) throw new Error('the test document must mark the cursor with |');
  const state = EditorState.create({ doc: doc.replace('|', '') });
  return latexCompletions(sources)(new CompletionContext(state, pos, true));
}

function labels(result: CompletionResult | null): string[] {
  return (result?.options ?? []).map((o) => o.label);
}

describe('context detection', () => {
  it('offers environments inside \\begin', () => {
    expect(labels(complete('\\begin{ali|'))).toContain('align');
  });

  it('offers environments inside \\begin when closeBrackets already added the brace', () => {
    // This is what the editor actually contains as you type: `{` auto-inserts
    // its `}`, so the cursor sits between them.
    expect(labels(complete('\\begin{ali|}'))).toContain('align');
  });

  it('offers labels inside \\ref, and nothing else', () => {
    const result = complete(
      '\\ref{|}',
      sourcesFrom({ labels: [{ key: 'sec:intro', file: 'main.tex', line: 3, section: 'Introduction' }] as project.Label[] }),
    );
    expect(labels(result)).toEqual(['sec:intro']);
  });

  it('offers citations inside \\cite', () => {
    const result = complete(
      '\\cite{|}',
      sourcesFrom({
        citations: [{ key: 'knuth1984', file: 'refs.bib', line: 1, type: 'article', title: 'T', author: 'Knuth', year: '1984' }] as project.Citation[],
      }),
    );
    expect(labels(result)).toEqual(['knuth1984']);
  });

  it('completes only the segment after the last comma in a \\cite list', () => {
    const result = complete(
      '\\cite{a,kn|}',
      sourcesFrom({
        citations: [{ key: 'knuth1984', file: 'refs.bib', line: 1, type: 'article', title: '', author: '', year: '' }] as project.Citation[],
      }),
    );
    // `from` must point at "kn", not at the start of the whole argument, or
    // accepting would wipe out the first key.
    expect(result?.from).toBe('\\cite{a,'.length);
  });

  it('offers project files inside \\input, without the .tex extension', () => {
    const got = labels(complete('\\input{|}'));
    expect(got).toContain('chapters/intro');
    expect(got).not.toContain('figures/plot.png');
  });

  it('offers images inside \\includegraphics, with the extension', () => {
    expect(labels(complete('\\includegraphics{|}'))).toContain('figures/plot.png');
  });

  it('offers commands after a bare backslash', () => {
    expect(labels(complete('\\sect|'))).toContain('section');
  });

  it('stays silent inside a comment', () => {
    expect(complete('% \\ref{|}')).toBeNull();
  });

  it('is not fooled by a completed argument earlier on the line', () => {
    // `\ref{a}` is closed, so this is a bare command, not a \ref argument.
    expect(labels(complete('\\ref{a} \\sect|'))).toContain('section');
  });
});

describe('\\end', () => {
  it('offers the enclosing environment first', () => {
    const result = complete('\\begin{itemize}\n\\item x\n\\end{|}');
    expect(labels(result)[0]).toBe('itemize');
  });

  it('leaves the auto-inserted brace alone', () => {
    // `to` must stay at the cursor: \end supplies no brace of its own.
    expect(complete('\\begin{itemize}\n\\end{|}')?.to).toBeUndefined();
  });
});

describe('\\begin consumes the auto-inserted brace', () => {
  it('extends the range over a `}` sitting at the cursor', () => {
    expect(consumeClosingBrace('}', 10)).toBe(11);
  });

  it('leaves the range alone when there is no brace to consume', () => {
    expect(consumeClosingBrace('', 10)).toBe(10);
    expect(consumeClosingBrace('x', 10)).toBe(10);
  });

  // The range itself must stay put: one that spans the brace makes validFor
  // see a `}`, fail, and drop the open completion on the next keystroke.
  it('does not widen the returned range, which would break validFor', () => {
    const result = complete('\\begin{ali|}');
    // Undefined means "ends at the cursor", which is what keeps validFor honest.
    expect(result?.to).toBeUndefined();
    expect('align}'.match(result!.validFor as RegExp)).toBeNull();
  });

  it('applies the block through a snippet', () => {
    const option = complete('\\begin{ali|}')?.options.find((o) => o.label === 'align');
    expect(typeof option?.apply).toBe('function');
  });
});

describe('project symbols outrank built-ins', () => {
  it('boosts a user-defined macro above the built-in vocabulary', () => {
    const result = complete(
      '\\vec|',
      sourcesFrom({ commands: [{ name: 'vect', file: 'defs.tex', line: 1, args: 1 }] as project.Macro[] }),
    );
    const mine = result?.options.find((o) => o.label === 'vect');
    const builtin = result?.options.find((o) => o.label === 'vec');
    expect(mine?.boost ?? 0).toBeGreaterThan(builtin?.boost ?? 0);
  });
});
