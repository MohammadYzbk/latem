// Package tex orchestrates an external TeX engine. It never implements TeX
// itself: it builds a command line, runs it, and reports where the artifacts
// landed.
//
// Phase 0 scope: prove that a hello-world document compiles from Go and that we
// get back a PDF, a log, and SyncTeX data. Log *parsing* is Phase 2; SyncTeX
// *parsing* is Phase 4. This package only guarantees the files exist.
package tex

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DefaultBinary is the engine looked up on PATH when none is configured.
// Phase 9 replaces this with a Tectonic binary bundled inside the app.
const DefaultBinary = "tectonic"

// Compiler runs a TeX engine as a subprocess.
type Compiler struct {
	// Binary is the engine executable. Empty means DefaultBinary.
	Binary string

	// Timeout bounds a single run. Zero means DefaultTimeout.
	Timeout time.Duration

	// Untrusted disables known-insecure engine features (notably shell-escape).
	// Default true: a project can be a freshly cloned repo whose .tex we have
	// never read. Documents that genuinely need shell-escape (minted, gnuplot)
	// must opt out per project.
	Untrusted bool

	// OnlyCached forbids network access, using just the local resource cache.
	// Useful for offline runs once the cache is warm.
	OnlyCached bool
}

// DefaultTimeout is generous: a cold first run downloads support files.
const DefaultTimeout = 3 * time.Minute

// NewCompiler returns a Compiler with the safe defaults.
func NewCompiler() *Compiler {
	return &Compiler{Binary: DefaultBinary, Timeout: DefaultTimeout, Untrusted: true}
}

// Result describes one engine invocation.
//
// Note the split between Result.Success and the error returned by Compile:
// Success=false means the engine ran and the *document* is broken (the normal
// case while writing LaTeX — Phase 2 turns RawLog into inline markers). A
// non-nil error means we could not get a verdict at all (missing binary,
// timeout, cancelled).
type Result struct {
	Success  bool
	Duration time.Duration

	// PDFPath is the PDF at the expected output location, or "" if there is
	// none. It can be non-empty even when Success is false, for two different
	// reasons: TeX often emits a partial PDF alongside errors, and a failed run
	// leaves any PDF from a previous run untouched. Either way a stale-but-real
	// preview beats a blank pane — but see PDFStale before calling it current.
	PDFPath string

	// PDFStale reports that PDFPath was not written by this run: the file was
	// already there and the engine did not touch it. Callers that show it
	// anyway must not present it as the current output.
	PDFStale bool

	LogPath     string
	SyncTeXPath string

	// RawLog is the engine's combined stdout+stderr. Always shown verbatim as
	// the fallback when parsing finds nothing it recognises.
	RawLog string

	// ExitCode is the engine's exit status (0 on success).
	ExitCode int
}

// ErrEngineMissing means the configured engine was not found on PATH.
var ErrEngineMissing = errors.New("tex: engine binary not found")

// Compile typesets mainFile (an absolute or cwd-relative .tex path) and writes
// artifacts to outDir, which is created if needed. mainFile's directory is the
// working directory, so \input and \includegraphics resolve relative to it.
func (c *Compiler) Compile(ctx context.Context, mainFile, outDir string) (*Result, error) {
	bin := c.Binary
	if bin == "" {
		bin = DefaultBinary
	}
	if _, err := exec.LookPath(bin); err != nil {
		return nil, fmt.Errorf("%w: %q", ErrEngineMissing, bin)
	}

	absMain, err := filepath.Abs(mainFile)
	if err != nil {
		return nil, fmt.Errorf("tex: resolve main file: %w", err)
	}
	if _, err := os.Stat(absMain); err != nil {
		return nil, fmt.Errorf("tex: main file: %w", err)
	}
	absOut, err := filepath.Abs(outDir)
	if err != nil {
		return nil, fmt.Errorf("tex: resolve out dir: %w", err)
	}
	if err := os.MkdirAll(absOut, 0o755); err != nil {
		return nil, fmt.Errorf("tex: create out dir: %w", err)
	}

	timeout := c.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// --keep-logs and --synctex are what make the artifacts survive the run;
	// without them the engine cleans up.
	args := (Invocation{
		MainFile:        absMain,
		OutputDirectory: absOut,
		Untrusted:       c.Untrusted,
		OnlyCached:      c.OnlyCached,
	}).Arguments()

	// Note the PDF's state before the run, so a failed compile that leaves an
	// older PDF in place can be reported as stale rather than as this run's work.
	base := strings.TrimSuffix(filepath.Base(absMain), filepath.Ext(absMain))
	pdfPath := filepath.Join(absOut, base+".pdf")
	pdfBefore := modTime(pdfPath)

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = filepath.Dir(absMain)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	start := time.Now()
	runErr := cmd.Run()
	res := &Result{
		Duration: time.Since(start),
		RawLog:   out.String(),
	}

	res.PDFPath = existingPath(pdfPath)
	res.PDFStale = res.PDFPath != "" && modTime(pdfPath).Equal(pdfBefore)
	res.LogPath = existingPath(filepath.Join(absOut, base+".log"))
	res.SyncTeXPath = existingPath(filepath.Join(absOut, base+".synctex.gz"))

	switch {
	case runErr == nil:
		res.Success = true
	case ctx.Err() != nil:
		// Timeout or caller cancellation: no verdict on the document.
		return res, fmt.Errorf("tex: engine did not finish: %w", ctx.Err())
	default:
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			return res, fmt.Errorf("tex: run engine: %w", runErr)
		}
		res.ExitCode = exitErr.ExitCode()
	}
	return res, nil
}

// Version reports the engine's self-identification, e.g. "Tectonic 0.17.0".
// Used by the app's first-run check.
func (c *Compiler) Version(ctx context.Context) (string, error) {
	bin := c.Binary
	if bin == "" {
		bin = DefaultBinary
	}
	if _, err := exec.LookPath(bin); err != nil {
		return "", fmt.Errorf("%w: %q", ErrEngineMissing, bin)
	}
	out, err := exec.CommandContext(ctx, bin, "--version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tex: version: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// existingPath returns p if it exists, otherwise "".
func existingPath(p string) string {
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// modTime returns p's modification time, or the zero time if it does not exist.
func modTime(p string) time.Time {
	fi, err := os.Stat(p)
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}
