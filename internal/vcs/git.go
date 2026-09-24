// Package vcs is the Git layer: cloning a repository and reporting where the
// working copy stands.
//
// It uses go-git rather than shelling out, which keeps the zero-config promise
// — there is no requirement that the user has a `git` binary. go-git does
// stumble on some real-world repositories, so everything here stays behind a
// small surface that a CLI-backed implementation could satisfy instead.
package vcs

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

// State is what the UI shows about a working copy.
type State struct {
	// Repository is false for an ordinary folder, which is a perfectly normal
	// project — the Git panel simply has nothing to say about it.
	Repository bool `json:"repository"`
	// Branch is the checked-out branch, empty when detached or unborn.
	Branch string `json:"branch"`
	// Detached means HEAD points at a commit rather than a branch.
	Detached bool `json:"detached"`
	// Unborn means the repository has no commits yet.
	Unborn bool `json:"unborn"`
	// Dirty means there is at least one uncommitted change, ignoring whatever
	// .gitignore excludes.
	Dirty bool `json:"dirty"`
	// Remote is origin's URL, empty if there is no origin.
	Remote string `json:"remote"`
	// Error explains why the state could not be read. The rest of the struct
	// is still safe to render.
	Error string `json:"error"`
}

// ErrExists means the destination already holds something.
var ErrExists = errors.New("vcs: destination already exists")

// Clone copies a repository into dir, which must not already exist.
//
// The token, when present, is sent as HTTP basic auth, which is how GitHub
// accepts one over https. It is never written to the repository config: go-git
// stores only the URL, so the token does not end up in .git/config where a
// later `git push` from a terminal would leak it into a credential helper.
func Clone(ctx context.Context, cloneURL, dir, token string) error {
	if info, err := os.Stat(dir); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("%w: %s", ErrExists, dir)
		}
		empty, err := isEmptyDir(dir)
		if err != nil {
			return err
		}
		if !empty {
			return fmt.Errorf("%w: %s", ErrExists, dir)
		}
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return fmt.Errorf("vcs: create parent directory: %w", err)
	}

	options := &git.CloneOptions{URL: cloneURL}
	if token != "" {
		// GitHub ignores the username for token auth, but it must not be empty.
		options.Auth = &githttp.BasicAuth{Username: "x-access-token", Password: token}
	}

	if _, err := git.PlainCloneContext(ctx, dir, false, options); err != nil {
		// A failed clone leaves a partial directory behind, which would then
		// look like "already exists" on the next attempt.
		_ = os.RemoveAll(dir)
		return fmt.Errorf("vcs: clone: %w", redact(err, token))
	}
	return nil
}

// Status reports on the working copy at dir.
//
// A directory that is not a repository is not an error: most projects are not.
func Status(dir string) State {
	repository, err := git.PlainOpen(dir)
	if err != nil {
		if errors.Is(err, git.ErrRepositoryNotExists) {
			return State{}
		}
		return State{Error: fmt.Sprintf("could not read the repository: %v", err)}
	}

	state := State{Repository: true}

	if remote, err := repository.Remote("origin"); err == nil {
		if urls := remote.Config().URLs; len(urls) > 0 {
			state.Remote = urls[0]
		}
	}

	head, err := repository.Head()
	switch {
	case errors.Is(err, plumbing.ErrReferenceNotFound):
		// A repository with no commits. Cloning an empty repo lands here, and
		// it is a normal place to start writing.
		state.Unborn = true
	case err != nil:
		state.Error = fmt.Sprintf("could not read HEAD: %v", err)
		return state
	case head.Name().IsBranch():
		state.Branch = head.Name().Short()
	default:
		state.Detached = true
	}

	worktree, err := repository.Worktree()
	if err != nil {
		state.Error = fmt.Sprintf("could not read the working tree: %v", err)
		return state
	}
	status, err := worktree.Status()
	if err != nil {
		state.Error = fmt.Sprintf("could not read the working tree status: %v", err)
		return state
	}
	state.Dirty = !status.IsClean()

	return state
}

// Slug turns a clone URL into the "owner/name" pair used to lay out the
// managed clone directory. It accepts the https and ssh forms GitHub offers.
func Slug(cloneURL string) (owner, name string, err error) {
	trimmed := strings.TrimSpace(cloneURL)
	if trimmed == "" {
		return "", "", errors.New("vcs: empty clone URL")
	}

	var repoPath string
	switch {
	case strings.HasPrefix(trimmed, "git@"), strings.HasPrefix(trimmed, "ssh://"):
		// git@github.com:owner/name.git
		if at := strings.LastIndex(trimmed, ":"); at >= 0 && !strings.HasPrefix(trimmed, "ssh://") {
			repoPath = trimmed[at+1:]
			break
		}
		parsed, parseErr := url.Parse(trimmed)
		if parseErr != nil {
			return "", "", fmt.Errorf("vcs: %w", parseErr)
		}
		repoPath = parsed.Path
	default:
		parsed, parseErr := url.Parse(trimmed)
		if parseErr != nil {
			return "", "", fmt.Errorf("vcs: %w", parseErr)
		}
		repoPath = parsed.Path
	}

	repoPath = strings.TrimSuffix(strings.Trim(repoPath, "/"), ".git")
	parts := strings.Split(repoPath, "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("vcs: %q does not look like a repository URL", cloneURL)
	}
	// Every segment is checked, not just the two that become directory names:
	// a path containing ".." is malformed, and quietly taking its tail would
	// turn nonsense into a plausible-looking owner and repository.
	for _, part := range parts {
		if !safeSegment(part) {
			return "", "", fmt.Errorf("vcs: %q does not look like a repository URL", cloneURL)
		}
	}

	return parts[len(parts)-2], parts[len(parts)-1], nil
}

// safeSegment keeps a URL-derived name from escaping the clone directory.
func safeSegment(segment string) bool {
	if segment == "" || segment == "." || segment == ".." {
		return false
	}
	if strings.ContainsAny(segment, `/\`) || strings.ContainsRune(segment, 0) {
		return false
	}
	return segment == path.Clean(segment)
}

func isEmptyDir(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, fmt.Errorf("vcs: read directory: %w", err)
	}
	return len(entries) == 0, nil
}

// redact keeps a token out of an error message. go-git echoes the remote URL
// on failure, and a URL can carry credentials.
func redact(err error, token string) error {
	if err == nil || token == "" {
		return err
	}
	text := err.Error()
	if !strings.Contains(text, token) {
		return err
	}
	return errors.New(strings.ReplaceAll(text, token, "***"))
}
