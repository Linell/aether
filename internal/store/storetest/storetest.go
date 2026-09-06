package storetest

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/linell/aether/internal/store"
)

func Open(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "aether.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func Exec(t *testing.T, st *store.Store, query string, args ...any) {
	t.Helper()
	if _, err := st.DB().Exec(query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

func Count(t *testing.T, st *store.Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := st.DB().QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}
