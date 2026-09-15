// Package texlog turns engine chatter into structured, human-readable
// diagnostics.
//
// TeX errors are endlessly varied, so this aims at the common cases and no
// further: undefined control sequences, missing maths mode, missing packages,
// unclosed braces, and undefined references. Anything unrecognised still
// reaches the user as the raw log, which the UI always keeps available — the
// parser's job is to put a marker on the right line when it can, never to be
// the only way to see what the engine said.
package texlog

import (
	"regexp"
	"strconv"
	"strings"
)

const maximumReportedLocationCandidates = 64

// Severity orders diagnostics by how much they should interrupt the writer.
type Severity string

const (
	// SeverityError means the document did not typeset.
	SeverityError Severity = "error"
	// SeverityWarning means it typeset, but probably not as intended — an
	// undefined reference, say.
	SeverityWarning Severity = "warning"
	// SeverityInfo is typographic nagging (overfull boxes). Real, but not worth
	// interrupting someone mid-sentence, so the UI keeps it out of the gutter.
	SeverityInfo Severity = "info"
)

// Diagnostic is one problem the engine reported.
type Diagnostic struct {
	Severity Severity `json:"severity"`

	// File is as the engine reported it, usually relative to the document's
	// directory. Empty when the engine gave no location.
	File string `json:"file"`
	// Line is 1-based, or 0 when unknown.
	Line int `json:"line"`

	// Message is the engine's text, cleaned of prefixes and duplication.
	Message string `json:"message"`

	// Hint is a plain-language explanation, empty when we have nothing useful
	// to add. It never replaces Message — a writer who knows TeX should still
	// see what TeX actually said.
	Hint string `json:"hint"`

	// Token is the specific command the error is about, when one is
	// identifiable (`\foo`). The UI underlines it instead of the whole line.
	Token string `json:"token"`

	// Approximate means Line was inferred rather than reported by the engine,
	// so the UI must not present it as the exact spot. See ResolveLocations.
	Approximate bool `json:"approximate"`
}

var (
	// Tectonic's own structured prefix: "error: file.tex:12: message".
	structuredRE = regexp.MustCompile(`^(error|warning): (.*)$`)

	// Classic LaTeX warnings, which carry their line inside the prose.
	latexWarningRE = regexp.MustCompile(`^(?:LaTeX|Package|Class)\b.*?Warning:\s*(.*)$`)
	inputLineRE    = regexp.MustCompile(`on input line (\d+)`)

	// The engine's "l.12 <text it had consumed>" context line.
	lineContextRE = regexp.MustCompile(`^l\.(\d+) (.*)$`)

	// The start of a TeX run. Tectonic typesets repeatedly to settle
	// cross-references and the bibliography.
	texPassRE = regexp.MustCompile(`^note: (?:Running|Rerunning) TeX`)

	// A trailing control sequence, used to name the offending command.
	trailingCommandRE = regexp.MustCompile(`(\\[a-zA-Z@]+)\s*$`)

	missingFileRE = regexp.MustCompile("File `([^']+)' not found")
	namedThingRE  = regexp.MustCompile("`([^']+)'")

	// "File ended while scanning use of \textbf" names its own culprit.
	scanningTokenRE = regexp.MustCompile(`(?:while scanning use of|scanning text of) (\\[a-zA-Z@]+)`)
)

// engineNoise are messages about the engine's own plumbing. They always
// accompany a real error and say nothing the writer can act on, so surfacing
// them would just bury the actual problem.
var engineNoise = []string{
	"something bad happened inside XeTeX",
	"the XeTeX engine had an unrecoverable error",
	"halted on potentially-recoverable error",
	"caused by:",
	"the TeX engine had an unrecoverable error",
	"the engine failed",
}

// summaryNoise are roll-ups of diagnostics we have already reported
// individually, and rerun requests that Tectonic acts on by itself.
var summaryNoise = []string{
	"There were undefined references",
	"There were undefined citations",
	"There were multiply-defined labels",
	"Label(s) may have changed",
	"Rerun to get cross-references right",
	"Please rerun LaTeX",
	// Tectonic's own footer telling us to pass flags we already pass.
	"warnings were issued by the TeX engine",
}

// ReportedFileLookup confirms that an ambiguous engine-reported filename names
// a source available to the caller. Callers should cache filesystem-backed
// lookups because the parser may encounter the same name more than once.
type ReportedFileLookup func(reported string) bool

// Parse extracts diagnostics from a run's combined engine output without
// attributing ambiguous prose-shaped text to a source file.
//
// Warnings are kept only from the final TeX pass because early passes describe
// a document that may settle later. Errors remain across pass markers: combined
// output is document-controlled, so a marker cannot authenticate that earlier
// failure evidence is stale. Identical diagnostics are collapsed.
func Parse(chatter string) []Diagnostic {
	return parse(chatter, nil)
}

