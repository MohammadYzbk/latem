package texlog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fixtures are real Tectonic output, captured by compiling the matching
// .tex file in testdata. Regenerate with:
//
//	tectonic -X compile testdata/undefined.tex --outdir out --keep-logs \
//	    --synctex --print --untrusted > testdata/undefined.chatter 2>&1
func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name+".chatter"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return string(b)
}

// firstOfSeverity returns the first diagnostic at the given severity.
func firstOfSeverity(diags []Diagnostic, s Severity) (Diagnostic, bool) {
	for _, d := range diags {
		if d.Severity == s {
			return d, true
		}
	}
	return Diagnostic{}, false
}

func TestParseUndefinedControlSequence(t *testing.T) {
	diags := Parse(fixture(t, "undefined"))

	d, ok := firstOfSeverity(diags, SeverityError)
	if !ok {
		t.Fatalf("no error parsed from:\n%+v", diags)
	}
	if d.File != "undefined.tex" {
		t.Errorf("File = %q, want undefined.tex", d.File)
	}
	if d.Line != 4 {
		t.Errorf("Line = %d, want 4 (where \\thisIsNotACommand is)", d.Line)
	}
	if !strings.Contains(d.Message, "Undefined control sequence") {
		t.Errorf("Message = %q", d.Message)
	}
	// The offending command is what makes the message actionable.
	if d.Token != `\thisIsNotACommand` {
		t.Errorf("Token = %q, want \\thisIsNotACommand", d.Token)
	}
	if !strings.Contains(d.Hint, `\thisIsNotACommand`) {
		t.Errorf("Hint does not name the command: %q", d.Hint)
	}
}

func TestParseBoundsStructuredDiagnosticContextLookahead(t *testing.T) {
	for _, test := range []struct {
		name     string
		padding  int
		wantLine int
	}{
		{name: "six following lines", padding: 5, wantLine: 42},
		{name: "seventh following line", padding: 6, wantLine: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			log := "error: Undefined control sequence\n" +
				strings.Repeat("ordinary engine context\n", test.padding) +
				"l.42 \\missing\n"
			diagnostics := Parse(log)
			if len(diagnostics) != 1 || diagnostics[0].Line != test.wantLine {
				t.Fatalf("diagnostics = %+v, want line %d", diagnostics, test.wantLine)
			}
		})
	}
}

func TestParseMissingMathMode(t *testing.T) {
	diags := Parse(fixture(t, "missingmath"))

	d, ok := firstOfSeverity(diags, SeverityError)
	if !ok {
		t.Fatalf("no error parsed from:\n%+v", diags)
	}
	if d.Line != 3 {
		t.Errorf("Line = %d, want 3", d.Line)
	}
	if !strings.Contains(d.Message, "Missing $ inserted") {
		t.Errorf("Message = %q", d.Message)
	}
	if !strings.Contains(d.Hint, "$…$") {
		t.Errorf("Hint should explain maths mode, got %q", d.Hint)
	}
}

func TestParseMissingPackage(t *testing.T) {
	diags := Parse(fixture(t, "missingpkg"))

	d, ok := firstOfSeverity(diags, SeverityError)
	if !ok {
		t.Fatalf("no error parsed from:\n%+v", diags)
	}
	// The engine doubles the "!" and appends the label; neither belongs in the UI.
	if strings.HasPrefix(d.Message, "!") || strings.Contains(d.Message, "LaTeX Error:") {
		t.Errorf("Message was not cleaned: %q", d.Message)
	}
	if !strings.Contains(d.Message, "thispackagedoesnotexist.sty") {
		t.Errorf("Message = %q", d.Message)
	}
	if !strings.Contains(d.Hint, "thispackagedoesnotexist") {
		t.Errorf("Hint should name the package: %q", d.Hint)
	}
}

