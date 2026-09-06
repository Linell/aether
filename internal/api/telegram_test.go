package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/linell/aether/internal/store/storetest"
	"github.com/linell/aether/internal/telegram"
)

func telegramServer(t *testing.T) (*server, func(body string) *httptest.ResponseRecorder) {
	t.Helper()
	s, st := newTestServer(t)
	seedThread(t, st, "anchored")
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	t.Cleanup(api.Close)
	s.telegram = Telegram{Client: &telegram.Client{BaseURL: api.URL, Token: "tok"}, WebhookSecret: "hook"}
	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.Header.Set(telegram.SecretHeader, "hook")
		rec := httptest.NewRecorder()
		s.handleTelegramWebhook(rec, req)
		return rec
	}
	return s, post
}

func TestWebhookRequiresSecret(t *testing.T) {
	s, _ := telegramServer(t)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"update_id":1}`))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	s.handleTelegramWebhook(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	s.telegram = Telegram{}
	if rec := call(s.handleTelegramWebhook, http.MethodPost, `{}`, nil); rec.Code != http.StatusNotFound {
		t.Errorf("disabled status = %d, want 404", rec.Code)
	}
}

func TestWebhookTextBindsChatAndDedupes(t *testing.T) {
	s, post := telegramServer(t)
	text := `{"update_id":10,"message":{"message_id":1,"chat":{"id":42},"text":"hello"}}`
	if rec := post(text); rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	post(text)
	post(`{"update_id":11,"message":{"message_id":2,"chat":{"id":42},"text":"again"}}`)
	st := s.store
	if n := storetest.Count(t, st, `SELECT COUNT(1) FROM channels WHERE kind = 'telegram' AND external_id = '42' AND thread_id = 't1'`); n != 1 {
		t.Errorf("channels = %d, want 1", n)
	}
	if n := storetest.Count(t, st, `SELECT COUNT(1) FROM outbox WHERE event_name = 'aether/message.sent'`); n != 2 {
		t.Errorf("message.sent events = %d, want 2", n)
	}
}

func TestWebhookCallbackAnswersApproval(t *testing.T) {
	s, post := telegramServer(t)
	storetest.Exec(t, s.store, `INSERT INTO approvals (id, thread_id, call_id, tool, args, context) VALUES ('a1', 't1', 'c1', 'shell', '{}', '{}')`)
	cb := `{"update_id":20,"callback_query":{"id":"q1","data":"approve:a1","message":{"message_id":5,"chat":{"id":42}}}}`
	if rec := post(cb); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"approved"`) {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	post(cb)
	post(`{"update_id":21,"callback_query":{"id":"q2","data":"deny:a1"}}`)
	if n := storetest.Count(t, s.store, `SELECT COUNT(1) FROM outbox WHERE event_name = 'aether/approval.answered'`); n != 1 {
		t.Errorf("approval.answered events = %d, want 1", n)
	}
	if rec := post(`{"update_id":22,"callback_query":{"id":"q3","data":"bogus"}}`); rec.Code != http.StatusOK {
		t.Errorf("bogus callback status = %d, want 200", rec.Code)
	}
}
