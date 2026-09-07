package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/linell/aether/internal/policy"
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

func TestCreateDaemonStoresModel(t *testing.T) {
	s, st := newTestServer(t)
	storetest.Exec(t, st, `INSERT INTO hosts (id, name) VALUES ('h1', 'host-1')`)
	rec := call(s.handleCreateDaemon, http.MethodPost,
		`{"name":"foo","host":"host-1","class":"anchored","offline_policy":"queue","model":"anthropic:claude-sonnet-4-5"}`, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var doc daemonDoc
	decode(t, rec, &doc)
	if doc.Model != "anthropic:claude-sonnet-4-5" {
		t.Errorf("model = %q, want anthropic:claude-sonnet-4-5", doc.Model)
	}
	rec = call(s.handleCreateDaemon, http.MethodPost,
		`{"name":"bar","host":"host-1","class":"anchored","offline_policy":"queue","model":"gpt-5"}`, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad model status = %d, want 400", rec.Code)
	}
}

func TestPatchDaemonModelRoundTrip(t *testing.T) {
	s, st := newTestServer(t)
	seedThread(t, st, "anchored")
	path := map[string]string{"name": "daemon-1"}

	rec := call(s.handlePatchDaemon, http.MethodPatch, `{"model":"openai:gpt-5"}`, path)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var doc daemonDoc
	decode(t, call(s.handleGetDaemon, http.MethodGet, "", path), &doc)
	if doc.Model != "openai:gpt-5" {
		t.Errorf("model = %q, want openai:gpt-5", doc.Model)
	}
	if rec := call(s.handlePatchDaemon, http.MethodPatch, `{"model":"llama"}`, path); rec.Code != http.StatusBadRequest {
		t.Errorf("bad spec status = %d, want 400", rec.Code)
	}
	if rec := call(s.handlePatchDaemon, http.MethodPatch, `{"model":"scripted"}`, map[string]string{"name": "nope"}); rec.Code != http.StatusNotFound {
		t.Errorf("unknown daemon status = %d, want 404", rec.Code)
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

func TestSchedulePutWithoutThreadUsesDefaultThread(t *testing.T) {
	s, st := newTestServer(t)
	seedThread(t, st, "anchored")

	rec := call(s.handlePutSchedule, http.MethodPut,
		`{"cron":"0 7 * * *","tz":"UTC","policy":"queue"}`,
		map[string]string{"name": "daemon-1", "id": "morning"})
	var doc struct{ Thread string }
	decode(t, rec, &doc)
	if rec.Code != http.StatusOK || doc.Thread != "t1" {
		t.Fatalf("status = %d, thread = %q; want 200, t1", rec.Code, doc.Thread)
	}
}

func TestSchedulePutRejectsForeignThreadAndKeepsPrompt(t *testing.T) {
	s, st := newTestServer(t)
	seedThread(t, st, "anchored")
	storetest.Exec(t, st, `INSERT INTO daemons (id, name, host_id, class, offline_policy) VALUES ('d2', 'daemon-2', 'h1', 'anchored', 'queue')`)
	storetest.Exec(t, st, `INSERT INTO threads (id, daemon_id, host_id, directory) VALUES ('t2', 'd2', 'h1', '/tmp/t2')`)
	path := map[string]string{"name": "daemon-1", "id": "s1"}

	rec := call(s.handlePutSchedule, http.MethodPut, `{"thread":"t2","cron":"0 7 * * *","tz":"UTC","policy":"queue"}`, path)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	rec = call(s.handlePutSchedule, http.MethodPut, `{"cron":"0 7 * * *","tz":"UTC","policy":"queue","prompt":"Review the day"}`, path)
	var doc struct{ Prompt string }
	decode(t, rec, &doc)
	if rec.Code != http.StatusOK || doc.Prompt != "Review the day" {
		t.Fatalf("status = %d, prompt = %q; want 200, Review the day", rec.Code, doc.Prompt)
	}
}

func TestAnswerApprovalOnce(t *testing.T) {
	s, st := newTestServer(t)
	seedThread(t, st, "anchored")
	storetest.Exec(t, st, `INSERT INTO approvals (id, thread_id, call_id, tool, args, context) VALUES ('a1', 't1', 'c1', 'shell', '{}', '{}')`)
	path := map[string]string{"id": "a1"}

	var first, second struct{ Status string }
	decode(t, call(s.handleAnswerApproval, http.MethodPost, `{"decision":"approved"}`, path), &first)
	decode(t, call(s.handleAnswerApproval, http.MethodPost, `{"decision":"denied"}`, path), &second)
	if first.Status != "approved" || second.Status != "approved" {
		t.Errorf("statuses = %q, %q; want approved twice", first.Status, second.Status)
	}
	if n := storetest.Count(t, st, `SELECT COUNT(1) FROM outbox WHERE event_name = 'aether/approval.answered'`); n != 1 {
		t.Errorf("outbox count = %d, want 1", n)
	}
	if rec := call(s.handleAnswerApproval, http.MethodPost, `{"decision":"maybe"}`, path); rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if rec := call(s.handleGetApproval, http.MethodGet, "", map[string]string{"id": "nope"}); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestMatchCallChecksThreadAndRules(t *testing.T) {
	s, st := newTestServer(t)
	seedThread(t, st, "anchored")
	s.rules = []policy.Rule{{Tool: "shell", Argv: []string{"ls", "..."}}}
	path := map[string]string{"name": "daemon-1"}
	match := func(body string) matchDoc {
		var doc matchDoc
		decode(t, call(s.handleMatchCall, http.MethodPost, body, path), &doc)
		return doc
	}
	if doc := match(`{"thread":"t1","call":{"id":"c1","tool":"shell","args":{"argv":["ls"]},"context":{"cwd":"/tmp","host":"host-1"}}}`); !doc.Allowed {
		t.Errorf("ls: %+v, want allowed", doc)
	}
	if doc := match(`{"thread":"t1","call":{"id":"c1","tool":"shell","args":{"argv":["rm"]},"context":{"cwd":"/tmp","host":"host-1"}}}`); doc.Allowed {
		t.Errorf("rm: %+v, want denied", doc)
	}
	if doc := match(`{"thread":"t1","call":{"id":"c1","tool":"shell","args":{"argv":["ls"]},"context":{"cwd":"/elsewhere","host":"host-1"}}}`); doc.Allowed {
		t.Errorf("foreign cwd: %+v, want denied", doc)
	}
}

func TestClaimOperationOnce(t *testing.T) {
	s, _ := newTestServer(t)
	var first, second struct{ Claimed bool }
	decode(t, call(s.handleClaimOperation, http.MethodPost, `{"id":"op1","kind":"tool.call"}`, nil), &first)
	decode(t, call(s.handleClaimOperation, http.MethodPost, `{"id":"op1","kind":"tool.call"}`, nil), &second)
	if !first.Claimed || second.Claimed {
		t.Errorf("claimed = %v, %v; want true, false", first.Claimed, second.Claimed)
	}
}

func TestApprovalsRequestDedupesEvent(t *testing.T) {
	s, st := newTestServer(t)
	seedThread(t, st, "anchored")
	body := `{"calls":[{"id":"c1","tool":"shell","args":{"argv":["rm"]},"context":{"cwd":"/tmp","host":"host-1"}}],"state":"s1"}`
	var first, second struct{ Approval string }
	decode(t, call(s.handleThreadApprovals, http.MethodPost, body, map[string]string{"id": "t1"}), &first)
	decode(t, call(s.handleThreadApprovals, http.MethodPost, strings.Replace(body, "s1", "s2", 1), map[string]string{"id": "t1"}), &second)
	if first.Approval != second.Approval {
		t.Errorf("approval ids = %q, %q; want equal", first.Approval, second.Approval)
	}
	if n := storetest.Count(t, st, `SELECT COUNT(1) FROM outbox WHERE event_name = 'daemon/approval.requested'`); n != 1 {
		t.Errorf("outbox count = %d, want 1", n)
	}
	var doc approvalDoc
	decode(t, call(s.handleGetApproval, http.MethodGet, "", map[string]string{"id": first.Approval}), &doc)
	if doc.State != "s2" {
		t.Errorf("state = %q, want s2", doc.State)
	}
}

func TestListMessagesNewestFirstBeforeCursorAndLimit(t *testing.T) {
	s, st := newTestServer(t)
	seedThread(t, st, "anchored")
	for i, m := range []string{"m1:user:a", "m2:assistant:b", "m3:user:c", "m4:assistant:d"} {
		p := strings.Split(m, ":")
		storetest.Exec(t, st, `INSERT INTO messages (id, thread_id, role, text, created_at) VALUES (?, 't1', ?, ?, ?)`,
			p[0], p[1], p[2], fmt.Sprintf("2026-09-06T07:00:0%d.000Z", i))
	}
	req := httptest.NewRequest(http.MethodGet, "/?before=m4&limit=2", nil)
	req.SetPathValue("id", "t1")
	rec := httptest.NewRecorder()
	s.handleListMessages(rec, req)
	var resp struct {
		Messages []messageDoc `json:"messages"`
	}
	decode(t, rec, &resp)
	got := []string{}
	for _, m := range resp.Messages {
		got = append(got, m.ID+":"+m.Role)
	}
	if want := []string{"m3:user", "m2:assistant"}; !reflect.DeepEqual(got, want) {
		t.Errorf("messages = %v, want %v", got, want)
	}
	req = httptest.NewRequest(http.MethodGet, "/?before=missing", nil)
	req.SetPathValue("id", "t1")
	rec = httptest.NewRecorder()
	s.handleListMessages(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown cursor status = %d, want 404", rec.Code)
	}
}
