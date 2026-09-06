package api

import (
	"context"
	"net/http"

	"github.com/linell/aether/internal/contract"
	"github.com/linell/aether/internal/store"
)

func (s *server) handleThreadReply(w http.ResponseWriter, r *http.Request) {
	var body messageBody
	if !decodeBody(w, r, &body) {
		return
	}
	body.ID = withID(body.ID)
	err := s.insertReply(r.Context(), r.PathValue("id"), body)
	respond(w, http.StatusOK, map[string]string{"reply": body.ID}, err)
}

func (s *server) insertReply(ctx context.Context, threadID string, body messageBody) error {
	return s.store.Tx(ctx, func(tx *store.Tx) error {
		daemon, err := threadDaemonName(ctx, tx, threadID)
		if err != nil {
			return err
		}
		inserted, err := insertMessage(ctx, tx, threadID, "assistant", body)
		if err != nil || !inserted {
			return err
		}
		_, err = tx.EnqueueOutbox(ctx, contract.EventMessageReplied, contract.MessageRepliedPayload{
			Daemon: daemon,
			Thread: threadID,
			Reply:  body.ID,
			Text:   body.Text,
		})
		return err
	})
}

func (s *server) handleThreadApprovals(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Calls []contract.Call `json:"calls"`
		State string          `json:"state"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if len(body.Calls) == 0 {
		writeError(w, http.StatusBadRequest, "calls must not be empty")
		return
	}
	ids, err := s.createApprovals(r.Context(), r.PathValue("id"), body.Calls, body.State)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"approval": ids[0], "approvals": ids})
}

func (s *server) createApprovals(ctx context.Context, threadID string, calls []contract.Call, state string) ([]string, error) {
	var ids []string
	err := s.store.Tx(ctx, func(tx *store.Tx) error {
		daemon, err := threadDaemonName(ctx, tx, threadID)
		if err != nil {
			return err
		}
		var created bool
		if ids, created, err = insertApprovals(ctx, tx, threadID, calls, state); err != nil || !created {
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

func insertApprovals(ctx context.Context, tx store.DBTX, threadID string, calls []contract.Call, state string) ([]string, bool, error) {
	ids := make([]string, len(calls))
	created := false
	for i, call := range calls {
		n, err := insertApproval(ctx, tx, threadID, call, state)
		if err != nil {
			return nil, false, err
		}
		created = created || n
		if err := tx.QueryRowContext(ctx, `SELECT id FROM approvals WHERE call_id = ?`, call.ID).Scan(&ids[i]); err != nil {
			return nil, false, err
		}
	}
	return ids, created, nil
}

func insertApproval(ctx context.Context, tx store.DBTX, threadID string, call contract.Call, state string) (bool, error) {
	out, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO approvals (id, thread_id, call_id, tool, args, context, state) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		store.NewID(), threadID, call.ID, call.Tool, string(call.Args), string(call.Context), state)
	if err != nil {
		return false, err
	}
	n, err := inserted(out)
	if err != nil || n {
		return n, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE approvals SET state = ? WHERE call_id = ? AND status = 'pending'`, state, call.ID)
	return false, err
}
