package tex

import (
	"slices"
	"testing"
)

func TestInvocationOwnsTheAuditableTectonicArgumentShape(t *testing.T) {
	t.Parallel()
	invocation := Invocation{
		MainFile:        "/private/snapshot/main.tex",
		OutputDirectory: "/private/output",
		Bundle:          "/signed/runtime/offline.ttb",
		Untrusted:       true,
		OnlyCached:      true,
	}
	want := []string{
		"-X", "compile",
		"--bundle", "/signed/runtime/offline.ttb",
		"--untrusted",
		"--only-cached",
		"--outdir", "/private/output",
		"--synctex",
		"--keep-logs",
		"--print",
		"/private/snapshot/main.tex",
	}
	if got := invocation.Arguments(); !slices.Equal(got, want) {
		t.Fatalf("Arguments() = %q, want %q", got, want)
	}
	if slices.Contains(invocation.Arguments(), "--color") {
		t.Fatal("Tectonic 0.17.0 invocation contains unsupported --color")
	}
}

func TestInvocationOmitsOptionalSafetyFlagsOnlyWhenExplicitlyDisabled(t *testing.T) {
	t.Parallel()
	got := (Invocation{MainFile: "main.tex", OutputDirectory: "out"}).Arguments()
	want := []string{
		"-X", "compile",
		"--outdir", "out",
		"--synctex",
		"--keep-logs",
		"--print",
		"main.tex",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Arguments() = %q, want %q", got, want)
	}
}