// This one has no file:line at all, which is exactly why it is worth a test:
// the parser must still produce a usable diagnostic rather than dropping it.
func TestParseUnclosedBrace(t *testing.T) {
	diags := Parse(fixture(t, "braces"))

	d, ok := firstOfSeverity(diags, SeverityError)
	if !ok {
		t.Fatalf("no error parsed from:\n%+v", diags)
	}
	if !strings.Contains(d.Message, "File ended while scanning") {
		t.Errorf("Message = %q", d.Message)
	}
	if !strings.Contains(d.Hint, "never closed") {
		t.Errorf("Hint should explain the unclosed brace, got %q", d.Hint)
	}
	// The message names the command even though the engine gave no location.
	if d.Token != `\textbf` {
		t.Errorf("Token = %q, want \\textbf", d.Token)
	}
	if d.Line != 0 {
		t.Errorf("Line = %d; the engine reports no location for this error", d.Line)
	}
}

// TeX gives no line for an unclosed brace, but the command it names is still in
// the source, so we can point at it — flagged as approximate.
func TestResolveLocationsPlacesUnclosedBrace(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("testdata", "braces.tex"))
	if err != nil {
		t.Fatal(err)
	}

	diags := ResolveLocations(Parse(fixture(t, "braces")), SingleSource(string(source)))
	d, ok := firstOfSeverity(diags, SeverityError)
	if !ok {
		t.Fatal("no error parsed")
	}
	if d.Line != 3 {
		t.Errorf("Line = %d, want 3 (the \\textbf line)", d.Line)
	}
	if !d.Approximate {
		t.Error("an inferred line must be marked Approximate")
	}
}

// A line the engine did report must never be overwritten by a guess.
func TestResolveLocationsLeavesReportedLinesAlone(t *testing.T) {
	source := "\\documentclass{article}\n\\begin{document}\n\\thisIsNotACommand\n\\thisIsNotACommand\n\\end{document}\n"

	for _, d := range ResolveLocations(Parse(fixture(t, "undefined")), SingleSource(source)) {
		if d.Severity != SeverityError {
			continue
		}
		if d.Line != 4 {
			t.Errorf("Line = %d, want the engine's 4", d.Line)
		}
		if d.Approximate {
			t.Error("a line reported by the engine must not be marked Approximate")
		}
	}
}

func TestResolveLocationsIndexesEachSourceOnceForManyDiagnostics(t *testing.T) {
	const diagnosticCount = 2048
	diagnostics := make([]Diagnostic, 0, diagnosticCount)
	for i := 0; i < diagnosticCount; i++ {
		diagnostics = append(diagnostics, Diagnostic{
			Severity: SeverityError,
			File:     "main.tex",
			Message:  fmt.Sprintf("failure %d", i),
			Token:    `\target`,
		})
	}
	lookups := 0
	scans := 0
	resolved := resolveLocations(diagnostics, func(file string) (string, bool) {
		lookups++
		if file != "main.tex" {
			t.Fatalf("source lookup file = %q", file)
		}
		return strings.Repeat("ordinary text\n", 10000) + "\\target\n", true
	}, func(content string, wanted map[string]struct{}) map[string]int {
		scans++
		return lastControlSequenceLines(content, wanted)
	})

	if lookups != 1 || scans != 1 {
		t.Fatalf("many-diagnostic work = %d lookups, %d scans; want one of each", lookups, scans)
	}
	for i, diagnostic := range resolved {
		if diagnostic.Line != 10001 || !diagnostic.Approximate {
			t.Fatalf("diagnostic %d = %+v, want one-pass approximate location", i, diagnostic)
		}
	}
}

// The context line after a missing-package error reads "l.3 \begin{document}".
// Taking \begin as the culprit would underline innocent code.
func TestMissingPackageDoesNotBlameSurroundingCommand(t *testing.T) {
	diags := Parse(fixture(t, "missingpkg"))
	d, ok := firstOfSeverity(diags, SeverityError)
	if !ok {
		t.Fatal("no error parsed")
	}
	if d.Token != "" {
		t.Errorf("Token = %q; a missing file is not about a command", d.Token)
	}
}

// The engine names files exactly as the document did, so "\input{chapters/intro}"
// produces "chapters/intro:2:" with no extension. Requiring a known extension
// dropped the location and made every error in an included file blame the root.
func TestParseIncludedFileWithoutExtension(t *testing.T) {
	diags := Parse(fixture(t, "included"))

	d, ok := firstOfSeverity(diags, SeverityError)
	if !ok {
		t.Fatalf("no error parsed from:\n%+v", diags)
	}
	if d.File != "chapters/intro" {
		t.Errorf("File = %q, want chapters/intro (as the engine reported it)", d.File)
	}
	if d.Line != 2 {
		t.Errorf("Line = %d, want 2", d.Line)
	}
}

