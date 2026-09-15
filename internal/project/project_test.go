package project

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// scaffold builds a project directory from a map of relative path to contents.
func scaffold(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const rootDoc = "\\documentclass{article}\n\\begin{document}\nhi\n\\end{document}\n"

// --- path safety -------------------------------------------------------------

// The frontend supplies these paths, so they are untrusted. A file tree that can
// be talked into writing outside the project is not a file tree.
func TestAbsRefusesEscapingPaths(t *testing.T) {
	p, err := Open(scaffold(t, map[string]string{"main.tex": rootDoc}))
	if err != nil {
		t.Fatal(err)
	}

	for _, rel := range []string{
		"../outside.tex",
		"../../etc/passwd",
		"chapters/../../outside.tex",
		"./../outside.tex",
		"",
	} {
		if _, err := p.Abs(rel); err == nil {
			t.Errorf("Abs(%q) was allowed; it must be refused", rel)
		}
	}

	if _, err := p.Abs("/etc/passwd"); !errors.Is(err, ErrOutsideProject) {
		t.Errorf("absolute path: got %v, want ErrOutsideProject", err)
	}
}

// "a/../b" is legitimate once cleaned, and refusing it would be needlessly
// strict — what matters is where it lands.
func TestAbsAllowsPathsThatCleanToInside(t *testing.T) {
	p, err := Open(scaffold(t, map[string]string{"chapters/one.tex": "hi"}))
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.Abs("chapters/../chapters/one.tex")
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	if want := filepath.Join(p.Root(), "chapters", "one.tex"); got != want {
		t.Errorf("Abs = %q, want %q", got, want)
	}
}

// A textual prefix check passes a symlink that points elsewhere, so the escape
// has to be caught after resolution too.
func TestAbsRefusesSymlinkOutOfProject(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	root := scaffold(t, map[string]string{"main.tex": rootDoc})
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}

	p, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Abs("link.txt"); !errors.Is(err, ErrOutsideProject) {
		t.Errorf("symlink out of the project: got %v, want ErrOutsideProject", err)
	}
	if _, err := p.ReadText("link.txt"); err == nil {
		t.Error("read through an escaping symlink was allowed")
	}
}

func TestRenameCannotEscape(t *testing.T) {
	p, err := Open(scaffold(t, map[string]string{"main.tex": rootDoc}))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Rename("main.tex", "../escaped.tex"); err == nil {
		t.Error("rename out of the project was allowed")
	}
	if !p.Exists("main.tex") {
		t.Error("the original file went missing after a refused rename")
	}
}

// --- root file detection -----------------------------------------------------

func TestDetectRootFileFindsDocumentClass(t *testing.T) {
	root := scaffold(t, map[string]string{
		"chapters/one.tex": "A chapter with no preamble.\n",
		"chapters/two.tex": "Another chapter.\n",
		"paper.tex":        rootDoc,
		"refs.bib":         "@book{x, title={y}}\n",
	})

	got, err := DetectRootFile(root)
	if err != nil {
		t.Fatalf("DetectRootFile: %v", err)
	}
	if got != "paper.tex" {
		t.Errorf("root file = %q, want paper.tex", got)
	}
}

