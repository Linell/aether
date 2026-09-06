package api

import (
	"context"
	"net/http"
	"path/filepath"

	"github.com/linell/aether/internal/contract"
	"github.com/linell/aether/internal/store"
)

type messageBody struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

func (s *server) handleCreateMessage(w http.ResponseWriter, r *http.Request) {
	var body messageBody
	if !decodeBody(w, r, &body) {
		return
	}
	if body.Text == "" {
		writeError(w, http.StatusBadRequest, "text must not be empty")
		return
	}
	body.ID = withID(body.ID)
	threadID, err := s.createMessage(r.Context(), r.PathValue("name"), body)
	respond(w, http.StatusOK, map[string]string{"thread": threadID, "message": body.ID}, err)
}

func (s *server) createMessage(ctx context.Context, name string, body messageBody) (string, error) {
	var threadID string
	err := s.store.Tx(ctx, func(tx *store.Tx) error {
		daemonID, err := daemonIDByName(ctx, tx, name)
		if err != nil {
			return err
		}
		if threadID, err = ensureThread(ctx, tx, daemonID, name); err != nil {
			return err
		}
		inserted, err := insertMessage(ctx, tx, threadID, "user", body)
		if err != nil || !inserted {
			return err
		}
		_, err = tx.EnqueueOutbox(ctx, contract.EventMessageSent, contract.MessageSentPayload{
			Daemon: name, Thread: threadID, Message: body.ID, Text: body.Text,
		})
		return err
	})
	return threadID, err
}

func insertMessage(ctx context.Context, tx store.DBTX, threadID, role string, body messageBody) (bool, error) {
	out, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO messages (id, thread_id, role, text, status) VALUES (?, ?, ?, ?, 'sent')`,
		body.ID, threadID, role, body.Text)
	if err != nil {
		return false, err
	}
	return inserted(out)
}

func ensureThread(ctx context.Context, tx store.DBTX, daemonID, name string) (string, error) {
	var id string
	err := lookup(ctx, tx,
		`SELECT id FROM threads WHERE daemon_id = ? ORDER BY created_at DESC, id DESC LIMIT 1`,
		daemonID, &id)
	if err == nil || err != errNotFound {
		return id, err
	}
	return createThread(ctx, tx, daemonID, name)
}

func createThread(ctx context.Context, tx store.DBTX, daemonID, name string) (string, error) {
	var hostID, root string
	err := lookup(ctx, tx,
		`SELECT h.id, h.root FROM daemons d JOIN hosts h ON h.id = d.host_id WHERE d.id = ?`,
		daemonID, &hostID, &root)
	if err != nil {
		return "", err
	}
	id := store.NewID()
	_, err = tx.ExecContext(ctx,
		`INSERT INTO threads (id, daemon_id, host_id, directory) VALUES (?, ?, ?, ?)`,
		id, daemonID, hostID, filepath.Join(root, name))
	return id, err
}
