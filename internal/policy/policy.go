package policy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

var DefaultEnvAllow = []string{"PATH", "HOME", "LANG", "TERM", "TZ"}

func ScrubEnv(env []string, allow []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, ok := strings.Cut(kv, "=")
		if ok && slices.Contains(allow, name) {
			out = append(out, kv)
		}
	}
	return out
}

type ErrOutsideRoot struct {
	Root string
	Path string
}

func (e *ErrOutsideRoot) Error() string {
	return fmt.Sprintf("policy: path %q is outside root %q", e.Path, e.Root)
}

func ResolveWithin(root, p string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("policy: resolve root: %w", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", fmt.Errorf("policy: resolve root: %w", err)
	}
	full, err := resolvePath(absoluteUnder(absRoot, p))
	if err != nil {
		return "", fmt.Errorf("policy: resolve %q: %w", p, err)
	}
	if !contains(resolvedRoot, full) {
		return "", &ErrOutsideRoot{Root: resolvedRoot, Path: full}
	}
	return full, nil
}

func absoluteUnder(root, p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(root, p)
}

func contains(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func resolvePath(p string) (string, error) {
	ancestor, remainder, err := deepestExisting(p)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolved, remainder), nil
}

func deepestExisting(p string) (string, string, error) {
	var rem []string
	for cur := p; ; cur = filepath.Dir(cur) {
		_, err := os.Lstat(cur)
		if err == nil || filepath.Dir(cur) == cur {
			return cur, filepath.Join(rem...), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", "", err
		}
		rem = append([]string{filepath.Base(cur)}, rem...)
	}
}