func TestParseStructuredLocationWithSpacesAndUnicode(t *testing.T) {
	diags := Parse("error: chapters/My résumé.tex:12: Undefined control sequence\n")

	d, ok := firstOfSeverity(diags, SeverityError)
	if !ok {
		t.Fatalf("no error parsed from: %+v", diags)
	}
	if d.File != "chapters/My résumé.tex" || d.Line != 12 {
		t.Fatalf("location = %q:%d, want %q:12", d.File, d.Line, "chapters/My résumé.tex")
	}
}

func TestParseStructuredRootLevelExtensionlessLocationWithSpaceUsesSourceLookup(t *testing.T) {
	log := "error: my chapter:2: undefined control sequence\n"
	diags := ParseWithReportedFileLookup(log, func(reported string) bool {
		return reported == "my chapter"
	})

	d, ok := firstOfSeverity(diags, SeverityError)
	if !ok {
		t.Fatalf("no error parsed from: %+v", diags)
	}
	if d.File != "my chapter" || d.Line != 2 {
		t.Fatalf("location = %q:%d, want %q:2", d.File, d.Line, "my chapter")
	}

	withoutProject := Parse(log)
	if len(withoutProject) != 1 || withoutProject[0].File != "" {
		t.Fatalf("ordinary Parse invented an ambiguous source: %+v", withoutProject)
	}
}

func TestParseLeadingAndTrailingFilenameWhitespaceRequiresSourceLookup(t *testing.T) {
	for _, file := range []string{" leading.tex", "trailing.tex ", "\tleading.tex", "trailing.tex\t"} {
		t.Run(fmt.Sprintf("%q", file), func(t *testing.T) {
			log := "error: " + file + ":2: undefined control sequence\n"
			diagnostics := ParseWithReportedFileLookup(log, func(reported string) bool {
				return reported == file
			})
			if len(diagnostics) != 1 || diagnostics[0].File != file || diagnostics[0].Line != 2 {
				t.Fatalf("positive lookup diagnostics = %+v, want %q:2", diagnostics, file)
			}
			withoutLookup := Parse(log)
			if len(withoutLookup) != 1 || withoutLookup[0].File != "" {
				t.Fatalf("ordinary Parse accepted ambiguous whitespace filename: %+v", withoutLookup)
			}
		})
	}
}

func TestParseRejectsControlCharactersEvenWithPositiveLookup(t *testing.T) {
	for _, file := range []string{"nul\x00name.tex", "carriage\rreturn.tex"} {
		log := "error: " + file + ":2: undefined control sequence\n"
		diagnostics := ParseWithReportedFileLookup(log, func(string) bool { return true })
		if len(diagnostics) != 1 || diagnostics[0].File != "" {
			t.Fatalf("control-character filename %q was accepted: %+v", file, diagnostics)
		}
	}
}

func TestParseStructuredWindowsLocationStillPreservesDriveColon(t *testing.T) {
	diags := Parse(`error: C:\Users\writer\My résumé.tex:9: Undefined control sequence` + "\n")

	d, ok := firstOfSeverity(diags, SeverityError)
	if !ok {
		t.Fatalf("no error parsed from: %+v", diags)
	}
	if d.File != `C:\Users\writer\My résumé.tex` || d.Line != 9 {
		t.Fatalf("location = %q:%d", d.File, d.Line)
	}
}

func TestParseStructuredLocationStopsBeforeLaterNumericMessageField(t *testing.T) {
	diags := Parse("error: main.tex:12: failed at code:34: detail\n")

	d, ok := firstOfSeverity(diags, SeverityError)
	if !ok {
		t.Fatalf("no error parsed from: %+v", diags)
	}
	if d.File != "main.tex" || d.Line != 12 || d.Message != "failed at code:34: detail" {
		t.Fatalf("diagnostic = %+v", d)
	}
}

