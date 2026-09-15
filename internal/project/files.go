package project

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// maxTextFileBytes bounds what we will load into the editor. A buffer big enough
// to freeze the webview is worse than a refusal.
const maxTextFileBytes = 8 << 20 // 8 MiB

// ErrNotText means the file is not something the editor should open.
var ErrNotText = errors.New("project: file is not editable text")

// ErrExists means the target path is already taken.
var ErrExists = errors.New("project: already exists")

// ReadText loads a file for editing, refusing anything that is not text. The
// check is on content, not extension: a .tex file full of binary is still not
// editable, and a stray extension should not corrupt someone's file when the
// editor saves it back.
func (p *Project) ReadText(rel string) (string, error) {
	abs, err := p.Abs(rel)
	if err != nil {
		return "", err
	}
	rootRelative, err := filepath.Rel(p.root, abs)
	if err != nil {
		return "", fmt.Errorf("project: read: %w", err)
	}
	root, err := os.OpenRoot(p.root)
	if err != nil {
		return "", fmt.Errorf("project: read: %w", err)
	}
	defer root.Close()
	file, err := openProjectTextFile(root, rootRelative)
	if err != nil {
		return "", fmt.Errorf("project: read: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("project: read: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("project: %q is not a regular file", rel)
	}
	if info.Size() > maxTextFileBytes {
		return "", textFileTooLargeError(rel, info.Size())
	}
	if p.beforeTextRead != nil {
		p.beforeTextRead(rel)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxTextFileBytes+1))
	if err != nil {
		return "", fmt.Errorf("project: read: %w", err)
	}
	if len(data) > maxTextFileBytes {
		return "", textFileTooLargeError(rel, int64(len(data)))
	}
	if !looksLikeText(data) {
		return "", fmt.Errorf("%w: %s", ErrNotText, rel)
	}
	return string(data), nil
}

// EditableTextSize performs the cheap admission checks shared by editor reads
// and diagnostic attribution. Content still has to pass ReadText before the
// caller treats the file as editable text.
func (p *Project) EditableTextSize(rel string) (int64, error) {
	abs, err := p.Abs(rel)
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return 0, fmt.Errorf("project: read: %w", err)
	}
	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("project: %q is not a regular file", rel)
	}
	if info.Size() > maxTextFileBytes {
		return 0, textFileTooLargeError(rel, info.Size())
	}
	return info.Size(), nil
}

func textFileTooLargeError(relative string, size int64) error {
	return fmt.Errorf("%w: %s is %d MiB, too large to edit", ErrNotText, relative, size/(1<<20))
}

// looksLikeText rejects data containing NUL bytes or invalid UTF-8, which is how
// every editor has decided this question for forty years.
func looksLikeText(data []byte) bool {
	if bytes.IndexByte(data, 0) >= 0 {
		return false
	}
	return utf8.Valid(data)
}

// WriteText saves a file, creating parent directories as needed.
func (p *Project) WriteText(rel, content string) error {
	abs, err := p.Abs(rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return fmt.Errorf("project: create parent directory: %w", err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		return fmt.Errorf("project: write: %w", err)
	}
	return nil
}

// Create makes a new empty file, or a directory when dir is true. It refuses to
// overwrite: creating and clobbering are different intentions, and only one of
// them is what a "New file" button means.
func (p *Project) Create(rel string, dir bool) error {
	if err := validateName(filepath.Base(rel)); err != nil {
		return err
	}
	abs, err := p.Abs(rel)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(abs); err == nil {
		return fmt.Errorf("%w: %s", ErrExists, rel)
	}

	if dir {
		if err := os.MkdirAll(abs, 0o755); err != nil {
			return fmt.Errorf("project: create directory: %w", err)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return fmt.Errorf("project: create parent directory: %w", err)
	}
	f, err := os.OpenFile(abs, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("project: create file: %w", err)
	}
	return f.Close()
}

// Rename moves a file or directory within the project. Both ends are validated,
// so a rename cannot be used to write outside the root.
func (p *Project) Rename(from, to string) error {
	if err := validateName(filepath.Base(to)); err != nil {
		return err
	}
	fromAbs, err := p.Abs(from)
	if err != nil {
		return err
	}
	toAbs, err := p.Abs(to)
	if err != nil {
		return err
	}
	if fromAbs == toAbs {
		return nil
	}
	if _, err := os.Lstat(toAbs); err == nil {
		return fmt.Errorf("%w: %s", ErrExists, to)
	}
	if err := os.MkdirAll(filepath.Dir(toAbs), 0o755); err != nil {
		return fmt.Errorf("project: create parent directory: %w", err)
	}
	if err := os.Rename(fromAbs, toAbs); err != nil {
		return fmt.Errorf("project: rename: %w", err)
	}

	// Keep the compile target pointing at the same document, including when a
	// parent directory was renamed underneath it.
	if p.rootFile != "" {
		fromSlash := filepath.ToSlash(filepath.Clean(from))
		toSlash := filepath.ToSlash(filepath.Clean(to))
		switch {
		case p.rootFile == fromSlash:
			p.rootFile = toSlash
		case strings.HasPrefix(p.rootFile, fromSlash+"/"):
			p.rootFile = toSlash + strings.TrimPrefix(p.rootFile, fromSlash)
		}
	}
	return nil
}

// Delete removes a file, or a directory and everything in it.
func (p *Project) Delete(rel string) error {
	abs, err := p.Abs(rel)
	if err != nil {
		return err
	}
	// Deleting the project root itself is never what was meant.
	if abs == p.root {
		return fmt.Errorf("project: refusing to delete the project directory")
	}
	if err := os.RemoveAll(abs); err != nil {
		return fmt.Errorf("project: delete: %w", err)
	}

	// The compile target may have just been deleted, in which case the caller
	// needs to be told rather than left compiling a file that is gone.
	if p.rootFile != "" {
		relSlash := filepath.ToSlash(filepath.Clean(rel))
		if p.rootFile == relSlash || strings.HasPrefix(p.rootFile, relSlash+"/") {
			p.rootFile = ""
		}
	}
	return nil
}

// validateName rejects names that would be confusing or dangerous. Path
// separators are the important one: "New file" must not silently create a
// directory tree, and "../x" must not resolve upward.
func validateName(name string) error {
	switch {
	case name == "", name == "." || name == "..":
		return fmt.Errorf("project: %q is not a valid name", name)
	case strings.ContainsAny(name, `/\`):
		return fmt.Errorf("project: name %q must not contain a path separator", name)
	case strings.ContainsRune(name, 0):
		return fmt.Errorf("project: name contains a NUL byte")
	case strings.HasPrefix(name, "."):
		// Hidden files are filtered from the tree, so creating one would produce
		// a file the writer cannot see.
		return fmt.Errorf("project: name %q would be hidden from the file tree", name)
	}
	return nil
}

// Exists reports whether a project-relative path exists.
func (p *Project) Exists(rel string) bool {
	abs, err := p.Abs(rel)
	if err != nil {
		return false
	}
	_, err = os.Lstat(abs)
	return err == nil
}

// StatKind classifies an existing entry, for deciding how to open it.
func (p *Project) StatKind(rel string) (Kind, error) {
	abs, err := p.Abs(rel)
	if err != nil {
		return KindBinary, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return KindBinary, fmt.Errorf("project: stat: %w", err)
	}
	if info.IsDir() {
		return KindText, fs.ErrInvalid
	}
	return KindOf(info.Name()), nil
}
