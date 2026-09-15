// Package project models a project as a directory on disk.
//
// Real LaTeX is multi-file: a root document that \inputs chapters, a .bib, a
// figures directory. So the unit of work is a folder, and every file operation
// is expressed as a path relative to that folder's root.
//
// Those relative paths arrive from the frontend, which makes them untrusted
// input. Everything funnels through Abs, which refuses to resolve outside the
// root — a file tree that can be talked into writing ../../.ssh/authorized_keys
// is not a file tree.
package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// Kind tells the UI how to present a file: as text to edit, as an image to
// look at, or as something it should not try to open at all.
type Kind string

const (
	// KindTeX is a LaTeX source file.
	KindTeX Kind = "tex"
	// KindBib is a BibTeX database.
	KindBib Kind = "bib"
	// KindImage is a figure the preview can display.
	KindImage Kind = "image"
	// KindText is any other file safe to open in the editor.
	KindText Kind = "text"
	// KindBinary is anything we should not load into a text buffer.
	KindBinary Kind = "binary"
)

// Node is one entry in the file tree. Path is always relative to the project
// root and slash-separated, so it is stable across platforms and safe to hand
// back to the backend.
type Node struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	IsDir    bool   `json:"isDir"`
	Kind     Kind   `json:"kind"`
	Children []Node `json:"children"`
}

// Project is an open directory.
type Project struct {
	root           string
	rootFile       string // relative path of the document to compile
	beforeTextRead func(relative string)
}

// ErrOutsideProject means a path resolved outside the project root.
var ErrOutsideProject = errors.New("project: path escapes the project directory")

// ErrNoRootFile means no file in the project contains \documentclass.
var ErrNoRootFile = errors.New("project: no file with \\documentclass found")

// maxTreeEntries bounds tree walking. Someone will eventually open their home
// directory by accident; that should be useless, not fatal.
const maxTreeEntries = 20000

// skippedDirs are never walked. They are always noise in a LaTeX project and
// can be enormous.
var skippedDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
}

// Open reads dir as a project and picks the document to compile. A directory
// with no \documentclass anywhere still opens — the caller can set the root file
// later — so browsing a folder is never blocked by it.
func Open(dir string) (*Project, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("project: resolve %q: %w", dir, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("project: open: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("project: %q is not a directory", abs)
	}

	p := &Project{root: abs}
	if rootFile, err := DetectRootFile(abs); err == nil {
		p.rootFile = rootFile
	} else if !errors.Is(err, ErrNoRootFile) {
		return nil, err
	}
	return p, nil
}

// Root is the absolute path of the project directory.
func (p *Project) Root() string { return p.root }

// RootFile is the relative path of the document that gets compiled, or "" if
// none has been identified.
func (p *Project) RootFile() string { return p.rootFile }

// SetRootFile overrides the compile entry point. Detection is a guess; a
// document with several \documentclass files (a paper and its standalone
// figures, say) needs the writer to say which one is the real one.
func (p *Project) SetRootFile(rel string) error {
	abs, err := p.Abs(rel)
	if err != nil {
		return err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("project: root file: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("project: root file %q is a directory", rel)
	}
	p.rootFile = filepath.ToSlash(rel)
	return nil
}

// Abs turns a project-relative path into an absolute one, refusing anything that
// escapes the root. Every path from outside this package must pass through here.
func (p *Project) Abs(rel string) (string, error) {
	if rel == "" {
		return "", fmt.Errorf("project: empty path")
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("%w: %q is absolute", ErrOutsideProject, rel)
	}

	// Clean collapses "a/../b" and strips trailing slashes; the prefix check
	// below then catches anything that climbed out of the root.
	clean := filepath.Clean(filepath.FromSlash(rel))
	abs := filepath.Join(p.root, clean)

	// Compare with a separator appended so "/proj" cannot match "/project-other".
	if abs != p.root && !strings.HasPrefix(abs, p.root+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q", ErrOutsideProject, rel)
	}

	// A symlink pointing outside the project would slip past the textual check,
	// so resolve it when the target exists. Missing paths are fine — creating a
	// new file is a legitimate reason for the target not to exist yet.
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		root, rootErr := filepath.EvalSymlinks(p.root)
		if rootErr == nil && resolved != root &&
			!strings.HasPrefix(resolved, root+string(filepath.Separator)) {
			return "", fmt.Errorf("%w: %q resolves outside the project", ErrOutsideProject, rel)
		}
	}
	return abs, nil
}

// Rel is the inverse of Abs, for reporting engine output against the tree.
func (p *Project) Rel(abs string) (string, error) {
	rel, err := filepath.Rel(p.root, abs)
	if err != nil {
		return "", fmt.Errorf("project: relative path: %w", err)
	}
	return filepath.ToSlash(rel), nil
}

// Tree walks the project, directories first and alphabetical within each level.
func (p *Project) Tree() (Node, error) {
	count := 0
	children, err := p.walk(p.root, "", &count)
	if err != nil {
		return Node{}, err
	}
	return Node{
		Name:     filepath.Base(p.root),
		Path:     "",
		IsDir:    true,
		Kind:     KindText,
		Children: children,
	}, nil
}

func (p *Project) walk(dir, rel string, count *int) ([]Node, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("project: read %q: %w", dir, err)
	}

	var nodes []Node
	for _, entry := range entries {
		name := entry.Name()
		// Hidden files are almost always tooling, not writing. .gitignore and
		// friends are edited elsewhere.
		if strings.HasPrefix(name, ".") || skippedDirs[name] {
			continue
		}
		if *count >= maxTreeEntries {
			break
		}
		*count++

		childRel := name
		if rel != "" {
			childRel = rel + "/" + name
		}
		node := Node{Name: name, Path: childRel, IsDir: entry.IsDir()}

		if entry.IsDir() {
			node.Kind = KindText
			node.Children, err = p.walk(filepath.Join(dir, name), childRel, count)
			if err != nil {
				return nil, err
			}
		} else {
			node.Kind = KindOf(name)
		}
		nodes = append(nodes, node)
	}

	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].IsDir != nodes[j].IsDir {
			return nodes[i].IsDir
		}
		return strings.ToLower(nodes[i].Name) < strings.ToLower(nodes[j].Name)
	})
	return nodes, nil
}

