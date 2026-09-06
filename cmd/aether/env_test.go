package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnvFillsOnlyUnset(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := "# comment\n\nexport A=1\nB=\"two words\"\nC='x' # trailing\nD=raw # note\nE=\nbad line\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"A", "B", "C", "D", "E"} {
		t.Setenv(k, "")
	}
	t.Setenv("B", "kept")
	if err := loadDotEnv(path); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"A": "1", "B": "kept", "C": "x", "D": "raw", "E": ""}
	for k, v := range want {
		if got := os.Getenv(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

func TestLoadDotEnvMissingFileIsFine(t *testing.T) {
	if err := loadDotEnv(filepath.Join(t.TempDir(), ".env")); err != nil {
		t.Fatal(err)
	}
}
