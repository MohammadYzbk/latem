import { describe, expect, it } from 'vitest';
import { outline } from './outline';

describe('outline', () => {
  it('reads headings with their level and 1-based line', () => {
    const got = outline('\\section{One}\n\ntext\n\\subsection{Two}\n');
    expect(got).toEqual([
      { level: 2, title: 'One', line: 1 },
      { level: 3, title: 'Two', line: 4 },
    ]);
  });

  it('handles every division level', () => {
    const got = outline(
      '\\part{P}\n\\chapter{C}\n\\section{S}\n\\subsection{SS}\n\\subsubsection{SSS}\n\\paragraph{Pa}\n',
    );
    expect(got.map((i) => i.level)).toEqual([0, 1, 2, 3, 4, 5]);
  });

  it('accepts starred headings', () => {
    expect(outline('\\section*{Unnumbered}\n')[0].title).toBe('Unnumbered');
  });

  it('reads a title containing braces', () => {
    expect(outline('\\section{A \\textbf{bold} idea}\n')[0].title).toBe('A bold idea');
  });

  it('ignores a commented-out heading', () => {
    expect(outline('% \\section{Hidden}\n\\section{Real}\n')).toHaveLength(1);
  });

  it('treats an escaped percent as ordinary text', () => {
    expect(outline('\\section{100\\% better}\n')).toHaveLength(1);
  });

  // The outline reads the live buffer, so it sees half-typed headings; popping
  // the entry in only once the brace is closed would feel like a stutter.
  it('shows a heading whose brace is still open', () => {
    expect(outline('\\section{Half typed')[0].title).toBe('Half typed');
  });

  it('never returns an empty title', () => {
    expect(outline('\\section{}\n')[0].title).toBe('(untitled)');
  });

  it('returns nothing for a document with no headings', () => {
    expect(outline('\\documentclass{article}\n\\begin{document}\nhi\n\\end{document}\n')).toEqual([]);
  });
});