var (
	texExts   = []string{".tex", ".sty", ".cls", ".ltx", ".def", ".clo"}
	imageExts = []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".pdf", ".eps"}
	textExts  = []string{".txt", ".md", ".csv", ".json", ".yml", ".yaml", ".toml", ".cfg", ".bst"}
)

// KindOf classifies a file by extension. Extensions are a guess, but a cheap
// and reliable enough one to decide whether to open a file in a text editor.
func KindOf(name string) Kind {
	ext := strings.ToLower(filepath.Ext(name))
	switch {
	case ext == ".bib":
		return KindBib
	case slices.Contains(texExts, ext):
		return KindTeX
	case slices.Contains(imageExts, ext):
		return KindImage
	case slices.Contains(textExts, ext):
		return KindText
	default:
		return KindBinary
	}
}

// documentClassRE matches \documentclass only when it is not commented out.
// A commented-out class line is common in files kept around as drafts.
var documentClassRE = regexp.MustCompile(`(?m)^[^%\n]*\\documentclass`)

// DetectRootFile finds the document to compile: the .tex file containing
// \documentclass. The file being edited is not the entry point — editing
// chapter3.tex must still compile the root — so this has to be answered for the
// project as a whole.
func DetectRootFile(root string) (string, error) {
	var candidates []string

	count := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// An unreadable subdirectory should not abort the search.
			return nil //nolint:nilerr // best-effort walk
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (strings.HasPrefix(name, ".") || skippedDirs[name]) {
				return filepath.SkipDir
			}
			return nil
		}
		if count >= maxTreeEntries {
			return filepath.SkipAll
		}
		count++

		if strings.ToLower(filepath.Ext(name)) != ".tex" {
			return nil
		}
		// Root documents are small; reading a bounded prefix keeps this cheap on
		// a project full of large generated .tex files.
		content, err := readPrefix(path, 64*1024)
		if err != nil || !documentClassRE.Match(content) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		candidates = append(candidates, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("project: detect root file: %w", err)
	}
	if len(candidates) == 0 {
		return "", ErrNoRootFile
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return rootFileRank(candidates[i]) < rootFileRank(candidates[j])
	})
	return candidates[0], nil
}

// rootFileRank orders candidates by how likely they are to be the real entry
// point: conventional names first, then files at the top level, then the rest
// alphabetically so the answer is at least stable.
func rootFileRank(rel string) string {
	base := strings.ToLower(filepath.Base(rel))
	depth := strings.Count(rel, "/")

	tier := 3
	switch base {
	case "main.tex":
		tier = 0
	case "root.tex", "master.tex", "thesis.tex", "paper.tex", "document.tex":
		tier = 1
	default:
		if depth == 0 {
			tier = 2
		}
	}
	return fmt.Sprintf("%d:%02d:%s", tier, min(depth, 99), strings.ToLower(rel))
}

func readPrefix(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	buf := make([]byte, limit)
	n, err := f.Read(buf)
	if n == 0 && err != nil {
		return nil, err
	}
	return buf[:n], nil
}
