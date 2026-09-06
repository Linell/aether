package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/linell/aether/internal/store"
	"github.com/linell/aether/internal/store/storetest"
)

func newTestServer(t *testing.T) (*server, *store.Store) {
	t.Helper()
	st := storetest.Open(t)
	return &server{store: st}, st
}

func seedThread(t *testing.T, st *store.Store, class string) {
	t.Helper()
	storetest.Exec(t, st, `INSERT INTO hosts (id, name) VALUES ('h1', 'host-1')`)
	storetest.Exec(t, st, `INSERT INTO daemons (id, name, host_id, class, offline_policy) VALUES ('d1', 'daemon-1', 'h1', ?, 'queue')`, class)
	storetest.Exec(t, st, `INSERT INTO threads (id, daemon_id, host_id, directory) VALUES ('t1', 'd1', 'h1', '/tmp')`)
}

func call(handler http.HandlerFunc, method, body string, path map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/", strings.NewReader(body))
	for k, v := range path {
		req.SetPathValue(k, v)
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("unmarshal %s: %v", rec.Body.String(), err)
	}
}

func TestReplyPersistsMessageAndOutboxRow(t *testing.T) {
	s, st := newTestServer(t)
	seedThread(t, st, "anchored")

	rec := call(s.handleThreadReply, http.MethodPost, `{"text":"hello"}`, map[string]string{"id": "t1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Reply string `json:"reply"`
	}
	decode(t, rec, &resp)
	if n := storetest.Count(t, st, `SELECT COUNT(1) FROM messages WHERE id = ? AND role = 'assistant'`, resp.Reply); n != 1 {
		t.Errorf("messages count = %d, want 1", n)
	}
	if n := storetest.Count(t, st, `SELECT COUNT(1) FROM outbox WHERE event_name = 'daemon/message.replied'`); n != 1 {
		t.Errorf("outbox count = %d, want 1", n)
	}
}

func TestMemoryPutStaleVersionReturns409(t *testing.T) {
	s, st := newTestServer(t)
	seedThread(t, st, "anchored")
	storetest.Exec(t, st, `INSERT INTO memory (id, daemon_id, version, body) VALUES ('m1', 'd1', 1, 'hello')`)

	rec := call(s.handlePutMemory, http.MethodPut, `{"content":"new","expected_version":0}`, map[string]string{"name": "daemon-1"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func TestSchedulePutOnOpportunisticDaemonReturns422(t *testing.T) {
	s, st := newTestServer(t)
	seedThread(t, st, "opportunistic")

	rec := call(s.handlePutSchedule, http.MethodPut,
		`{"thread":"t1","cron":"0 * * * *","tz":"UTC","policy":"queue"}`,
		map[string]string{"name": "daemon-1", "id": "s1"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestHostsPostIsIdempotent(t *testing.T) {
	s, _ := newTestServer(t)

	post := func() string {
		rec := call(s.handleCreateHost, http.MethodPost, `{"name":"host-1"}`, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var resp struct {
			ID string `json:"id"`
		}
		decode(t, rec, &resp)
		return resp.ID
	}
	if id1, id2 := post(), post(); id1 != id2 {
		t.Errorf("ids differ: %q vs %q", id1, id2)
	}
}

func TestCreateDaemonEnqueuesConjureOnce(t *testing.T) {
	s, st := newTestServer(t)
	storetest.Exec(t, st, `INSERT INTO hosts (id, name) VALUES ('h1', 'host-1')`)
	body := `{"name":"foo","host":"host-1","class":"anchored","offline_policy":"queue"}`

	first := call(s.handleCreateDaemon, http.MethodPost, body, nil)
	second := call(s.handleCreateDaemon, http.MethodPost, body, nil)
	if first.Code != http.StatusCreated || second.Code != http.StatusOK {
		t.Fatalf("status = %d, %d; want 201, 200", first.Code, second.Code)
	}
	if n := storetest.Count(t, st, `SELECT COUNT(1) FROM outbox WHERE event_name = 'daemon/conjure.requested'`); n != 1 {
		t.Errorf("outbox count = %d, want 1", n)
	}
	rec := call(s.handleCreateDaemon, http.MethodPost, `{"name":"foo","host":"other","class":"anchored","offline_policy":"queue"}`, nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown host status = %d, want 404", rec.Code)
	}
}

func TestCreateMessageReusesThreadAndDedupes(t *testing.T) {
	s, st := newTestServer(t)
	storetest.Exec(t, st, `INSERT INTO hosts (id, name, root) VALUES ('h1', 'host-1', '/srv/daemons')`)
	storetest.Exec(t, st, `INSERT INTO daemons (id, name, host_id, class, offline_policy) VALUES ('d1', 'foo', 'h1', 'anchored', 'queue')`)
	path := map[string]string{"name": "foo"}

	var first, second struct{ Thread, Message string }
	decode(t, call(s.handleCreateMessage, http.MethodPost, `{"id":"m1","text":"hi"}`, path), &first)
	decode(t, call(s.handleCreateMessage, http.MethodPost, `{"id":"m1","text":"hi"}`, path), &second)
	decode(t, call(s.handleCreateMessage, http.MethodPost, `{"text":"again"}`, path), &second)
	if first.Thread != second.Thread {
		t.Errorf("threads differ: %q vs %q", first.Thread, second.Thread)
	}
	if n := storetest.Count(t, st, `SELECT COUNT(1) FROM outbox WHERE event_name = 'aether/message.sent'`); n != 2 {
		t.Errorf("outbox count = %d, want 2", n)
	}
	if n := storetest.Count(t, st, `SELECT COUNT(1) FROM threads WHERE directory = '/srv/daemons/foo'`); n != 1 {
		t.Errorf("thread directory rows = %d, want 1", n)
	}
}

func TestReplyWithClientIDDedupes(t *testing.T) {
	s, st := newTestServer(t)
	seedThread(t, st, "anchored")
	path := map[string]string{"id": "t1"}

	call(s.handleThreadReply, http.MethodPost, `{"id":"run-1:reply","text":"hello"}`, path)
	call(s.handleThreadReply, http.MethodPost, `{"id":"run-1:reply","text":"hello"}`, path)
	if n := storetest.Count(t, st, `SELECT COUNT(1) FROM outbox WHERE event_name = 'daemon/message.replied'`); n != 1 {
		t.Errorf("outbox count = %d, want 1", n)
	}
}

func TestCreateMarkerRejectsUnknownKind(t *testing.T) {
	s, st := newTestServer(t)
	seedThread(t, st, "anchored")
	path := map[string]string{"name": "daemon-1"}

	if rec := call(s.handleCreateMarker, http.MethodPost, `{"kind":"made.up","ref":"x"}`, path); rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if rec := call(s.handleCreateMarker, http.MethodPost, `{"kind":"schedule.stale","thread":"t1","ref":"s1","detail":"{}"}`, path); rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if n := storetest.Count(t, st, `SELECT COUNT(1) FROM markers WHERE kind = 'schedule.stale'`); n != 1 {
		t.Errorf("markers = %d, want 1", n)
	}
}
