package project

import "testing"

func TestParseRelativePathAcceptsCanonicalProjectPaths(t *testing.T) {
	t.Parallel()
	for _, value := range []string{
		"main.tex",
		"chapters/one.tex",
		"Résumé/δ.tex",
		"name:with-colon.tex",
	} {
		value := value
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			path, err := ParseRelativePath(value)
			if err != nil {
				t.Fatalf("ParseRelativePath(%q): %v", value, err)
			}
			if path.String() != value {
				t.Fatalf("path = %q, want %q", path, value)
			}
		})
	}
}

func TestParseRelativePathRejectsUnsafeOrNoncanonicalValues(t *testing.T) {
	t.Parallel()
	for _, value := range []string{
		"",
		".",
		"..",
		"../main.tex",
		"chapters/../../main.tex",
		"chapters/../main.tex",
		"/tmp/main.tex",
		`chapters\main.tex`,
		"chapters//main.tex",
		"chapters/./main.tex",
		"main.tex/",
		"main\x00.tex",
	} {
		value := value
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			if _, err := ParseRelativePath(value); err == nil {
				t.Fatalf("ParseRelativePath(%q) succeeded", value)
			}
		})
	}
}
