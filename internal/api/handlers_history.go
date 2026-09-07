package api

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"

	"github.com/linell/aether/internal/store"
)

const historyLimit = 200

type messageDoc struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Text      string `json:"text"`
	CreatedAt string `json:"created_at"`
}

func (s *server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	messages, err := s.listMessages(r.Context(), r.PathValue("id"), q.Get("before"), limitOf(q.Get("limit")))
	respond(w, http.StatusOK, map[string]any{"messages": messages}, err)
}

func limitOf(raw string) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > historyLimit {
		return historyLimit
	}
	return n
}

func (s *server) listMessages(ctx context.Context, threadID, before string, limit int) ([]messageDoc, error) {
	db := s.store.DB()
	if _, err := threadByID(ctx, db, threadID); err != nil {
		return nil, err
	}
	if before == "" {
		return store.Query(ctx, db, scanMessage, historyQuery(""), threadID, limit)
	}
	at, err := cursorAt(ctx, db, threadID, before)
	if err != nil {
		return nil, err
	}
	return store.Query(ctx, db, scanMessage, historyQuery(`AND (created_at, id) < (?, ?)`), threadID, at, before, limit)
}

func cursorAt(ctx context.Context, q store.DBTX, threadID, id string) (string, error) {
	var thread, at string
	if err := lookup(ctx, q, `SELECT thread_id, created_at FROM messages WHERE id = ?`, id, &thread, &at); err != nil {
		return "", err
	}
	if thread != threadID {
		return "", errNotFound
	}
	return at, nil
}

func historyQuery(cursor string) string {
	return `SELECT id, role, text, created_at FROM messages
		WHERE thread_id = ? AND role IN ('user', 'assistant') ` + cursor + `
		ORDER BY created_at DESC, id DESC LIMIT ?`
}

func scanMessage(rows *sql.Rows) (messageDoc, error) {
	var doc messageDoc
	err := rows.Scan(&doc.ID, &doc.Role, &doc.Text, &doc.CreatedAt)
	return doc, err
}
