package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/linell/aether/internal/contract"
	"github.com/linell/aether/internal/store"
)

func (s *server) handleThreadReply(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("id")
	var body struct {
		Text string `json:"text"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	replyID, err := s.insertReply(r.Context(), threadID, body.Text)
	if err != nil {
		writeTxErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"reply": replyID})
}

func (s *server) insertReply(ctx context.Context, threadID, text string) (string, error) {
	replyID := store.NewID()
	err := s.store.Tx(ctx, func(tx *sql.Tx) error {
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
		_, err = s.store.EnqueueOutbox(ctx, tx, contract.EventMessageReplied, contract.MessageRepliedPayload{
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
	threadID := r.PathValue("id")
	calls, err := decodeApprovalCalls(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ids, err := s.createApprovals(r.Context(), threadID, calls)
	if err != nil {
		writeTxErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"approval": ids[0], "approvals": ids})
}

func decodeApprovalCalls(r *http.Request) ([]contract.Call, error) {
	var body struct {
		Calls []contract.Call `json:"calls"`
	}
	if err := decodeJSON(r, &body); err != nil {
		return nil, errors.New("invalid body")
	}
	if len(body.Calls) == 0 {
		return nil, errors.New("calls must not be empty")
	}
	return body.Calls, nil
}

func (s *server) createApprovals(ctx context.Context, threadID string, calls []contract.Call) ([]string, error) {
	var ids []string
	err := s.store.Tx(ctx, func(tx *sql.Tx) error {
		daemon, err := threadDaemonName(ctx, tx, threadID)
		if err != nil {
			return err
		}
		ids, err = insertApprovals(ctx, tx, threadID, calls)
		if err != nil {
			return err
		}
		_, err = s.store.EnqueueOutbox(ctx, tx, contract.EventApprovalRequested, contract.ApprovalRequestedPayload{
			Daemon: daemon,
			Thread: threadID,
			Calls:  calls,
		})
		return err
	})
	return ids, err
}

func insertApprovals(ctx context.Context, tx *sql.Tx, threadID string, calls []contract.Call) ([]string, error) {
	ids := make([]string, len(calls))
	for i, call := range calls {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO approvals (id, thread_id, call_id, tool, args, context) VALUES (?, ?, ?, ?, ?, ?)`,
			store.NewID(), threadID, call.ID, call.Tool, string(call.Args), string(call.Context),
		); err != nil {
			return nil, err
		}
		var approvalID string
		if err := tx.QueryRowContext(ctx,
			`SELECT id FROM approvals WHERE call_id = ?`, call.ID,
		).Scan(&approvalID); err != nil {
			return nil, err
		}
		ids[i] = approvalID
	}
	return ids, nil
}
