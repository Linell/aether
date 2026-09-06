package inngest

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewFailsClosedWithoutCredentials(t *testing.T) {
	if _, err := New(Options{AppID: "x", EventKey: "k"}); !errors.Is(err, ErrMissingCredentials) {
		t.Fatalf("err = %v, want ErrMissingCredentials", err)
	}
}

func TestPublishSendsOutboxIDAsEventID(t *testing.T) {
	var got []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ids":["evt"],"status":200}`))
	}))
	defer srv.Close()

	c, err := New(Options{AppID: "t", EventKey: "ek", SigningKey: "sk", EventAPIURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Publish(context.Background(), "row-1", "test/event.happened", json.RawMessage(`{"daemon":"d"}`)); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(got) != 1 || got[0]["id"] != "row-1" || got[0]["name"] != "test/event.happened" {
		t.Fatalf("sent = %v, want one event with id row-1", got)
	}
}
