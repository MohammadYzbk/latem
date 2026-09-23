package project

import (
	"strings"
	"testing"
)

// findLabel returns the label with the given key, or nil.
func findLabel(symbols Symbols, key string) *Label {
	for i := range symbols.Labels {
		if symbols.Labels[i].Key == key {
			return &symbols.Labels[i]
		}
	}
	return nil
}

func findCitation(symbols Symbols, key string) *Citation {
	for i := range symbols.Citations {
		if symbols.Citations[i].Key == key {
			return &symbols.Citations[i]
		}
	}
	return nil
}

func keysOf(macros []Macro) []string {
	out := make([]string, 0, len(macros))
	for _, m := range macros {
		out = append(out, m.Name)
	}
	return out
}

// The whole point of scanning on the backend: a \ref in one file has to be able
// to complete against a \label in another.
func TestSymbolsCollectsLabelsAcrossFiles(t *testing.T) {
	p, err := Open(scaffold(t, map[string]string{
		"main.tex":             rootDoc + "\\input{chapters/intro}\n",
		"chapters/intro.tex":   "\\section{Introduction}\n\\label{sec:intro}\n",
		"chapters/results.tex": "\\section{Results}\n\\label{sec:results}\n",
	}))
	if err != nil {
		t.Fatal(err)
	}

	symbols, err := p.Symbols()
	if err != nil {
		t.Fatal(err)
	}

	intro := findLabel(symbols, "sec:intro")
	if intro == nil {
		t.Fatalf("sec:intro was not found; labels: %+v", symbols.Labels)
	}
	if intro.File != "chapters/intro.tex" {
		t.Errorf("file: got %q, want chapters/intro.tex", intro.File)
	}
	if intro.Line != 2 {
		t.Errorf("line: got %d, want 2", intro.Line)
	}
	if findLabel(symbols, "sec:results") == nil {
		t.Error("sec:results was not found")
	}
}

// A label's key is often opaque; the heading above it is what makes a
// completion list readable.
func TestSymbolsAttributesLabelToNearestHeading(t *testing.T) {
	p, err := Open(scaffold(t, map[string]string{
		"main.tex": rootDoc,
		"body.tex": "\\section{Method}\n\\label{a}\n\\subsection{Sampling}\n\\label{b}\n",
	}))
	if err != nil {
		t.Fatal(err)
	}

	symbols, err := p.Symbols()
	if err != nil {
		t.Fatal(err)
	}

	if got := findLabel(symbols, "a"); got == nil || got.Section != "Method" {
		t.Errorf("label a section: got %+v, want Method", got)
	}
	if got := findLabel(symbols, "b"); got == nil || got.Section != "Sampling" {
		t.Errorf("label b section: got %+v, want Sampling", got)
	}
}

// A heading may contain braces of its own, so the title cannot be read with a
// non-greedy match up to the first "}".
func TestSymbolsReadsHeadingWithNestedBraces(t *testing.T) {
	p, err := Open(scaffold(t, map[string]string{
		"main.tex": rootDoc,
		"body.tex": "\\section{A \\textbf{bold} idea}\n\\label{x}\n",
	}))
	if err != nil {
		t.Fatal(err)
	}

	symbols, err := p.Symbols()
	if err != nil {
		t.Fatal(err)
	}

	got := findLabel(symbols, "x")
	if got == nil {
		t.Fatal("label x was not found")
	}
	if !strings.Contains(got.Section, "bold") || !strings.Contains(got.Section, "idea") {
		t.Errorf("section: got %q, want the full heading text", got.Section)
	}
}

// Commented-out labels are not real targets. Offering one produces a document
// that fails to compile with an undefined reference.
func TestSymbolsIgnoresCommentedLabels(t *testing.T) {
	p, err := Open(scaffold(t, map[string]string{
		"main.tex": rootDoc,
		"body.tex": "% \\label{commented}\n\\label{real}\n100\\% \\label{after-escape}\n",
	}))
	if err != nil {
		t.Fatal(err)
	}

	symbols, err := p.Symbols()
	if err != nil {
		t.Fatal(err)
	}

	if findLabel(symbols, "commented") != nil {
		t.Error("a commented-out label was offered as a target")
	}
	if findLabel(symbols, "real") == nil {
		t.Error("real label was not found")
	}
	// The % here is escaped, so the label after it is live code.
	if findLabel(symbols, "after-escape") == nil {
		t.Error("a label following an escaped %% was treated as commented out")
	}
}