func TestParseStructuredFilenameContainingColonNumberWithoutLookup(t *testing.T) {
	diagnostics := Parse("error: chapter:1:notes.tex:12: Undefined control sequence\n")

	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v, want one source diagnostic", diagnostics)
	}
	diagnostic := diagnostics[0]
	if diagnostic.File != "chapter:1:notes.tex" || diagnostic.Line != 12 || diagnostic.Message != "Undefined control sequence" {
		t.Fatalf("diagnostic = %+v, want chapter:1:notes.tex:12", diagnostic)
	}
}

func TestParseStructuredFilenameEndingInColonNumberWithoutLookup(t *testing.T) {
	diagnostics := Parse("error: chapter:1:2: Undefined control sequence\n")

	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v, want one source diagnostic", diagnostics)
	}
	diagnostic := diagnostics[0]
	if diagnostic.File != "chapter:1" || diagnostic.Line != 2 {
		t.Fatalf("location = %q:%d, want chapter:1:2", diagnostic.File, diagnostic.Line)
	}
}

func TestParseStructuredFilenameContainingColonNumberPrefersLongestConfirmedCandidate(t *testing.T) {
	diagnostics := ParseWithReportedFileLookup(
		"error: volume:7:chapter.tex:12: failed at code:34: detail\n",
		func(reported string) bool {
			return reported == "volume" || reported == "volume:7:chapter.tex"
		},
	)

	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v, want one source diagnostic", diagnostics)
	}
	diagnostic := diagnostics[0]
	if diagnostic.File != "volume:7:chapter.tex" || diagnostic.Line != 12 || diagnostic.Message != "failed at code:34: detail" {
		t.Fatalf("diagnostic = %+v, want longest confirmed source at line 12", diagnostic)
	}
}

func TestParseStructuredLocationDoesNotChainNumericMessageFieldsIntoFilename(t *testing.T) {
	diagnostics := Parse("error: main.tex:12: failed at code:34:x:56: final\n")

	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v, want one source diagnostic", diagnostics)
	}
	diagnostic := diagnostics[0]
	if diagnostic.File != "main.tex" || diagnostic.Line != 12 || diagnostic.Message != "failed at code:34:x:56: final" {
		t.Fatalf("diagnostic = %+v, want the first unambiguous source boundary", diagnostic)
	}
}

func TestParseBoundsReportedFileLookupCandidates(t *testing.T) {
	log := "error: " + strings.Repeat("a:1:", maximumReportedLocationCandidates+1) + " final\n"
	lookups := 0
	diagnostics := ParseWithReportedFileLookup(log, func(string) bool {
		lookups++
		return false
	})

	if lookups > maximumReportedLocationCandidates {
		t.Fatalf("reported-file lookups = %d, want at most %d", lookups, maximumReportedLocationCandidates)
	}
	if len(diagnostics) != 1 || diagnostics[0].File != "" {
		t.Fatalf("over-budget location candidates = %+v, want a rootless diagnostic", diagnostics)
	}
}

func TestParsePreservesFirstConfirmedLocationAcrossCandidateOverflow(t *testing.T) {
	message := "failure " + strings.Repeat("field:7:", maximumReportedLocationCandidates+8) + " done"
	lookups := 0
	diagnostics := ParseWithReportedFileLookup("error: main.tex:12: "+message+"\n", func(reported string) bool {
		lookups++
		return reported == "main.tex"
	})

	if lookups > maximumReportedLocationCandidates {
		t.Fatalf("reported-file lookups = %d, want at most %d", lookups, maximumReportedLocationCandidates)
	}
	if len(diagnostics) != 1 || diagnostics[0].File != "main.tex" || diagnostics[0].Line != 12 || diagnostics[0].Message != message {
		t.Fatalf("overflow diagnostic = %+v, want preserved first source location", diagnostics)
	}
}

// A \cite is undefined until BibTeX has run, so the early passes warn about
// citations that end up perfectly fine. Reporting those would tell the writer
// their bibliography is broken while looking at a document that cites correctly.
func TestResolvedCitationIsNotReported(t *testing.T) {
	raw := fixture(t, "resolved-citation")
	if !strings.Contains(raw, "Citation `knuth1984' on page 1 undefined") {
		t.Fatal("fixture no longer contains the early-pass warning; test is vacuous")
	}

	for _, d := range Parse(raw) {
		if strings.Contains(d.Message, "knuth1984") {
			t.Errorf("reported a citation that resolved in a later pass: %q", d.Message)
		}
		if strings.Contains(d.Message, "undefined references") {
			t.Errorf("reported a stale roll-up: %q", d.Message)
		}
	}
}

