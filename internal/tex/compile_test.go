package tex

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// requireEngine skips when no engine is installed, so `go test ./...` stays
// green on a machine (or CI job) without Tectonic.
func requireEngine(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath(DefaultBinary); err != nil {
		t.Skipf("%s not on PATH", DefaultBinary)
	}
}

// stageFixture copies a testdata document into a fresh temp dir and returns its
// path, so a compile never writes artifacts back into the repo.
func stageFixture(t *testing.T, name string) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	dst := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(dst, src, 0o644); err != nil {
		t.Fatalf("stage fixture: %v", err)
	}
	return dst
}

func TestCompileHelloWorld(t *testing.T) {
	requireEngine(t)

	main := stageFixture(t, "hello.tex")
	outDir := filepath.Join(filepath.Dir(main), "out")

	res, err := NewCompiler().Compile(context.Background(), main, outDir)
	if err != nil {
		t.Fatalf("Compile: %v\n%s", err, res.RawLog)
	}
	if !res.Success {
		t.Fatalf("expected success, got exit %d\n%s", res.ExitCode, res.RawLog)
	}

	// The three artifacts the rest of the app is built on.
	if res.PDFPath == "" {
		t.Error("no PDF produced")
	}
	if res.LogPath == "" {
		t.Error("no log produced (--keep-logs regression?)")
	}
	if res.SyncTeXPath == "" {
		t.Error("no synctex produced (--synctex regression?)")
	}

	if res.PDFPath != "" {
		got, err := os.ReadFile(res.PDFPath)
		if err != nil {
			t.Fatalf("read PDF: %v", err)
		}
		if !strings.HasPrefix(string(got), "%PDF-") {
			t.Errorf("output is not a PDF (first bytes: %q)", got[:min(8, len(got))])
		}
		if len(got) < 1000 {
			t.Errorf("PDF suspiciously small: %d bytes", len(got))
		}
	}
	t.Logf("compiled in %s", res.Duration)
}

// A broken document is not a failure of ours: the engine ran and gave a verdict,
// so Compile returns nil error with Success=false. Phase 2 depends on this split.
func TestCompileBrokenDocumentReportsFailureNotError(t *testing.T) {
	requireEngine(t)

	main := stageFixture(t, "broken.tex")
	outDir := filepath.Join(filepath.Dir(main), "out")

	res, err := NewCompiler().Compile(context.Background(), main, outDir)
	if err != nil {
		t.Fatalf("expected a verdict, got error: %v", err)
	}
	if res.Success {
		t.Fatal("expected the document to fail")
	}
	if res.ExitCode == 0 {
		t.Error("expected a non-zero engine exit code")
	}
	// The raw log must carry something a human (and later, a parser) can use.
	if !strings.Contains(res.RawLog, "Undefined control sequence") {
		t.Errorf("log lacks the actual TeX error:\n%s", res.RawLog)
	}
	if !strings.Contains(res.RawLog, "thisCommandDoesNotExist") {
		t.Errorf("log lacks the offending command:\n%s", res.RawLog)
	}
}

func TestCompileMissingEngine(t *testing.T) {
	c := &Compiler{Binary: "definitely-not-a-real-tex-engine"}
	_, err := c.Compile(context.Background(), "testdata/hello.tex", t.TempDir())
	if !errors.Is(err, ErrEngineMissing) {
		t.Errorf("got %v, want ErrEngineMissing", err)
	}
}

func TestCompileMissingMainFile(t *testing.T) {
	requireEngine(t)
	_, err := NewCompiler().Compile(context.Background(), "testdata/no-such-file.tex", t.TempDir())
	if err == nil {
		t.Fatal("expected an error for a missing main file")
	}
}

func TestVersion(t *testing.T) {
	requireEngine(t)
	v, err := NewCompiler().Version(context.Background())
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v == "" {
		t.Fatal("empty version string")
	}
	t.Logf("engine: %s", v)
}
