package host

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/linell/aether/internal/rest"
)

type fakeRegistry struct {
	daemons []Daemon
}

func (f *fakeRegistry) Daemons(ctx context.Context) ([]Daemon, error) {
	return f.daemons, nil
}

func writeManifest(t *testing.T, dir string, m Manifest) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "aether.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReconcileSpawnsExactlyOnce(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, filepath.Join(root, "foo"), Manifest{Name: "foo", Run: "sleep 30"})

	sup := New(root, &fakeRegistry{daemons: []Daemon{{Name: "foo"}}})
	ctx := context.Background()

	if err := sup.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	first := sup.running["foo"]
	if first == nil {
		t.Fatal("expected foo to be running after first Reconcile")
	}

	if err := sup.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	second := sup.running["foo"]
	if second != first {
		t.Error("second Reconcile spawned a new process instead of reusing the running one")
	}

	sup.Stop()
}

func TestClientDaemons(t *testing.T) {
	const token = "secret-token"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/hosts/host-1/daemons" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			t.Errorf("Authorization header = %q, want %q", got, "Bearer "+token)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"name":"foo"},{"name":"bar"}]`))
	}))
	defer srv.Close()

	c := &Client{Client: rest.Client{BaseURL: srv.URL, Token: token}, Host: "host-1"}
	got, err := c.Daemons(context.Background())
	if err != nil {
		t.Fatalf("Daemons: %v", err)
	}

	want := []Daemon{{Name: "foo"}, {Name: "bar"}}
	if len(got) != len(want) {
		t.Fatalf("Daemons = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Daemons[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}
