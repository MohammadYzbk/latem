// The built-in LaTeX vocabulary offered by completion.
//
// This is deliberately a curated list rather than an exhaustive one. A dropdown
// containing every control sequence in TeX is a dropdown you scroll past; the
// useful set is the few hundred things people actually type, with the awkward
// ones (\frac, \begin{tabular}, \includegraphics) carrying their argument
// structure so you never hand-balance the braces.
//
// `snippet` uses CodeMirror's syntax: #{name} marks a placeholder, #{} the
// final cursor stop.

export interface CommandSpec {
  /** Name without the leading backslash. */
  name: string;
  /** Snippet body, also without the backslash. Defaults to the bare name. */
  snippet?: string;
  detail?: string;
  /** Sorted above unmarked entries — the ones worth reaching for first. */
  common?: boolean;
  /** Only offered in a math context. */
  math?: boolean;
}

export interface EnvironmentSpec {
  name: string;
  detail?: string;
  /** Body inserted between \begin and \end. */
  body?: string;
  common?: boolean;
}

export const COMMANDS: CommandSpec[] = [
  // --- structure -------------------------------------------------------------
  { name: 'part', snippet: 'part{#{title}}', detail: 'Top-level division' },
  { name: 'chapter', snippet: 'chapter{#{title}}', detail: 'Chapter (book, report)' },
  { name: 'section', snippet: 'section{#{title}}', detail: 'Section', common: true },
  { name: 'subsection', snippet: 'subsection{#{title}}', detail: 'Subsection', common: true },
  { name: 'subsubsection', snippet: 'subsubsection{#{title}}', detail: 'Subsubsection' },
  { name: 'paragraph', snippet: 'paragraph{#{title}}', detail: 'Named paragraph' },
  { name: 'appendix', detail: 'Switch numbering to appendices' },
  { name: 'tableofcontents', detail: 'Insert the table of contents' },

  // --- preamble --------------------------------------------------------------
  { name: 'documentclass', snippet: 'documentclass{#{article}}', detail: 'Document class' },
  { name: 'usepackage', snippet: 'usepackage{#{package}}', detail: 'Load a package', common: true },
  { name: 'title', snippet: 'title{#{title}}', detail: 'Document title' },
  { name: 'author', snippet: 'author{#{name}}', detail: 'Author' },
  { name: 'date', snippet: 'date{#{date}}', detail: 'Date' },
  { name: 'maketitle', detail: 'Typeset the title block' },
  { name: 'newcommand', snippet: 'newcommand{\\#{name}}{#{definition}}', detail: 'Define a command' },
  { name: 'renewcommand', snippet: 'renewcommand{\\#{name}}{#{definition}}', detail: 'Redefine a command' },
  { name: 'newenvironment', snippet: 'newenvironment{#{name}}{#{begin}}{#{end}}', detail: 'Define an environment' },
  { name: 'DeclareMathOperator', snippet: 'DeclareMathOperator{\\#{name}}{#{text}}', detail: 'Define an operator' },

  // --- text ------------------------------------------------------------------
  { name: 'textbf', snippet: 'textbf{#{text}}', detail: 'Bold', common: true },
  { name: 'textit', snippet: 'textit{#{text}}', detail: 'Italic', common: true },
  { name: 'emph', snippet: 'emph{#{text}}', detail: 'Emphasis', common: true },
  { name: 'texttt', snippet: 'texttt{#{text}}', detail: 'Monospace' },
  { name: 'textsc', snippet: 'textsc{#{text}}', detail: 'Small caps' },
  { name: 'textsf', snippet: 'textsf{#{text}}', detail: 'Sans serif' },
  { name: 'underline', snippet: 'underline{#{text}}', detail: 'Underline' },
  { name: 'footnote', snippet: 'footnote{#{text}}', detail: 'Footnote' },
  { name: 'item', detail: 'List item', common: true },
  { name: 'newpage', detail: 'Page break' },
  { name: 'clearpage', detail: 'Page break, flushing floats' },
  { name: 'noindent', detail: 'Suppress paragraph indent' },
  { name: 'centering', detail: 'Centre the enclosing block' },
  { name: 'url', snippet: 'url{#{https://}}', detail: 'Literal URL' },
  { name: 'href', snippet: 'href{#{url}}{#{text}}', detail: 'Hyperlink' },
  { name: 'today', detail: "Today's date" },

  // --- cross-references ------------------------------------------------------
  { name: 'label', snippet: 'label{#{key}}', detail: 'Define a target', common: true },
  { name: 'ref', snippet: 'ref{#{key}}', detail: 'Reference a target', common: true },
  { name: 'eqref', snippet: 'eqref{#{key}}', detail: 'Equation reference' },
  { name: 'autoref', snippet: 'autoref{#{key}}', detail: 'Reference with its name' },
  { name: 'pageref', snippet: 'pageref{#{key}}', detail: 'Page of a target' },
  { name: 'cref', snippet: 'cref{#{key}}', detail: 'cleveref reference' },
  { name: 'cite', snippet: 'cite{#{key}}', detail: 'Citation', common: true },
  { name: 'citep', snippet: 'citep{#{key}}', detail: 'Parenthetical citation' },
  { name: 'citet', snippet: 'citet{#{key}}', detail: 'Textual citation' },
  { name: 'bibliography', snippet: 'bibliography{#{file}}', detail: 'BibTeX database' },
  { name: 'bibliographystyle', snippet: 'bibliographystyle{#{plain}}', detail: 'Bibliography style' },

  // --- files and figures -----------------------------------------------------
  { name: 'input', snippet: 'input{#{file}}', detail: 'Include a source file', common: true },
  { name: 'include', snippet: 'include{#{file}}', detail: 'Include, starting a page' },
  {
    name: 'includegraphics',
    snippet: 'includegraphics[width=#{0.8}\\linewidth]{#{file}}',
    detail: 'Insert an image',
    common: true,
  },
  { name: 'caption', snippet: 'caption{#{text}}', detail: 'Float caption', common: true },

  // --- maths -----------------------------------------------------------------
  { name: 'frac', snippet: 'frac{#{numerator}}{#{denominator}}', detail: 'Fraction', common: true, math: true },
  { name: 'dfrac', snippet: 'dfrac{#{a}}{#{b}}', detail: 'Display-style fraction', math: true },
  { name: 'sqrt', snippet: 'sqrt{#{x}}', detail: 'Square root', common: true, math: true },
  { name: 'sum', snippet: 'sum_{#{i=1}}^{#{n}}', detail: 'Summation', common: true, math: true },
  { name: 'prod', snippet: 'prod_{#{i=1}}^{#{n}}', detail: 'Product', math: true },
  { name: 'int', snippet: 'int_{#{a}}^{#{b}}', detail: 'Integral', common: true, math: true },
  { name: 'lim', snippet: 'lim_{#{x \\to 0}}', detail: 'Limit', math: true },
  { name: 'binom', snippet: 'binom{#{n}}{#{k}}', detail: 'Binomial coefficient', math: true },
  { name: 'mathbf', snippet: 'mathbf{#{x}}', detail: 'Bold maths', math: true },
  { name: 'mathrm', snippet: 'mathrm{#{x}}', detail: 'Roman maths', math: true },
  { name: 'mathcal', snippet: 'mathcal{#{X}}', detail: 'Calligraphic', math: true },
  { name: 'mathbb', snippet: 'mathbb{#{R}}', detail: 'Blackboard bold', math: true },
  { name: 'hat', snippet: 'hat{#{x}}', detail: 'Hat accent', math: true },
  { name: 'bar', snippet: 'bar{#{x}}', detail: 'Bar accent', math: true },
  { name: 'vec', snippet: 'vec{#{x}}', detail: 'Vector arrow', math: true },
  { name: 'partial', detail: 'Partial derivative', math: true },
  { name: 'infty', detail: 'Infinity', math: true },
  { name: 'cdot', detail: 'Centred dot', math: true },
  { name: 'cdots', detail: 'Centred ellipsis', math: true },
  { name: 'ldots', detail: 'Baseline ellipsis', math: true },
  { name: 'times', detail: 'Multiplication sign', math: true },
  { name: 'leq', detail: 'Less than or equal', math: true },
  { name: 'geq', detail: 'Greater than or equal', math: true },
  { name: 'neq', detail: 'Not equal', math: true },
  { name: 'approx', detail: 'Approximately equal', math: true },
  { name: 'equiv', detail: 'Equivalent', math: true },
  { name: 'propto', detail: 'Proportional to', math: true },
  { name: 'in', detail: 'Element of', math: true },
  { name: 'subset', detail: 'Subset', math: true },
  { name: 'forall', detail: 'For all', math: true },
  { name: 'exists', detail: 'There exists', math: true },
  { name: 'rightarrow', detail: 'Right arrow', math: true },
  { name: 'Rightarrow', detail: 'Bold right arrow', math: true },
  { name: 'leftarrow', detail: 'Left arrow', math: true },
  { name: 'to', detail: 'Maps to', math: true },
  { name: 'quad', detail: 'Wide space', math: true },
  { name: 'qquad', detail: 'Wider space', math: true },
  { name: 'text', snippet: 'text{#{words}}', detail: 'Prose inside maths', math: true },

  // Greek. Typed constantly in maths and impossible to misremember the spelling
  // of only when it is in front of you.
  ...[
    'alpha', 'beta', 'gamma', 'delta', 'epsilon', 'varepsilon', 'zeta', 'eta',
    'theta', 'vartheta', 'iota', 'kappa', 'lambda', 'mu', 'nu', 'xi', 'pi',
    'rho', 'sigma', 'tau', 'upsilon', 'phi', 'varphi', 'chi', 'psi', 'omega',
    'Gamma', 'Delta', 'Theta', 'Lambda', 'Xi', 'Pi', 'Sigma', 'Upsilon', 'Phi',
    'Psi', 'Omega',
  ].map((name): CommandSpec => ({ name, detail: 'Greek letter', math: true })),
];

