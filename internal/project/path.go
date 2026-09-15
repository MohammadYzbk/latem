package project

import (
	"errors"
	"path"
	"strings"
)

// ErrInvalidRelativePath means a value is not a canonical project-relative
// POSIX path. Transport adapters may wrap this error without exposing a host
// filesystem path.
var ErrInvalidRelativePath = errors.New("project: invalid relative path")

// RelativePath is a validated, canonical project-relative POSIX path. It is a
// semantic value: it never contains a host-specific separator or absolute
// filesystem location.
type RelativePath string

// ParseRelativePath admits only canonical project-relative POSIX paths.
func ParseRelativePath(value string) (RelativePath, error) {
	if value == "" || strings.ContainsRune(value, 0) || strings.Contains(value, `\`) ||
		strings.HasPrefix(value, "/") {
		return "", ErrInvalidRelativePath
	}
	clean := path.Clean(value)
	if clean != value || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", ErrInvalidRelativePath
	}
	return RelativePath(clean), nil
}

func (path RelativePath) String() string {
	return string(path)
}
