package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/linell/aether/internal/contract"
	"github.com/linell/aether/internal/store"
)

func (s *server) handleThreadReply(w http.ResponseWriter, r *http.Request) {
	var body messageBody
	if !decodeBody(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Text) == "" {
		writeError(w, http.StatusBadRequest, "text must not be empty")
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

type pauseBody struct {
	Calls []contract.Call `json:"calls"`
	State string          `json:"state"`
	Run   string          `json:"run"`
}

type pauseDoc struct {
	Group     string   `json:"group"`
	Approvals []string `json:"approvals"`
}

func (s *server) handleThreadApprovals(w http.ResponseWriter, r *http.Request) {
	var body pauseBody
	if !decodeBody(w, r, &body) || !requireFields(w, body.Run) {
		return
	}
	if len(body.Calls) == 0 {
		writeError(w, http.StatusBadRequest, "calls must not be empty")
		return
	}
	doc, err := s.createApprovals(r.Context(), r.PathValue("id"), body)
	respond(w, http.StatusOK, doc, err)
}

func (s *server) createApprovals(ctx context.Context, threadID string, body pauseBody) (pauseDoc, error) {
	var doc pauseDoc
	err := s.store.Tx(ctx, func(tx *store.Tx) error {
		daemon, err := threadDaemonName(ctx, tx, threadID)
		if err != nil {
			return err
		}
		var created bool
		if doc.Group, created, err = insertGroup(ctx, tx, threadID, body.Run, body.State); err != nil {
			return err
		}
		if doc.Approvals, err = insertApprovals(ctx, tx, threadID, doc.Group, body.Calls); err != nil || !created {
			return err
		}
		_, err = tx.EnqueueOutbox(ctx, contract.EventApprovalRequested, contract.ApprovalRequestedPayload{
			Daemon: daemon,
			Thread: threadID,
			Calls:  body.Calls,
		})
		return err
	})
	return doc, err
}

func insertGroup(ctx context.Context, tx store.DBTX, threadID, run, state string) (string, bool, error) {
	out, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO approval_groups (id, thread_id, run_id, state) VALUES (?, ?, ?, ?)`,
		store.NewID(), threadID, run, state)
	if err != nil {
		return "", false, err
	}
	created, err := inserted(out)
	if err != nil {
		return "", false, err
	}
	var id string
	err = tx.QueryRowContext(ctx, `SELECT id FROM approval_groups WHERE run_id = ?`, run).Scan(&id)
	return id, created, err
}

func insertApprovals(ctx context.Context, tx store.DBTX, threadID, group string, calls []contract.Call) ([]string, error) {
	ids := make([]string, len(calls))
	for i, call := range calls {
		if err := insertApproval(ctx, tx, threadID, group, call); err != nil {
			return nil, err
		}
		if err := tx.QueryRowContext(ctx, `SELECT id FROM approvals WHERE call_id = ?`, call.ID).Scan(&ids[i]); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

func insertApproval(ctx context.Context, tx store.DBTX, threadID, group string, call contract.Call) error {
	out, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO approvals (id, thread_id, call_id, tool, args, context, group_id) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		store.NewID(), threadID, call.ID, call.Tool, string(call.Args), string(call.Context), group)
	if err != nil {
		return err
	}
	if n, err := inserted(out); err != nil || n {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE approvals SET group_id = ? WHERE call_id = ? AND status = 'pending'`, group, call.ID)
	return err
}
