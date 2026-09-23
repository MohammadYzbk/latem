package project

// Cross-reference targets, gathered across the whole project.
//
// These have to be project-wide rather than per-buffer: a \ref in chapter3.tex
// routinely points at a \label in chapter1.tex, and a \cite points into a .bib
// the author never opens. The editor cannot complete either from the text on
// screen, so the scan happens here, where the files are.

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Label is a \label the project defines.
type Label struct {
	Key  string `json:"key"`
	File string `json:"file"`
	Line int    `json:"line"`
	// Section is the nearest heading above the label. Two labels named "main"
	// and "main2" tell you nothing; "Method" and "Results" tell you which one
	// you meant.
	Section string `json:"section"`
}

// Citation is one entry in a .bib database.
type Citation struct {
	Key    string `json:"key"`
	File   string `json:"file"`
	Line   int    `json:"line"`
	Type   string `json:"type"`
	Title  string `json:"title"`
	Author string `json:"author"`
	Year   string `json:"year"`
}

// Macro is a command or environment the project defines for itself. Completing
// these matters more than completing \textbf: nobody forgets \textbf, everybody
// forgets what they called their own shortcut three chapters ago.
type Macro struct {
	Name string `json:"name"`
	File string `json:"file"`
	Line int    `json:"line"`
	Args int    `json:"args"`
}

// Symbols is everything the editor can offer as a completion target.
type Symbols struct {
	Labels       []Label    `json:"labels"`
	Citations    []Citation `json:"citations"`
	Commands     []Macro    `json:"commands"`
	Environments []Macro    `json:"environments"`
	// Error reports a scan that could not start at all. Individual unreadable
	// files are skipped silently — one binary file with a .tex extension should
	// not cost you completion for the other forty.
	Error string `json:"error"`
}

var (
	labelRE    = regexp.MustCompile(`\\label\s*\{([^}]*)\}`)
	headingRE  = regexp.MustCompile(`\\(part|chapter|section|subsection|subsubsection|paragraph)\*?\s*\{`)
	commandRE  = regexp.MustCompile(`\\(?:re)?newcommand\*?\s*\{?\\([A-Za-z@]+)\}?\s*(?:\[(\d+)\])?`)
	operatorRE = regexp.MustCompile(`\\DeclareMathOperator\*?\s*\{?\\([A-Za-z@]+)\}?`)
	environRE  = regexp.MustCompile(`\\(?:re)?newenvironment\*?\s*\{([^}]*)\}\s*(?:\[(\d+)\])?`)

	bibEntryRE = regexp.MustCompile(`^\s*@([A-Za-z]+)\s*[{(]\s*([^,\s{}]+)\s*,`)
	bibFieldRE = regexp.MustCompile(`^\s*([A-Za-z]+)\s*=\s*(.*)$`)
)

// bibNonEntries are @-blocks that declare no citation key.
var bibNonEntries = map[string]bool{"comment": true, "string": true, "preamble": true}

// Symbols scans every .tex and .bib file in the project.
//
// The walk reuses Tree, so it inherits the same skip list and entry cap: a
// project that is too big to browse is also too big to index.
func (p *Project) Symbols() (Symbols, error) {
	var out Symbols

	tree, err := p.Tree()
	if err != nil {
		return out, err
	}

	var visit func(n Node)
	visit = func(n Node) {
		if n.IsDir {
			for _, child := range n.Children {
				visit(child)
			}
			return
		}
		switch n.Kind {
		case KindTeX:
			text, err := p.ReadText(n.Path)
			if err != nil {
				return
			}
			scanTeX(n.Path, text, &out)
		case KindBib:
			text, err := p.ReadText(n.Path)
			if err != nil {
				return
			}
			scanBib(n.Path, text, &out)
		}
	}
	visit(tree)

	sortSymbols(&out)
	return out, nil
}

