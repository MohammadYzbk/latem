package vcs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// sourceRepo builds a real repository on disk with one commit, so the tests
// exercise go-git rather than a stand-in.
func sourceRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	repository, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.tex"), []byte("\\documentclass{article}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	worktree, err := repository.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Add("main.tex"); err != nil {
		t.Fatal(err)
	}
	_, err = worktree.Commit("initial", &git.CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "test@example.com", When: time.Now()},
	})
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCloneAndStatus(t *testing.T) {
	source := sourceRepo(t)
	destination := filepath.Join(t.TempDir(), "clone")

	if err := Clone(context.Background(), source, destination, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destination, "main.tex")); err != nil {
		t.Fatalf("the cloned working copy has no content: %v", err)
	}

	state := Status(destination)
	if state.Error != "" {
		t.Fatalf("status error: %s", state.Error)
	}
	if !state.Repository {
		t.Error("a fresh clone was not recognised as a repository")
	}
	if state.Branch == "" {
		t.Error("no branch reported")
	}
	if state.Dirty {
		t.Error("a fresh clone was reported dirty")
	}
	if state.Remote != source {
		t.Errorf("remote: got %q, want %q", state.Remote, source)
	}
}

// The dirty flag is the whole point of showing Git state: it has to notice both
// an edit to a tracked file and a brand-new one.
func TestStatusNoticesChanges(t *testing.T) {
	source := sourceRepo(t)
	destination := filepath.Join(t.TempDir(), "clone")
	if err := Clone(context.Background(), source, destination, ""); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(destination, "main.tex"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !Status(destination).Dirty {
		t.Error("an edited tracked file did not show as dirty")
	}

	// Start clean again, then add an untracked file.
	if err := os.RemoveAll(destination); err != nil {
		t.Fatal(err)
	}
	if err := Clone(context.Background(), source, destination, ""); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "notes.tex"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !Status(destination).Dirty {
		t.Error("an untracked file did not show as dirty")
	}
}

// Build artifacts live outside the project, but a repo may still ignore things.
// An ignored file must not make the project look permanently unsaved.
func TestStatusRespectsGitignore(t *testing.T) {
	source := sourceRepo(t)
	destination := filepath.Join(t.TempDir(), "clone")
	if err := Clone(context.Background(), source, destination, ""); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(destination, ".gitignore"), []byte("*.pdf\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	worktree, err := openWorktree(destination)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Add(".gitignore"); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Commit("ignore pdfs", &git.CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "test@example.com", When: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}

	if Status(destination).Dirty {
		t.Fatal("the clone was dirty before the ignored file was written")
	}
	if err := os.WriteFile(filepath.Join(destination, "main.pdf"), []byte("%PDF"), 0o644); err != nil {
		t.Fatal(err)
	}
	if Status(destination).Dirty {
		t.Error("an ignored file was reported as a change")
	}
}

// Most projects are plain folders. That is not an error state.
func TestStatusOnPlainFolder(t *testing.T) {
	state := Status(t.TempDir())
	if state.Repository {
		t.Error("a plain folder was reported as a repository")
	}
	if state.Error != "" {
		t.Errorf("a plain folder produced an error: %s", state.Error)
	}
}

// Cloning over existing work would destroy it.
func TestCloneRefusesNonEmptyDestination(t *testing.T) {
	source := sourceRepo(t)
	destination := t.TempDir()
	if err := os.WriteFile(filepath.Join(destination, "mine.tex"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := Clone(context.Background(), source, destination, "")
	if !errors.Is(err, ErrExists) {
		t.Fatalf("got %v, want ErrExists", err)
	}
	if _, err := os.Stat(filepath.Join(destination, "mine.tex")); err != nil {
		t.Errorf("the existing file was destroyed: %v", err)
	}
}

// A failed clone must not leave a partial directory that then looks like
// "already exists" on the next attempt.
func TestFailedCloneLeavesNothingBehind(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "clone")

	if err := Clone(context.Background(), filepath.Join(t.TempDir(), "not-a-repo"), destination, ""); err == nil {
		t.Fatal("cloning a non-repository succeeded")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Errorf("a partial clone was left at %s", destination)
	}
}

func TestSlug(t *testing.T) {
	for _, testCase := range []struct {
		url, owner, name string
	}{
		{"https://github.com/octocat/Hello-World.git", "octocat", "Hello-World"},
		{"https://github.com/octocat/Hello-World", "octocat", "Hello-World"},
		{"git@github.com:octocat/Hello-World.git", "octocat", "Hello-World"},
		{"ssh://git@github.com/octocat/Hello-World.git", "octocat", "Hello-World"},
		{"https://github.com/octocat/Hello-World/", "octocat", "Hello-World"},
	} {
		owner, name, err := Slug(testCase.url)
		if err != nil {
			t.Errorf("%s: %v", testCase.url, err)
			continue
		}
		if owner != testCase.owner || name != testCase.name {
			t.Errorf("%s: got %s/%s, want %s/%s", testCase.url, owner, name, testCase.owner, testCase.name)
		}
	}
}

// The slug names a directory, so a crafted URL must not be able to point the
// clone somewhere else on disk.
func TestSlugRefusesPathEscapes(t *testing.T) {
	for _, bad := range []string{
		"", "https://github.com/", "https://github.com/onlyone",
		"https://github.com/../etc/passwd",
		"https://github.com/owner/..",
	} {
		if owner, name, err := Slug(bad); err == nil {
			t.Errorf("%q was accepted as %s/%s", bad, owner, name)
		}
	}
}

// The token must never survive into an error message shown to the user.
func TestRedactRemovesToken(t *testing.T) {
	err := redact(errors.New("auth failed for https://x-access-token:gho_secret@github.com/o/r"), "gho_secret")
	if got := err.Error(); contains(got, "gho_secret") {
		t.Errorf("the token survived redaction: %s", got)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		(haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func openWorktree(dir string) (*git.Worktree, error) {
	repository, err := git.PlainOpen(dir)
	if err != nil {
		return nil, err
	}
	return repository.Worktree()
}
