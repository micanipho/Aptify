package domain

import (
	"strconv"
	"strings"
)

// UnsafePathError reports a generated file path that could escape the project root.
type UnsafePathError struct {
	Path   string
	Reason string
}

func (e *UnsafePathError) Error() string {
	return "unsafe path " + strconv.Quote(e.Path) + ": " + e.Reason
}

// CheckPath rejects a generated path that is empty, absolute, uses backslashes, contains a
// ".." segment or a NUL byte. Generated paths are untrusted: check on write and again when
// archiving.
func CheckPath(p string) error {
	switch {
	case p == "":
		return &UnsafePathError{Path: p, Reason: "empty"}
	case strings.HasPrefix(p, "/"):
		return &UnsafePathError{Path: p, Reason: "absolute"}
	case strings.Contains(p, `\`):
		return &UnsafePathError{Path: p, Reason: "contains a backslash"}
	case strings.ContainsRune(p, 0):
		return &UnsafePathError{Path: p, Reason: "contains a NUL byte"}
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return &UnsafePathError{Path: p, Reason: `contains a ".." segment`}
		}
	}
	return nil
}