// ParseWithReportedFileLookup resolves otherwise ambiguous root-level,
// extensionless filenames against the caller's actual project sources.
func ParseWithReportedFileLookup(chatter string, lookup ReportedFileLookup) []Diagnostic {
	return parse(chatter, lookup)
}

func parse(chatter string, lookup ReportedFileLookup) []Diagnostic {
	lines := strings.Split(chatter, "\n")
	finalPassStart := lastTeXPassStart(lines)

	var out []Diagnostic
	seen := make(map[Diagnostic]bool)
	add := func(d Diagnostic) {
		if seen[d] {
			return
		}
		seen[d] = true
		out = append(out, d)
	}

	for i, raw := range lines {
		line := strings.TrimRight(raw, "\r")

		if m := structuredRE.FindStringSubmatch(line); m != nil {
			severity, body := Severity(m[1]), m[2]
			if severity != SeverityError && i < finalPassStart {
				continue
			}
			d := Diagnostic{Severity: severity}
			if loc, ok := reportedLocation(body, lookup); ok {
				d.File = loc.file
				d.Line, _ = strconv.Atoi(loc.line)
				d.Message = cleanMessage(loc.message)
			} else {
				d.Message = cleanMessage(body)
			}
			if d.File == "" && (containsAny(body, engineNoise) || containsAny(d.Message, summaryNoise)) {
				continue
			}
			if d.Message == "" {
				continue
			}
			// Overfull/underfull boxes are typography, not breakage.
			if severity == SeverityWarning && isBoxWarning(d.Message) {
				d.Severity = SeverityInfo
			}
			enrich(&d, lines[i:])
			add(d)
			continue
		}

		// LaTeX's own warnings are printed verbatim, without Tectonic's prefix.
		if m := latexWarningRE.FindStringSubmatch(line); m != nil {
			if i < finalPassStart {
				continue
			}
			message := cleanMessage(m[1])
			if message == "" || containsAny(message, summaryNoise) {
				continue
			}
			d := Diagnostic{Severity: SeverityWarning, Message: message}
			if loc := inputLineRE.FindStringSubmatch(message); loc != nil {
				d.Line, _ = strconv.Atoi(loc[1])
			}
			d.Hint = hintFor(d.Message, "")
			add(d)
		}
	}
	return out
}

type locationCandidate struct {
	file    string
	line    string
	message string
}

// reportedLocation considers every :line: boundary because colon-number text
// is legal inside a macOS filename. A project lookup is authoritative when it
// confirms a candidate. Without one, the last path-shaped candidate wins, but
// a later prose-shaped field such as "failed at code:34:" does not consume an
// earlier unambiguous location.
func reportedLocation(body string, lookup ReportedFileLookup) (locationCandidate, bool) {
	candidates, overflow := locationCandidates(body)
	if lookup != nil {
		for i := len(candidates) - 1; i >= 0; i-- {
			if validReportedFile(candidates[i].file) && lookup(candidates[i].file) {
				return candidates[i], true
			}
		}
	}
	if overflow {
		return locationCandidate{}, false
	}

	if len(candidates) == 0 || !plausibleReportedFile(candidates[0].file) {
		return locationCandidate{}, false
	}
	fallback := candidates[0]
	for _, candidate := range candidates[1:] {
		if !plausibleReportedFile(candidate.file) {
			continue
		}
		continuation := candidate.file[len(fallback.file):]
		if strings.ContainsAny(continuation, " \t") && !strings.ContainsAny(continuation, `/\\.`) {
			continue
		}
		fallback = candidate
	}
	return fallback, true
}