export const ENVIRONMENTS: EnvironmentSpec[] = [
  { name: 'document', detail: 'Document body' },
  { name: 'abstract', detail: 'Abstract' },
  { name: 'itemize', body: '  \\item #{}', detail: 'Bulleted list', common: true },
  { name: 'enumerate', body: '  \\item #{}', detail: 'Numbered list', common: true },
  { name: 'description', body: '  \\item[#{term}] #{}', detail: 'Description list' },
  {
    name: 'figure',
    body: '  \\centering\n  \\includegraphics[width=0.8\\linewidth]{#{file}}\n  \\caption{#{caption}}\n  \\label{fig:#{key}}',
    detail: 'Figure float',
    common: true,
  },
  {
    name: 'table',
    body: '  \\centering\n  \\caption{#{caption}}\n  \\label{tab:#{key}}\n  #{}',
    detail: 'Table float',
    common: true,
  },
  { name: 'tabular', body: '  #{a} & #{b} \\\\', detail: 'Tabular material', common: true },
  { name: 'equation', body: '  #{}', detail: 'Numbered equation', common: true },
  { name: 'equation*', body: '  #{}', detail: 'Unnumbered equation' },
  { name: 'align', body: '  #{a} &= #{b} \\\\', detail: 'Aligned equations', common: true },
  { name: 'align*', body: '  #{a} &= #{b} \\\\', detail: 'Aligned, unnumbered' },
  { name: 'gather', body: '  #{}', detail: 'Centred equations' },
  { name: 'split', body: '  #{}', detail: 'Split one equation' },
  { name: 'cases', body: '  #{a} & #{condition} \\\\', detail: 'Case distinction' },
  { name: 'matrix', body: '  #{a} & #{b} \\\\', detail: 'Matrix, no delimiters' },
  { name: 'pmatrix', body: '  #{a} & #{b} \\\\', detail: 'Matrix, parentheses' },
  { name: 'bmatrix', body: '  #{a} & #{b} \\\\', detail: 'Matrix, brackets' },
  { name: 'center', body: '  #{}', detail: 'Centred block' },
  { name: 'quote', body: '  #{}', detail: 'Short quotation' },
  { name: 'quotation', body: '  #{}', detail: 'Long quotation' },
  { name: 'verbatim', body: '#{}', detail: 'Verbatim text' },
  { name: 'minipage', body: '  #{}', detail: 'Box of the given width' },
  { name: 'theorem', body: '  #{}', detail: 'Theorem (amsthm)' },
  { name: 'proof', body: '  #{}', detail: 'Proof (amsthm)' },
  { name: 'lemma', body: '  #{}', detail: 'Lemma (amsthm)' },
];

/**
 * Commands whose braces hold a cross-reference key, grouped by what they point
 * at. Completion uses this to decide whether the cursor wants labels or
 * citations, so adding a command here is all it takes to support it.
 */
export const REFERENCE_COMMANDS = [
  'ref', 'eqref', 'autoref', 'pageref', 'cref', 'Cref', 'nameref', 'vref',
];

export const CITATION_COMMANDS = [
  'cite', 'citep', 'citet', 'citeauthor', 'citeyear', 'citealp', 'citealt',
  'parencite', 'textcite', 'autocite', 'nocite', 'footcite',
];

/** Commands whose braces hold a path to another file in the project. */
export const FILE_COMMANDS = ['input', 'include', 'includegraphics', 'subfile', 'bibliography'];
