package api

import (
	"context"
	"net/http"

	"github.com/linell/aether/internal/contract"
	"github.com/linell/aether/internal/store"
)

func (s *server) handleThreadReply(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text string `json:"text"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	replyID, err := s.insertReply(r.Context(), r.PathValue("id"), body.Text)
	respond(w, http.StatusOK, map[string]string{"reply": replyID}, err)
}

func (s *server) insertReply(ctx context.Context, threadID, text string) (string, error) {
	replyID := store.NewID()
	err := s.store.Tx(ctx, func(tx *store.Tx) error {
		daemon, err := threadDaemonName(ctx, tx, threadID)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO messages (id, thread_id, role, text, status) VALUES (?, ?, 'assistant', ?, 'sent')`,
			replyID, threadID, text,
		); err != nil {
			return err
		}
		_, err = tx.EnqueueOutbox(ctx, contract.EventMessageReplied, contract.MessageRepliedPayload{
			Daemon: daemon,
			Thread: threadID,
			Reply:  replyID,
			Text:   text,
		})
		return err
	})
	return replyID, err
}

func (s *server) handleThreadApprovals(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Calls []contract.Call `json:"calls"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if len(body.Calls) == 0 {
		writeError(w, http.StatusBadRequest, "calls must not be empty")
		return
	}
	ids, err := s.createApprovals(r.Context(), r.PathValue("id"), body.Calls)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"approval": ids[0], "approvals": ids})
}

func (s *server) createApprovals(ctx context.Context, threadID string, calls []contract.Call) ([]string, error) {
	var ids []string
	err := s.store.Tx(ctx, func(tx *store.Tx) error {
		daemon, err := threadDaemonName(ctx, tx, threadID)
		if err != nil {
			return err
		}
		ids, err = insertApprovals(ctx, tx, threadID, calls)
		if err != nil {
			return err
		}
		_, err = tx.EnqueueOutbox(ctx, contract.EventApprovalRequested, contract.ApprovalRequestedPayload{
			Daemon: daemon,
			Thread: threadID,
			Calls:  calls,
		})
		return err
	})
	return ids, err
}

func insertApprovals(ctx context.Context, tx store.DBTX, threadID string, calls []contract.Call) ([]string, error) {
	ids := make([]string, len(calls))
	for i, call := range calls {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO approvals (id, thread_id, call_id, tool, args, context) VALUES (?, ?, ?, ?, ?, ?)`,
			store.NewID(), threadID, call.ID, call.Tool, string(call.Args), string(call.Context),
		); err != nil {
			return nil, err
		}
		if err := tx.QueryRowContext(ctx, `SELECT id FROM approvals WHERE call_id = ?`, call.ID).Scan(&ids[i]); err != nil {
			return nil, err
		}
	}
	return ids, nil
}