func TestSymbolsReadsBibEntries(t *testing.T) {
	p, err := Open(scaffold(t, map[string]string{
		"main.tex": rootDoc,
		"refs.bib": `@article{knuth1984,
  title = {Literate Programming},
  author = {Donald E. Knuth},
  year = {1984},
}

@book{lamport1994,
  title = "LaTeX: A Document Preparation System",
  author = {Leslie Lamport},
  year = 1994
}
`,
	}))
	if err != nil {
		t.Fatal(err)
	}

	symbols, err := p.Symbols()
	if err != nil {
		t.Fatal(err)
	}

	knuth := findCitation(symbols, "knuth1984")
	if knuth == nil {
		t.Fatalf("knuth1984 not found; citations: %+v", symbols.Citations)
	}
	if knuth.Title != "Literate Programming" {
		t.Errorf("title: got %q", knuth.Title)
	}
	if knuth.Author != "Donald E. Knuth" {
		t.Errorf("author: got %q", knuth.Author)
	}
	if knuth.Year != "1984" {
		t.Errorf("year: got %q", knuth.Year)
	}
	if knuth.Type != "article" {
		t.Errorf("type: got %q, want article", knuth.Type)
	}

	// Quoted values and an unbraced year are both ordinary BibTeX.
	lamport := findCitation(symbols, "lamport1994")
	if lamport == nil {
		t.Fatal("lamport1994 not found")
	}
	if !strings.HasPrefix(lamport.Title, "LaTeX") {
		t.Errorf("quoted title: got %q", lamport.Title)
	}
	if lamport.Year != "1994" {
		t.Errorf("bare year: got %q", lamport.Year)
	}
}

// @string and @comment declare no citation key, so offering them would put a
// word in a \cite that resolves to nothing.
func TestSymbolsSkipsBibNonEntries(t *testing.T) {
	p, err := Open(scaffold(t, map[string]string{
		"main.tex": rootDoc,
		"refs.bib": "@string{acm = {ACM}}\n@comment{ignore me}\n@article{real2020,\n  title = {Real},\n}\n",
	}))
	if err != nil {
		t.Fatal(err)
	}

	symbols, err := p.Symbols()
	if err != nil {
		t.Fatal(err)
	}

	if findCitation(symbols, "acm") != nil || findCitation(symbols, "ignore me") != nil {
		t.Errorf("a non-entry block was offered as a citation: %+v", symbols.Citations)
	}
	if findCitation(symbols, "real2020") == nil {
		t.Error("real2020 was not found")
	}
}

// Nobody forgets \textbf. Everybody forgets what they named their own macro.
func TestSymbolsCollectsUserDefinitions(t *testing.T) {
	p, err := Open(scaffold(t, map[string]string{
		"main.tex": rootDoc,
		"defs.tex": `\newcommand{\vect}[1]{\mathbf{#1}}
\renewcommand{\qed}{\blacksquare}
\DeclareMathOperator{\argmin}{arg\,min}
\newenvironment{theorem}[1]{\begin{quote}}{\end{quote}}
`,
	}))
	if err != nil {
		t.Fatal(err)
	}

	symbols, err := p.Symbols()
	if err != nil {
		t.Fatal(err)
	}

	commands := strings.Join(keysOf(symbols.Commands), " ")
	for _, want := range []string{"vect", "qed", "argmin"} {
		if !strings.Contains(commands, want) {
			t.Errorf("command %q missing from %q", want, commands)
		}
	}
	if got := keysOf(symbols.Environments); len(got) != 1 || got[0] != "theorem" {
		t.Errorf("environments: got %v, want [theorem]", got)
	}

	// The argument count drives snippet placeholders, so a wrong count produces
	// a snippet that leaves braces behind.
	for _, m := range symbols.Commands {
		if m.Name == "vect" && m.Args != 1 {
			t.Errorf("vect args: got %d, want 1", m.Args)
		}
	}
}

// One unreadable file must not cost completion for every other file.
func TestSymbolsSkipsUnreadableFilesWithoutFailing(t *testing.T) {
	p, err := Open(scaffold(t, map[string]string{
		"main.tex":   rootDoc,
		"good.tex":   "\\label{good}\n",
		"binary.tex": "\x00\x01\x02 not text at all",
	}))
	if err != nil {
		t.Fatal(err)
	}

	symbols, err := p.Symbols()
	if err != nil {
		t.Fatalf("a binary .tex file aborted the whole scan: %v", err)
	}
	if findLabel(symbols, "good") == nil {
		t.Error("the readable file's label was lost")
	}
}

// Two scans of an unchanged project must agree, or the completion list reorders
// itself under the cursor.
func TestSymbolsOrderIsStable(t *testing.T) {
	p, err := Open(scaffold(t, map[string]string{
		"main.tex": rootDoc,
		"a.tex":    "\\label{zeta}\n\\label{alpha}\n",
		"b.tex":    "\\label{mid}\n",
	}))
	if err != nil {
		t.Fatal(err)
	}

	first, err := p.Symbols()
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.Symbols()
	if err != nil {
		t.Fatal(err)
	}

	if strings.Join(labelKeys(first), ",") != strings.Join(labelKeys(second), ",") {
		t.Errorf("order changed between scans: %v then %v", labelKeys(first), labelKeys(second))
	}
	if got := labelKeys(first); got[0] != "alpha" {
		t.Errorf("labels are not sorted: %v", got)
	}
}

func labelKeys(s Symbols) []string {
	out := make([]string, 0, len(s.Labels))
	for _, l := range s.Labels {
		out = append(out, l.Key)
	}
	return out
}
