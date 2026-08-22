package utils

import (
	"errors"
	"path/filepath"
	"strings"
)

// SecureJoin safely joins root and userPath ensuring the result remains within root.
// It returns an absolute cleaned path or error if traversal outside root is detected.
// root should be an absolute path. userPath may be relative; empty segments are ignored.
func SecureJoin(root, userPath string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", errors.New("root required")
	}
	cleanRoot := filepath.Clean(root)
	// If userPath empty just return root
	if strings.TrimSpace(userPath) == "" {
		return cleanRoot, nil
	}
	// Clean the user supplied path and strip any leading separators to avoid absolute takeover
	up := filepath.Clean(userPath)
	// If user provided an absolute path, make it relative for containment check
	if filepath.IsAbs(up) {
		// Turn it into a relative path by trimming leading separator
		up = strings.TrimPrefix(up, string(filepath.Separator))
	}
	candidate := filepath.Join(cleanRoot, up)
	rel, err := filepath.Rel(cleanRoot, candidate)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes root")
	}
	return candidate, nil
}
