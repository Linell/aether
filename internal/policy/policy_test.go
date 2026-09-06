package policy

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestScrubEnvDropsUnlistedVars(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"HOME=/home/x",
		"AWS_SECRET_ACCESS_KEY=shh",
		"MALFORMED",
	}
	got := ScrubEnv(env, []string{"PATH", "HOME"})

	want := map[string]bool{"PATH=/usr/bin": true, "HOME=/home/x": true}
	if len(got) != len(want) {
		t.Fatalf("ScrubEnv = %v, want exactly %v", got, want)
	}
	for _, kv := range got {
		if !want[kv] {
			t.Errorf("unexpected entry %q in scrubbed env", kv)
		}
	}
}

func TestResolveWithinAllowsNonexistentChild(t *testing.T) {
	root := t.TempDir()
	got, err := ResolveWithin(root, "new/child.txt")
	if err != nil {
		t.Fatalf("ResolveWithin: %v", err)
	}
	wantSuffix := filepath.Join("new", "child.txt")
	if filepath.Base(filepath.Dir(got)) != "new" || filepath.Base(got) != "child.txt" {
		t.Errorf("ResolveWithin = %q, want to end with %q", got, wantSuffix)
	}
}

func TestResolveWithinRejectsParentEscape(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "x"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ResolveWithin(root, "../x")
	var outside *ErrOutsideRoot
	if !errors.As(err, &outside) {
		t.Fatalf("ResolveWithin(../x) error = %v, want *ErrOutsideRoot", err)
	}
}

func TestResolveWithinRejectsSiblingPrefixTrap(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	sibling := filepath.Join(parent, "root-sibling")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(sibling, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := ResolveWithin(root, sibling)
	var outside *ErrOutsideRoot
	if !errors.As(err, &outside) {
		t.Fatalf("ResolveWithin(sibling) error = %v, want *ErrOutsideRoot", err)
	}
}

func TestResolveWithinRejectsSymlinkEscape(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	outsideDir := filepath.Join(parent, "outside")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outsideFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(root, "escape")
	if err := os.Symlink(outsideFile, link); err != nil {
		t.Fatal(err)
	}

	_, err := ResolveWithin(root, "escape")
	var outside *ErrOutsideRoot
	if !errors.As(err, &outside) {
		t.Fatalf("ResolveWithin(escape symlink) error = %v, want *ErrOutsideRoot", err)
	}
}

func TestResolveWithinAllowsRootItself(t *testing.T) {
	root := t.TempDir()
	got, err := ResolveWithin(root, ".")
	if err != nil {
		t.Fatalf("ResolveWithin(root): %v", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != resolvedRoot {
		t.Errorf("ResolveWithin(root) = %q, want %q", got, resolvedRoot)
	}
}

func TestMatchShellRules(t *testing.T) {
	root := t.TempDir()
	rules := []Rule{{Daemon: "foo", Tool: "shell", Argv: []string{"ls", "..."}}, {Tool: "shell", Argv: []string{"git", "*"}}}
	cases := []struct {
		name, daemon, args string
		want               bool
	}{
		{"trailing args", "foo", `{"argv":["ls","-la","sub"]}`, true},
		{"other daemon", "bar", `{"argv":["ls"]}`, false},
		{"wildcard arg", "bar", `{"argv":["git","status"]}`, true},
		{"too many args", "bar", `{"argv":["git","push","--force"]}`, false},
		{"path escapes root", "foo", `{"argv":["ls","../.."]}`, false},
		{"flag path escapes root", "foo", `{"argv":["ls","--dir=/etc"]}`, false},
		{"cwd escapes root", "foo", `{"argv":["ls"],"cwd":".."}`, false},
		{"unknown tool", "foo", `{}`, false},
	}
	for _, tc := range cases {
		tool := "shell"
		if tc.name == "unknown tool" {
			tool = "http"
		}
		_, got := Match(rules, Request{Daemon: tc.daemon, Tool: tool, Args: []byte(tc.args), Cwd: root})
		if got != tc.want {
			t.Errorf("%s: match = %v, want %v", tc.name, got, tc.want)
		}
	}
}