func locationCandidates(body string) ([]locationCandidate, bool) {
	var first locationCandidate
	tail := make([]locationCandidate, 0, maximumReportedLocationCandidates-1)
	tailStart := 0
	candidateCount := 0
	for boundaryStart := 0; boundaryStart < len(body); boundaryStart++ {
		if body[boundaryStart] != ':' {
			continue
		}
		digitsEnd := boundaryStart + 1
		for digitsEnd < len(body) && body[digitsEnd] >= '0' && body[digitsEnd] <= '9' {
			digitsEnd++
		}
		if digitsEnd == boundaryStart+1 || digitsEnd >= len(body) || body[digitsEnd] != ':' {
			continue
		}
		candidate := locationCandidate{
			file:    body[:boundaryStart],
			line:    body[boundaryStart+1 : digitsEnd],
			message: body[digitsEnd+1:],
		}
		candidateCount++
		if candidateCount == 1 {
			first = candidate
			continue
		}
		if len(tail) < cap(tail) {
			tail = append(tail, candidate)
			continue
		}
		tail[tailStart] = candidate
		tailStart = (tailStart + 1) % len(tail)
	}
	if candidateCount == 0 {
		return nil, false
	}
	candidates := make([]locationCandidate, 0, min(candidateCount, maximumReportedLocationCandidates))
	candidates = append(candidates, first)
	if candidateCount <= maximumReportedLocationCandidates {
		candidates = append(candidates, tail...)
		return candidates, false
	}
	// Preserve both the earliest, ordinary `file:line:` boundary and the
	// longest bounded candidates needed for colon-number filename components.
	// Numeric fields in diagnostic prose cannot evict the ordinary boundary.
	candidates = append(candidates, tail[tailStart:]...)
	candidates = append(candidates, tail[:tailStart]...)
	return candidates, true
}

func validReportedFile(file string) bool {
	return file != "" && !strings.ContainsAny(file, "\x00\r\n")
}

func plausibleReportedFile(file string) bool {
	if !validReportedFile(file) {
		return false
	}
	if strings.Trim(file, " \t") != file {
		return false
	}
	// Keep prose such as "something went wrong: 42: ..." from becoming a
	// location. A caller with project access can disambiguate the same shape by
	// confirming an actual source instead of inspecting diagnostic prose.
	if strings.ContainsAny(file, " \t") && !strings.ContainsAny(file, `/\\.`) {
		return false
	}
	return true
}

func lastTeXPassStart(lines []string) int {
	start := 0
	for i, line := range lines {
		if texPassRE.MatchString(line) {
			start = i
		}
	}
	return start
}

// enrich looks at the lines following a structured error for the engine's
// "l.12 ..." context, which supplies a line number when the structured prefix
// had none and names the command that actually broke.
func enrich(d *Diagnostic, following []string) {
	// Some messages name their own culprit, which beats guessing from context.
	if m := scanningTokenRE.FindStringSubmatch(d.Message); m != nil {
		d.Token = m[1]
	}

	const lookahead = 6
	for _, line := range following[min(1, len(following)):min(lookahead+1, len(following))] {
		// Stop at the next diagnostic so we never borrow its context.
		if structuredRE.MatchString(line) {
			break
		}
		m := lineContextRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if d.Line == 0 {
			d.Line, _ = strconv.Atoi(m[1])
		}
		// Only trust the context line's trailing command when the error is
		// actually *about* a command. For "File `foo.sty' not found" the
		// context reads "l.3 \begin{document}", and underlining \begin would
		// point the writer at innocent code.
		if d.Token == "" && strings.Contains(d.Message, "Undefined control sequence") {
			if cmd := trailingCommandRE.FindStringSubmatch(m[2]); cmd != nil {
				d.Token = cmd[1]
			}
		}
		break
	}
	d.Hint = hintFor(d.Message, d.Token)
}

// SourceLookup returns the contents of a file the engine mentioned, and whether
// it could be found. A project spans many files, so resolution has to be able to
// ask for whichever one a diagnostic belongs to.
type SourceLookup func(file string) (string, bool)

// ResolveLocations fills in a line for diagnostics the engine could not place,
// by finding the command they name in the source.
//
// The unclosed-brace case needs this: TeX reports "File ended while scanning
// use of \textbf" with no location at all, because by the time it noticed, it
// was at the end of the file. The command it names is still in the source
// though, so we can point at it. Such diagnostics are marked Approximate, since
// a document using \textbf twenty times gets the last one, which may not be the
// unbalanced one.
func ResolveLocations(diags []Diagnostic, source SourceLookup) []Diagnostic {
	return resolveLocations(diags, source, lastControlSequenceLines)
}

type controlSequenceLineScanner func(content string, wanted map[string]struct{}) map[string]int

func resolveLocations(diags []Diagnostic, source SourceLookup, scan controlSequenceLineScanner) []Diagnostic {
	out := make([]Diagnostic, len(diags))
	copy(out, diags)
	type sourceRequests struct {
		byToken map[string][]int
	}
	requests := make(map[string]*sourceRequests)
	requestOrder := make([]string, 0)
	for i := range out {
		if out[i].Line != 0 || out[i].Token == "" {
			continue
		}
		request := requests[out[i].File]
		if request == nil {
			request = &sourceRequests{byToken: make(map[string][]int)}
			requests[out[i].File] = request
			requestOrder = append(requestOrder, out[i].File)
		}
		request.byToken[out[i].Token] = append(request.byToken[out[i].Token], i)
	}
	for _, file := range requestOrder {
		request := requests[file]
		content, ok := source(file)
		if !ok {
			continue
		}
		wanted := make(map[string]struct{}, len(request.byToken))
		for token := range request.byToken {
			wanted[token] = struct{}{}
		}
		for token, line := range scan(content, wanted) {
			for _, diagnosticIndex := range request.byToken[token] {
				out[diagnosticIndex].Line = line
				out[diagnosticIndex].Approximate = true
			}
		}
	}
	return out
}