// scanTeX pulls labels and user definitions out of one source file.
func scanTeX(file, text string, out *Symbols) {
	section := ""

	for i, raw := range strings.Split(text, "\n") {
		line := stripComment(raw)
		if line == "" {
			continue
		}
		number := i + 1

		// Track the current heading so labels can be described by where they
		// are, not just what they are called.
		if loc := headingRE.FindStringIndex(line); loc != nil {
			if title, ok := braceGroup(line, loc[1]-1); ok {
				section = flattenTitle(title)
			}
		}

		for _, m := range labelRE.FindAllStringSubmatch(line, -1) {
			key := strings.TrimSpace(m[1])
			if key == "" {
				continue
			}
			out.Labels = append(out.Labels, Label{Key: key, File: file, Line: number, Section: section})
		}

		if m := commandRE.FindStringSubmatch(line); m != nil {
			out.Commands = append(out.Commands, Macro{
				Name: m[1], File: file, Line: number, Args: atoi(m[2]),
			})
		}
		if m := operatorRE.FindStringSubmatch(line); m != nil {
			out.Commands = append(out.Commands, Macro{Name: m[1], File: file, Line: number})
		}
		if m := environRE.FindStringSubmatch(line); m != nil {
			out.Environments = append(out.Environments, Macro{
				Name: m[1], File: file, Line: number, Args: atoi(m[2]),
			})
		}
	}
}

// scanBib pulls citation keys and enough bibliographic detail to tell two
// similar keys apart in a completion list.
func scanBib(file, text string, out *Symbols) {
	current := -1

	for i, raw := range strings.Split(text, "\n") {
		line := raw

		if m := bibEntryRE.FindStringSubmatch(line); m != nil {
			kind := strings.ToLower(m[1])
			if bibNonEntries[kind] {
				current = -1
				continue
			}
			out.Citations = append(out.Citations, Citation{
				Key: m[2], File: file, Line: i + 1, Type: kind,
			})
			current = len(out.Citations) - 1
			continue
		}

		if current < 0 {
			continue
		}
		m := bibFieldRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		value := bibValue(m[2])
		if value == "" {
			continue
		}
		switch strings.ToLower(m[1]) {
		case "title":
			out.Citations[current].Title = value
		case "author":
			out.Citations[current].Author = value
		case "year", "date":
			if out.Citations[current].Year == "" {
				out.Citations[current].Year = value
			}
		}
	}
}

// bibValue strips the delimiters and trailing comma from a BibTeX field.
//
// It deliberately handles only single-line values. A title wrapped across three
// lines loses its tail, which costs a little display text in a dropdown; the
// citation key itself — the part that has to be correct — is never affected.
func bibValue(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, ",")
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `{}"`)
	return flattenTitle(s)
}

// stripComment removes a TeX comment, respecting the \% escape.
func stripComment(line string) string {
	for i := 0; i < len(line); i++ {
		if line[i] != '%' {
			continue
		}
		// Count the backslashes immediately before it: an odd number escapes.
		slashes := 0
		for j := i - 1; j >= 0 && line[j] == '\\'; j-- {
			slashes++
		}
		if slashes%2 == 0 {
			return strings.TrimRight(line[:i], " \t")
		}
	}
	return strings.TrimRight(line, " \t")
}

// braceGroup reads the balanced { ... } group beginning at open, which lets a
// section title contain braces of its own.
func braceGroup(s string, open int) (string, bool) {
	if open >= len(s) || s[open] != '{' {
		return "", false
	}
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[open+1 : i], true
			}
		}
	}
	return "", false
}

// flattenTitle reduces a heading to something that reads well in a one-line
// list: no markup, no runs of whitespace.
func flattenTitle(s string) string {
	s = strings.NewReplacer("\\,", " ", "~", " ", "\\&", "&").Replace(s)
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimSpace(s)
}

func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

// sortSymbols gives the UI a stable order. Completion ranking happens in the
// editor; what matters here is that two scans of an unchanged project produce
// the same list.
func sortSymbols(s *Symbols) {
	sort.SliceStable(s.Labels, func(i, j int) bool { return s.Labels[i].Key < s.Labels[j].Key })
	sort.SliceStable(s.Citations, func(i, j int) bool { return s.Citations[i].Key < s.Citations[j].Key })
	sort.SliceStable(s.Commands, func(i, j int) bool { return s.Commands[i].Name < s.Commands[j].Name })
	sort.SliceStable(s.Environments, func(i, j int) bool { return s.Environments[i].Name < s.Environments[j].Name })
}
