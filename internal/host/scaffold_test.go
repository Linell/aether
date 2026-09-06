package host

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScaffoldIsIdempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "foo")
	var ran []string
	run := func(_ context.Context, _ string, args ...string) error {
		ran = append(ran, strings.Join(args, " "))
		if args[0] == "git" {
			return os.Mkdir(filepath.Join(dir, ".git"), 0o755)
		}
		return nil
	}
	for range 2 {
		if err := Scaffold(context.Background(), dir, "foo", "file:../sdk", run); err != nil {
			t.Fatalf("Scaffold: %v", err)
		}
	}
	if strings.Join(ran, ",") != "bun install,git init -q,bun install" {
		t.Errorf("commands = %v", ran)
	}
	index, _ := os.ReadFile(filepath.Join(dir, "index.ts"))
	if lines := strings.Count(string(index), "\n"); lines != 3 {
		t.Errorf("index.ts has %d lines, want 3", lines)
	}
	if m, err := ReadManifest(dir); err != nil || m.Name != "foo" {
		t.Errorf("manifest = %+v, %v", m, err)
	}
}
