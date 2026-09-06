package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/linell/aether/internal/store"
)

func newTestServer(t *testing.T) (*server, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(context.Background(), filepath.Join(dir, "aether.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return &server{store: st}, st
}

func seedHost(t *testing.T, db *sql.DB, id, name string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO hosts (id, name) VALUES (?, ?)`, id, name); err != nil {
		t.Fatalf("seed host: %v", err)
	}
}

func seedDaemon(t *testing.T, db *sql.DB, id, name, hostID, class, policy string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO daemons (id, name, host_id, class, offline_policy) VALUES (?, ?, ?, ?, ?)`,
		id, name, hostID, class, policy,
	); err != nil {
		t.Fatalf("seed daemon: %v", err)
	}
}

func seedThread(t *testing.T, db *sql.DB, id, daemonID, hostID string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO threads (id, daemon_id, host_id, directory) VALUES (?, ?, ?, ?)`,
		id, daemonID, hostID, "/tmp",
	); err != nil {
		t.Fatalf("seed thread: %v", err)
	}
}

func TestReplyPersistsMessageAndOutboxRow(t *testing.T) {
	s, st := newTestServer(t)
	seedHost(t, st.DB(), "h1", "host-1")
	seedDaemon(t, st.DB(), "d1", "daemon-1", "h1", "anchored", "queue")
	seedThread(t, st.DB(), "t1", "d1", "h1")

	req := httptest.NewRequest(http.MethodPost, "/threads/t1/reply", strings.NewReader(`{"text":"hello"}`))
	req.SetPathValue("id", "t1")
	rec := httptest.NewRecorder()
	s.handleThreadReply(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Reply string `json:"reply"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Reply == "" {
		t.Fatal("empty reply id")
	}

	var msgCount int
	if err := st.DB().QueryRow(
		`SELECT COUNT(1) FROM messages WHERE id = ? AND role = 'assistant'`, resp.Reply,
	).Scan(&msgCount); err != nil {
		t.Fatalf("query messages: %v", err)
	}
	if msgCount != 1 {
		t.Errorf("messages count = %d, want 1", msgCount)
	}

	var outboxCount int
	if err := st.DB().QueryRow(
		`SELECT COUNT(1) FROM outbox WHERE event_name = ?`, "daemon/message.replied",
	).Scan(&outboxCount); err != nil {
		t.Fatalf("query outbox: %v", err)
	}
	if outboxCount != 1 {
		t.Errorf("outbox count = %d, want 1", outboxCount)
	}
}

func TestMemoryPutStaleVersionReturns409(t *testing.T) {
	s, st := newTestServer(t)
	seedHost(t, st.DB(), "h1", "host-1")
	seedDaemon(t, st.DB(), "d1", "daemon-1", "h1", "anchored", "queue")
	if _, err := st.DB().Exec(
		`INSERT INTO memory (id, daemon_id, version, body) VALUES (?, ?, ?, ?)`,
		"m1", "d1", 1, "hello",
	); err != nil {
		t.Fatalf("seed memory: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/daemons/daemon-1/memory",
		strings.NewReader(`{"content":"new","expected_version":0}`))
	req.SetPathValue("name", "daemon-1")
	rec := httptest.NewRecorder()
	s.handlePutMemory(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func TestSchedulePutOnOpportunisticDaemonReturns422(t *testing.T) {
	s, st := newTestServer(t)
	seedHost(t, st.DB(), "h1", "host-1")
	seedDaemon(t, st.DB(), "d1", "daemon-1", "h1", "opportunistic", "queue")
	seedThread(t, st.DB(), "t1", "d1", "h1")

	req := httptest.NewRequest(http.MethodPut, "/daemons/daemon-1/schedules/s1",
		strings.NewReader(`{"thread":"t1","cron":"0 * * * *","tz":"UTC","policy":"queue"}`))
	req.SetPathValue("name", "daemon-1")
	req.SetPathValue("id", "s1")
	rec := httptest.NewRecorder()
	s.handlePutSchedule(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestHostsPostIsIdempotent(t *testing.T) {
	s, _ := newTestServer(t)

	post := func() string {
		req := httptest.NewRequest(http.MethodPost, "/hosts", strings.NewReader(`{"name":"host-1"}`))
		rec := httptest.NewRecorder()
		s.handleCreateHost(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var resp struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return resp.ID
	}

	id1 := post()
	id2 := post()
	if id1 != id2 {
		t.Errorf("ids differ: %q vs %q", id1, id2)
	}
}
