package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/linell/aether/internal/contract"
	"github.com/linell/aether/internal/store/storetest"
)

type fakeAPI struct {
	srv   *httptest.Server
	sent  []map[string]any
	paths []string
	fail  bool
}

func newFakeAPI(t *testing.T) *fakeAPI {
	f := &fakeAPI{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(body, &m)
		f.sent = append(f.sent, m)
		f.paths = append(f.paths, r.URL.Path)
		if f.fail {
			_, _ = w.Write([]byte(`{"ok":false,"description":"boom"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":7}}`))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func TestSendMessageHitsBotPathAndDecodes(t *testing.T) {
	api := newFakeAPI(t)
	c := &Client{BaseURL: api.srv.URL, Token: "tok"}
	sent, err := c.SendMessage(context.Background(), Message{ChatID: 42, Text: "hi"}, ApprovalKeyboard("a1"))
	if err != nil || sent.MessageID != 7 {
		t.Fatalf("SendMessage = %+v, %v", sent, err)
	}
	if api.paths[0] != "/bottok/sendMessage" || api.sent[0]["chat_id"] != float64(42) {
		t.Errorf("request = %s %v", api.paths[0], api.sent[0])
	}
	api.fail = true
	if err := c.AnswerCallback(context.Background(), "q", "ok"); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("AnswerCallback err = %v, want boom", err)
	}
}

func TestSendApprovalOncePerOutboxRow(t *testing.T) {
	api := newFakeAPI(t)
	st := storetest.Open(t)
	storetest.Exec(t, st, `INSERT INTO hosts (id, name) VALUES ('h1', 'host-1')`)
	storetest.Exec(t, st, `INSERT INTO daemons (id, name, host_id, class, offline_policy) VALUES ('d1', 'foo', 'h1', 'anchored', 'queue')`)
	storetest.Exec(t, st, `INSERT INTO threads (id, daemon_id, host_id, directory) VALUES ('t1', 'd1', 'h1', '/srv/foo')`)
	storetest.Exec(t, st, `INSERT INTO channels (id, kind, external_id, thread_id) VALUES ('c1', 'telegram', '42', 't1')`)
	storetest.Exec(t, st, `INSERT INTO approvals (id, thread_id, call_id, tool, args, context) VALUES ('a1', 't1', 'call-1', 'shell', '{"argv":["rm","x"]}', '{"cwd":"/srv/foo"}')`)
	out := &Outbound{Store: st, Client: &Client{BaseURL: api.srv.URL, Token: "tok"}}
	payload := contract.ApprovalRequestedPayload{Daemon: "foo", Thread: "t1", Calls: []contract.Call{
		{ID: "call-1", Tool: "shell", Args: json.RawMessage(`{"argv":["rm","x"]}`), Context: json.RawMessage(`{"cwd":"/srv/foo"}`)},
	}}

	for range 2 {
		if _, err := out.SendApproval(context.Background(), "evt-1", payload); err != nil {
			t.Fatalf("SendApproval: %v", err)
		}
	}
	if len(api.sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(api.sent))
	}
	text := api.sent[0]["text"].(string)
	if !strings.Contains(text, "shell rm x") || !strings.Contains(text, "approval a1") || api.sent[0]["reply_markup"] == nil {
		t.Errorf("rendered = %q markup = %v", text, api.sent[0]["reply_markup"])
	}
	if n, err := out.SendReply(context.Background(), "evt-2", contract.MessageRepliedPayload{Daemon: "foo", Thread: "t1", Text: "done"}); n != 1 || err != nil {
		t.Errorf("SendReply = %d, %v", n, err)
	}
	if n, _ := out.SendReply(context.Background(), "evt-3", contract.MessageRepliedPayload{Daemon: "foo", Thread: "unbound", Text: "x"}); n != 0 {
		t.Errorf("unbound thread sent to %d chats", n)
	}
}

func TestFailedSendReleasesClaim(t *testing.T) {
	api := newFakeAPI(t)
	api.fail = true
	st := storetest.Open(t)
	storetest.Exec(t, st, `INSERT INTO hosts (id, name) VALUES ('h1', 'host-1')`)
	storetest.Exec(t, st, `INSERT INTO daemons (id, name, host_id, class, offline_policy) VALUES ('d1', 'foo', 'h1', 'anchored', 'queue')`)
	storetest.Exec(t, st, `INSERT INTO threads (id, daemon_id, host_id, directory) VALUES ('t1', 'd1', 'h1', '/srv/foo')`)
	storetest.Exec(t, st, `INSERT INTO channels (id, kind, external_id, thread_id) VALUES ('c1', 'telegram', '42', 't1')`)
	out := &Outbound{Store: st, Client: &Client{BaseURL: api.srv.URL, Token: "tok"}}
	reply := contract.MessageRepliedPayload{Daemon: "foo", Thread: "t1", Text: "x"}

	if _, err := out.SendReply(context.Background(), "evt-1", reply); err == nil {
		t.Fatal("want error from failed send")
	}
	api.fail = false
	if _, err := out.SendReply(context.Background(), "evt-1", reply); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if len(api.sent) != 2 {
		t.Errorf("sent %d, want 2 attempts", len(api.sent))
	}
}