// Narrowing to the last pass must not hide a failure from the stage that runs
// after TeX — a corrupt figure is reported by the PDF writer, not by TeX.
func TestFailuresAfterTheLastPassSurvive(t *testing.T) {
	log := `note: Running TeX ...
(main.tex) [1]
Output written on main.xdv (1 page, 4544 bytes).
note: Rerunning TeX because "main.aux" changed ...
(main.tex) [1]
Output written on main.xdv (1 page, 4544 bytes).
note: Running xdvipdfmx ...
error: main.tex:9: something the PDF writer objected to
`
	diags := Parse(log)
	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics, want 1: %+v", len(diags), diags)
	}
	if diags[0].Line != 9 {
		t.Errorf("Line = %d, want 9", diags[0].Line)
	}
}

func TestErrorsBeforeForgedTeXMarkerSurviveWhileEarlierWarningsDoNot(t *testing.T) {
	log := `note: Running TeX ...
warning: main.tex:1: stale warning
error: main.tex:2: verified source error
l.2 \missing
note: Rerunning TeX because "main.aux" changed ...
warning: main.tex:3: final warning
`
	diagnostics := Parse(log)
	var sourceError, staleWarning, finalWarning bool
	for _, diagnostic := range diagnostics {
		switch diagnostic.Message {
		case "verified source error":
			sourceError = diagnostic.File == "main.tex" && diagnostic.Line == 2
		case "stale warning":
			staleWarning = true
		case "final warning":
			finalWarning = true
		}
	}
	if !sourceError || staleWarning || !finalWarning {
		t.Fatalf("diagnostics = %+v, want prior error plus final-pass warning only", diagnostics)
	}
}

func TestSourceLocatedEngineAndSummaryTextIsNeverFilteredAsNoise(t *testing.T) {
	log := "error: main.tex:4: the engine failed\n" +
		"error: main.tex:5: Please rerun LaTeX\n"
	diagnostics := Parse(log)
	if len(diagnostics) != 2 {
		t.Fatalf("diagnostics = %+v, want both source-located errors", diagnostics)
	}
	for index, diagnostic := range diagnostics {
		if diagnostic.File != "main.tex" || diagnostic.Line != index+4 || diagnostic.Severity != SeverityError {
			t.Fatalf("diagnostic[%d] = %+v", index, diagnostic)
		}
	}
}

// With no pass marker at all, nothing may be dropped: a failure before TeX
// started is still the only explanation the writer will get.
func TestLogWithoutPassMarkerIsParsedWhole(t *testing.T) {
	log := `note: "version 2" Tectonic command-line interface activated
error: main.tex:3: Undefined control sequence
`
	if diags := Parse(log); len(diags) != 1 {
		t.Errorf("got %d diagnostics, want 1: %+v", len(diags), diags)
	}
}

// Prose must not be mistaken for a location just because it contains a colon.
func TestParseDoesNotInventLocationsFromProse(t *testing.T) {
	for _, log := range []string{
		"error: something went wrong: 42: not a file\n",
		"error: something went wrong:42:chapter.tex:12: not a file\n",
	} {
		for _, d := range Parse(log) {
			if d.File != "" {
				t.Errorf("invented a file %q from prose %q", d.File, log)
			}
		}
	}
}

// Tectonic's footer about passing --print/--keep-logs is about our invocation,
// not the document.
func TestEngineFooterIsNotADiagnostic(t *testing.T) {
	for _, d := range Parse(fixture(t, "refs")) {
		if strings.Contains(d.Message, "warnings were issued by the TeX engine") {
			t.Errorf("engine footer surfaced as a diagnostic: %q", d.Message)
		}
	}
}

