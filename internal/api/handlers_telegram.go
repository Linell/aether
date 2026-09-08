package api

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/linell/aether/internal/store"
	"github.com/linell/aether/internal/telegram"
)

var (
	errIgnored   = errors.New("ignored")
	errDuplicate = ignored("duplicate update")
)

type ignored string

func (e ignored) Error() string        { return string(e) }
func (e ignored) Is(target error) bool { return target == errIgnored }

type Telegram struct {
	Client        *telegram.Client
	WebhookSecret string
}

func (s *server) handleTelegramWebhook(w http.ResponseWriter, r *http.Request) {
	if s.telegram.Client == nil || s.telegram.WebhookSecret == "" {
		writeError(w, http.StatusNotFound, "telegram disabled")
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get(telegram.SecretHeader)), []byte(s.telegram.WebhookSecret)) != 1 {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var update telegram.Update
	if !decodeBody(w, r, &update) {
		return
	}
	result, err := s.handleUpdate(r.Context(), update)
	if errors.Is(err, errIgnored) {
		log.Printf("telegram: update %d ignored: %v", update.ID, err)
		writeJSON(w, http.StatusOK, map[string]string{"ignored": err.Error()})
		return
	}
	respond(w, http.StatusOK, result, err)
}

func (s *server) handleUpdate(ctx context.Context, u telegram.Update) (any, error) {
	switch {
	case u.Callback != nil:
		return s.telegramCallback(ctx, u)
	case u.Message != nil && u.Message.Text != "":
		return s.telegramText(ctx, u)
	}
	return nil, ignored("no text or callback")
}

func (s *server) telegramText(ctx context.Context, u telegram.Update) (map[string]string, error) {
	msg := u.Message
	body := messageBody{ID: store.NewID(), Text: msg.Text}
	var threadID string
	err := s.store.Tx(ctx, func(tx *store.Tx) error {
		if err := claimUpdate(ctx, tx, u.ID); err != nil {
			return err
		}
		daemon, thread, err := s.bindChat(ctx, tx, msg.Chat.ID, msg.Text)
		if err != nil {
			return err
		}
		threadID = thread
		if _, isStart := telegram.StartCommand(msg.Text); isStart {
			return nil
		}
		return insertUserMessage(ctx, tx, daemon, thread, body)
	})
	return map[string]string{"thread": threadID, "message": body.ID}, err
}

func claimUpdate(ctx context.Context, tx store.DBTX, id int64) error {
	out, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO operations (id, kind) VALUES (?, 'telegram.update')`,
		"telegram:update:"+strconv.FormatInt(id, 10))
	if err != nil {
		return err
	}
	if fresh, err := inserted(out); err != nil || fresh {
		return err
	}
	return errDuplicate
}

func (s *server) bindChat(ctx context.Context, tx store.DBTX, chat int64, text string) (string, string, error) {
	external := strconv.FormatInt(chat, 10)
	var threadID string
	err := lookup(ctx, tx, `SELECT thread_id FROM channels WHERE kind = 'telegram' AND external_id = ?`, external, &threadID)
	if err == nil {
		daemon, err := threadDaemonName(ctx, tx, threadID)
		return daemon, threadID, err
	}
	if !errors.Is(err, errNotFound) {
		return "", "", err
	}
	return s.bindNewChat(ctx, tx, external, text)
}

func (s *server) bindNewChat(ctx context.Context, tx store.DBTX, external, text string) (string, string, error) {
	daemon, err := daemonForChat(ctx, tx, text)
	if err != nil {
		return "", "", err
	}
	daemonID, err := daemonIDByName(ctx, tx, daemon)
	if err != nil {
		return "", "", err
	}
	threadID, err := ensureThread(ctx, tx, daemonID, daemon)
	if err != nil {
		return "", "", err
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO channels (id, kind, external_id, thread_id) VALUES (?, 'telegram', ?, ?)`,
		store.NewID(), external, threadID)
	return daemon, threadID, err
}

func daemonForChat(ctx context.Context, q store.DBTX, text string) (string, error) {
	if name, ok := telegram.StartCommand(text); ok && name != "" {
		return name, nil
	}
	names, err := store.Query(ctx, q, scanString, `SELECT name FROM daemons WHERE status = 'active'`)
	if err != nil {
		return "", err
	}
	if len(names) != 1 {
		return "", ignored("unbound chat: send /start <daemon>")
	}
	return names[0], nil
}

func (s *server) telegramCallback(ctx context.Context, u telegram.Update) (approvalDoc, error) {
	decision, ok := telegram.ParseCallback(u.Callback.Data)
	if !ok {
		return approvalDoc{}, ignored("unknown callback")
	}
	var doc approvalDoc
	err := s.store.Tx(ctx, func(tx *store.Tx) error {
		if err := claimUpdate(ctx, tx, u.ID); err != nil {
			return err
		}
		var err error
		doc, err = answerApproval(ctx, tx, decision.Approval, decision.Decision)
		return err
	})
	if err == nil {
		s.acknowledgeCallback(u.Callback, doc)
	}
	return doc, err
}

func ackText(doc approvalDoc) string {
	if doc.Remaining == 0 {
		return doc.Status
	}
	return fmt.Sprintf("%s, %d more pending", doc.Status, doc.Remaining)
}

func (s *server) acknowledgeCallback(cb *telegram.CallbackQuery, doc approvalDoc) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.telegram.Client.AnswerCallback(ctx, cb.ID, ackText(doc)); err != nil {
		log.Printf("telegram: answer callback: %v", err)
	}
	if cb.Message == nil {
		return
	}
	text, _ := telegram.RenderApproval(telegram.PendingCall{Approval: doc.ID, Status: doc.Status, Daemon: doc.Daemon, Call: doc.Call})
	if err := s.telegram.Client.EditText(ctx, cb.Message.Chat.ID, cb.Message.MessageID, text); err != nil {
		log.Printf("telegram: edit message: %v", err)
	}
}