// main.tex is the overwhelming convention, so it wins even when another
// candidate sorts earlier alphabetically.
func TestDetectRootFilePrefersMainTex(t *testing.T) {
	root := scaffold(t, map[string]string{
		"appendix.tex": rootDoc,
		"main.tex":     rootDoc,
	})
	got, err := DetectRootFile(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != "main.tex" {
		t.Errorf("root file = %q, want main.tex", got)
	}
}

// A top-level document beats one buried in a subdirectory, which is usually a
// standalone figure.
func TestDetectRootFilePrefersShallowerFile(t *testing.T) {
	root := scaffold(t, map[string]string{
		"figures/diagram.tex": rootDoc,
		"writeup.tex":         rootDoc,
	})
	got, err := DetectRootFile(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != "writeup.tex" {
		t.Errorf("root file = %q, want writeup.tex", got)
	}
}

// Draft files often keep an old \documentclass commented out; compiling those
// instead of the real document would be baffling.
func TestDetectRootFileIgnoresCommentedDocumentClass(t *testing.T) {
	root := scaffold(t, map[string]string{
		"aaa-draft.tex": "% \\documentclass{article}\nnotes to self\n",
		"real.tex":      rootDoc,
	})
	got, err := DetectRootFile(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != "real.tex" {
		t.Errorf("root file = %q, want real.tex", got)
	}
}

// Opening a folder must work even with no compilable document in it; the writer
// can pick one, or is about to create it.
func TestOpenWithoutRootFileStillOpens(t *testing.T) {
	p, err := Open(scaffold(t, map[string]string{"notes.txt": "nothing to compile"}))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if p.RootFile() != "" {
		t.Errorf("RootFile = %q, want empty", p.RootFile())
	}
}

func TestSetRootFile(t *testing.T) {
	p, err := Open(scaffold(t, map[string]string{
		"main.tex":  rootDoc,
		"other.tex": rootDoc,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.SetRootFile("other.tex"); err != nil {
		t.Fatalf("SetRootFile: %v", err)
	}
	if p.RootFile() != "other.tex" {
		t.Errorf("RootFile = %q, want other.tex", p.RootFile())
	}
	if err := p.SetRootFile("../escape.tex"); err == nil {
		t.Error("SetRootFile accepted a path outside the project")
	}
}

// --- tree --------------------------------------------------------------------

func TestTreeSortsAndClassifies(t *testing.T) {
	p, err := Open(scaffold(t, map[string]string{
		"main.tex":            rootDoc,
		"refs.bib":            "@book{x}",
		"figures/plot.png":    "\x89PNG\r\n\x1a\n",
		"chapters/intro.tex":  "intro",
		"notes.md":            "# notes",
		".hidden":             "tooling",
		".git/config":         "[core]",
		"node_modules/x/y.js": "junk",
	}))
	if err != nil {
		t.Fatal(err)
	}
	tree, err := p.Tree()
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}

	var names []string
	kinds := map[string]Kind{}
	for _, n := range tree.Children {
		names = append(names, n.Name)
		kinds[n.Name] = n.Kind
	}

	// Directories first, then files, alphabetical within each group.
	want := []string{"chapters", "figures", "main.tex", "notes.md", "refs.bib"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("tree order = %v, want %v", names, want)
	}
	if kinds["main.tex"] != KindTeX {
		t.Errorf("main.tex kind = %q", kinds["main.tex"])
	}
	if kinds["refs.bib"] != KindBib {
		t.Errorf("refs.bib kind = %q", kinds["refs.bib"])
	}

	for _, n := range tree.Children {
		if n.Name == ".git" || n.Name == "node_modules" || n.Name == ".hidden" {
			t.Errorf("%s should not appear in the tree", n.Name)
		}
		if n.Name == "figures" {
			if len(n.Children) != 1 || n.Children[0].Kind != KindImage {
				t.Errorf("figures children = %+v, want one image", n.Children)
			}
			if n.Children[0].Path != "figures/plot.png" {
				t.Errorf("child path = %q, want figures/plot.png", n.Children[0].Path)
			}
		}
	}
}

// --- file operations ---------------------------------------------------------

func TestCreateReadWrite(t *testing.T) {
	p, err := Open(scaffold(t, map[string]string{"main.tex": rootDoc}))
	if err != nil {
		t.Fatal(err)
	}

	if err := p.Create("chapters/one.tex", false); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got, err := p.ReadText("chapters/one.tex"); err != nil || got != "" {
		t.Errorf("new file: content %q, err %v", got, err)
	}
	if err := p.WriteText("chapters/one.tex", "Chapter one.\n"); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	if got, _ := p.ReadText("chapters/one.tex"); got != "Chapter one.\n" {
		t.Errorf("content = %q", got)
	}

	// Creating over an existing file would silently destroy work.
	if err := p.Create("chapters/one.tex", false); !errors.Is(err, ErrExists) {
		t.Errorf("Create over existing: got %v, want ErrExists", err)
	}
}

func TestCreateRejectsBadNames(t *testing.T) {
	p, err := Open(scaffold(t, map[string]string{"main.tex": rootDoc}))
	if err != nil {
		t.Fatal(err)
	}
	// "." and ".." are not names. A leading dot would create a file the tree
	// filters out, so the writer could never see or reopen it.
	for _, name := range []string{"..", ".", ".hidden.tex", "", "chapters/.draft.tex"} {
		if err := p.Create(name, false); err == nil {
			t.Errorf("Create(%q) was allowed", name)
		}
	}
}

// Create takes a path, not just a name, and makes the parents — "New file" in a
// tree naturally wants chapters/intro.tex. Confinement is Abs's job, and it
// still applies.
func TestCreateMakesParentDirectories(t *testing.T) {
	p, err := Open(scaffold(t, map[string]string{"main.tex": rootDoc}))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Create("parts/appendix/notes.tex", false); err != nil {
		t.Fatalf("Create nested: %v", err)
	}
	if !p.Exists("parts/appendix/notes.tex") {
		t.Error("nested file was not created")
	}
	if err := p.Create("../outside/notes.tex", false); err == nil {
		t.Error("Create escaped the project")
	}
}

// A binary file must not be loaded into a text buffer, or saving it back would
// corrupt it.
func TestReadTextRefusesBinary(t *testing.T) {
	p, err := Open(scaffold(t, map[string]string{
		"figures/plot.png": "\x89PNG\r\n\x1a\n\x00\x00binary",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.ReadText("figures/plot.png"); !errors.Is(err, ErrNotText) {
		t.Errorf("got %v, want ErrNotText", err)
	}
}

func TestReadTextRefusesFileThatGrowsPastLimitAfterAdmission(t *testing.T) {
	root := scaffold(t, map[string]string{"main.tex": "small\n"})
	p, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	p.beforeTextRead = func(relative string) {
		p.beforeTextRead = nil
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(relative)), bytes.Repeat([]byte("x"), maxTextFileBytes+1), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := p.ReadText("main.tex"); !errors.Is(err, ErrNotText) {
		t.Fatalf("ReadText after growth = %v, want ErrNotText", err)
	}
}

func TestRenameAndDelete(t *testing.T) {
	p, err := Open(scaffold(t, map[string]string{
		"main.tex":         rootDoc,
		"chapters/one.tex": "one",
	}))
	if err != nil {
		t.Fatal(err)
	}

	if err := p.Rename("chapters/one.tex", "chapters/intro.tex"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if p.Exists("chapters/one.tex") || !p.Exists("chapters/intro.tex") {
		t.Error("rename did not move the file")
	}

	if err := p.Delete("chapters"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if p.Exists("chapters") {
		t.Error("directory was not deleted")
	}
	if err := p.Delete(""); err == nil {
		t.Error("deleting the project root was allowed")
	}
}

// Renaming the root document must not leave the app compiling a path that no
// longer exists.
func TestRenameFollowsRootFile(t *testing.T) {
	p, err := Open(scaffold(t, map[string]string{"main.tex": rootDoc}))
	if err != nil {
		t.Fatal(err)
	}
	if p.RootFile() != "main.tex" {
		t.Fatalf("setup: root file = %q", p.RootFile())
	}
	if err := p.Rename("main.tex", "paper.tex"); err != nil {
		t.Fatal(err)
	}
	if p.RootFile() != "paper.tex" {
		t.Errorf("RootFile = %q, want paper.tex after rename", p.RootFile())
	}
}

func TestRenameFollowsRootFileAcrossParentDirectory(t *testing.T) {
	p, err := Open(scaffold(t, map[string]string{"src/main.tex": rootDoc}))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.SetRootFile("src/main.tex"); err != nil {
		t.Fatal(err)
	}
	if err := p.Rename("src", "source"); err != nil {
		t.Fatal(err)
	}
	if p.RootFile() != "source/main.tex" {
		t.Errorf("RootFile = %q, want source/main.tex", p.RootFile())
	}
}

// Deleting the root document has to be visible to the caller, not discovered as
// a mystifying compile failure.
func TestDeleteClearsRootFile(t *testing.T) {
	p, err := Open(scaffold(t, map[string]string{"main.tex": rootDoc}))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Delete("main.tex"); err != nil {
		t.Fatal(err)
	}
	if p.RootFile() != "" {
		t.Errorf("RootFile = %q, want empty after the root was deleted", p.RootFile())
	}
}