func lastControlSequenceLines(content string, wanted map[string]struct{}) map[string]int {
	lines := make(map[string]int, len(wanted))
	line := 1
	for offset := 0; offset < len(content); {
		switch content[offset] {
		case '\n':
			line++
			offset++
		case '\\':
			end := offset + 1
			for end < len(content) && isControlSequenceByte(content[end]) {
				end++
			}
			if end > offset+1 {
				token := content[offset:end]
				if _, requested := wanted[token]; requested {
					lines[token] = line
				}
				offset = end
				continue
			}
			offset++
		default:
			offset++
		}
	}
	return lines
}

func isControlSequenceByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value == '@'
}

// SingleSource is a SourceLookup for the one-file case, which is what the tests
// and a single-document project both need.
func SingleSource(content string) SourceLookup {
	return func(string) (string, bool) { return content, true }
}

// cleanMessage strips the engine's decoration: leading "!" markers, the
// "LaTeX Error:" label, and duplicated trailing periods.
func cleanMessage(s string) string {
	s = strings.TrimSpace(s)
	for strings.HasPrefix(s, "!") {
		s = strings.TrimSpace(strings.TrimPrefix(s, "!"))
	}
	for _, prefix := range []string{"LaTeX Error:", "Package Error:", "Class Error:"} {
		if rest, ok := strings.CutPrefix(s, prefix); ok {
			s = strings.TrimSpace(rest)
			break
		}
	}
	for strings.HasSuffix(s, "..") {
		s = strings.TrimSuffix(s, ".")
	}
	return strings.TrimSpace(s)
}

// hintFor translates the common failures into something a writer can act on.
// An empty result means we have nothing better to say than the engine did.
func hintFor(message, token string) string {
	switch {
	case strings.Contains(message, "Undefined control sequence"):
		if token != "" {
			return "TeX does not know the command " + token +
				". Check the spelling, or load the package that defines it."
		}
		return "A command in this line is not defined. Check the spelling, or load the package that defines it."

	case strings.Contains(message, "Missing $ inserted"):
		return "A maths-only character such as _ or ^ was used in ordinary text. Wrap it in $…$."

	case strings.Contains(message, "not found"):
		if m := missingFileRE.FindStringSubmatch(message); m != nil {
			name := strings.TrimSuffix(m[1], ".sty")
			return "The package " + name + " is not available. Check the name, " +
				"or remove the \\usepackage line if it is not needed."
		}
		return "A file the document asks for does not exist. Check the path."

	case strings.Contains(message, "File ended while scanning"):
		hint := "A { was opened and never closed, so the argument ran to the end of the file."
		if token != "" {
			hint = "A { opened for " + token + " was never closed, so its argument ran to the end of the file."
		}
		return hint

	case strings.Contains(message, "Missing } inserted"),
		strings.Contains(message, "Extra }"):
		return "The braces on this line do not balance."

	case strings.Contains(message, "Environment") && strings.Contains(message, "undefined"):
		return "There is no such environment. Check the name, or load the package that provides it."

	case strings.Contains(message, "ended by"):
		return "A \\begin{…} is closed by a different \\end{…}. Make the names match."

	case strings.Contains(message, "Reference") && strings.Contains(message, "undefined"):
		if m := namedThingRE.FindStringSubmatch(message); m != nil {
			return "No \\label{" + m[1] + "} exists anywhere in the document."
		}
		return "This \\ref points at a label that does not exist."

	case strings.Contains(message, "Citation") && strings.Contains(message, "undefined"):
		if m := namedThingRE.FindStringSubmatch(message); m != nil {
			return "No bibliography entry named " + m[1] + " was found."
		}
		return "This \\cite points at an entry that is not in the bibliography."

	case isBoxWarning(message):
		return "A line does not fit the text width. Usually cosmetic; often a long word or URL that cannot be hyphenated."
	}
	return ""
}

func isBoxWarning(message string) bool {
	return strings.Contains(message, "Overfull \\hbox") ||
		strings.Contains(message, "Underfull \\hbox") ||
		strings.Contains(message, "Overfull \\vbox") ||
		strings.Contains(message, "Underfull \\vbox")
}

func containsAny(s string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}