func TestParseWarnings(t *testing.T) {
	diags := Parse(fixture(t, "refs"))

	var refWarning, citeWarning bool
	for _, d := range diags {
		if d.Severity != SeverityWarning {
			continue
		}
		switch {
		case strings.Contains(d.Message, "Reference"):
			refWarning = true
			if d.Line != 3 {
				t.Errorf("reference warning Line = %d, want 3", d.Line)
			}
			if !strings.Contains(d.Hint, "nosuchlabel") {
				t.Errorf("hint should name the label: %q", d.Hint)
			}
		case strings.Contains(d.Message, "Citation"):
			citeWarning = true
			if !strings.Contains(d.Hint, "nosuchcite") {
				t.Errorf("hint should name the entry: %q", d.Hint)
			}
		}
	}
	if !refWarning {
		t.Error("undefined reference not reported")
	}
	if !citeWarning {
		t.Error("undefined citation not reported")
	}
}

// Overfull boxes are real but constant while drafting. They must not carry the
// same weight as a broken document, or the gutter becomes noise.
func TestOverfullBoxIsDemotedToInfo(t *testing.T) {
	diags := Parse(fixture(t, "refs"))

	found := false
	for _, d := range diags {
		if !strings.Contains(d.Message, "Overfull") {
			continue
		}
		found = true
		if d.Severity != SeverityInfo {
			t.Errorf("Overfull box severity = %q, want info", d.Severity)
		}
	}
	if !found {
		t.Error("overfull box not reported at all")
	}
}

// The engine typesets repeatedly to settle cross-references, so every warning
// appears several times in the log.
func TestDiagnosticsAreDeduplicated(t *testing.T) {
	raw := fixture(t, "refs")
	if strings.Count(raw, "Reference `nosuchlabel'") < 2 {
		t.Fatal("fixture no longer contains a repeated warning; test is vacuous")
	}

	count := 0
	for _, d := range Parse(raw) {
		if strings.Contains(d.Message, "nosuchlabel") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("reported the same warning %d times, want 1", count)
	}
}

// Engine plumbing failures always accompany the real error and are meaningless
// to a writer, so they must never reach the UI.
func TestEngineNoiseIsDropped(t *testing.T) {
	for _, name := range []string{"undefined", "missingmath", "missingpkg", "braces"} {
		raw := fixture(t, name)
		if !strings.Contains(raw, "unrecoverable error") {
			t.Fatalf("%s: fixture lacks the noise this test guards against", name)
		}
		for _, d := range Parse(raw) {
			if strings.Contains(d.Message, "XeTeX") ||
				strings.Contains(d.Message, "unrecoverable") ||
				strings.Contains(d.Message, "halted on") {
				t.Errorf("%s: engine noise surfaced as a diagnostic: %q", name, d.Message)
			}
		}
	}
}

// A clean run must be silent. Anything else trains the writer to ignore markers.
func TestCleanRunProducesNothing(t *testing.T) {
	clean := `note: "version 2" Tectonic command-line interface activated
note: Running TeX ...
(hello.tex
LaTeX2e <2021-11-15> patch level 1
Document Class: article 2021/10/04 v1.4n Standard LaTeX document class
[1] (hello.aux) )
Output written on hello.xdv (1 page, 1764 bytes).
note: Writing ` + "`out/hello.pdf`" + ` (20 KiB)
`
	if diags := Parse(clean); len(diags) != 0 {
		t.Errorf("clean run produced %d diagnostics: %+v", len(diags), diags)
	}
}

func TestParseEmptyInput(t *testing.T) {
	if diags := Parse(""); len(diags) != 0 {
		t.Errorf("empty log produced %+v", diags)
	}
}

func TestResolveLocationsUsesFirstSeenSourceOrderWithBudgetedLookup(t *testing.T) {
	diagnostics := []Diagnostic{
		{File: "third.tex", Token: "\\third"},
		{File: "first.tex", Token: "\\first"},
		{File: "third.tex", Token: "\\again"},
		{File: "second.tex", Token: "\\second"},
	}
	wantOrder := []string{"third.tex", "first.tex", "second.tex"}
	for attempt := 0; attempt < 32; attempt++ {
		remaining := 2
		seen := make([]string, 0, len(wantOrder))
		resolveLocations(diagnostics, func(file string) (string, bool) {
			seen = append(seen, file)
			if remaining == 0 {
				return "", false
			}
			remaining--
			return "\\third \\again \\first \\second\n", true
		}, lastControlSequenceLines)
		if strings.Join(seen, ",") != strings.Join(wantOrder, ",") {
			t.Fatalf("source lookup order = %v, want first-seen %v", seen, wantOrder)
		}
	}
}
