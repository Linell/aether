package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/linell/aether/internal/contract"
	"github.com/linell/aether/internal/store"
)

type approvalDoc struct {
	ID        string        `json:"id"`
	Daemon    string        `json:"daemon"`
	Thread    string        `json:"thread"`
	Group     string        `json:"group,omitempty"`
	Status    string        `json:"status"`
	Call      contract.Call `json:"call"`
	Remaining int           `json:"remaining"`
}

type groupDoc struct {
	ID        string              `json:"id"`
	Thread    string              `json:"thread"`
	Status    string              `json:"status"`
	State     string              `json:"state"`
	Decisions []contract.Decision `json:"decisions"`
}

func (s *server) handleGetApproval(w http.ResponseWriter, r *http.Request) {
	doc, err := approvalByID(r.Context(), s.store.DB(), r.PathValue("id"))
	respond(w, http.StatusOK, doc, err)
}

func approvalByID(ctx context.Context, q store.DBTX, id string) (approvalDoc, error) {
	doc := approvalDoc{ID: id}
	var args, callCtx string
	var group sql.NullString
	err := lookup(ctx, q,
		`SELECT d.name, a.thread_id, a.group_id, a.status, a.call_id, a.tool, a.args, a.context,
		        (SELECT COUNT(1) FROM approvals p WHERE p.group_id = a.group_id AND p.status = 'pending' AND p.id != a.id)
		 FROM approvals a JOIN threads t ON t.id = a.thread_id JOIN daemons d ON d.id = t.daemon_id
		 WHERE a.id = ?`,
		id, &doc.Daemon, &doc.Thread, &group, &doc.Status, &doc.Call.ID, &doc.Call.Tool, &args, &callCtx, &doc.Remaining)
	doc.Call.Args, doc.Call.Context, doc.Group = json.RawMessage(args), json.RawMessage(callCtx), group.String
	return doc, err
}

func (s *server) handleGetApprovalGroup(w http.ResponseWriter, r *http.Request) {
	doc, err := groupByID(r.Context(), s.store.DB(), r.PathValue("id"))
	respond(w, http.StatusOK, doc, err)
}

func groupByID(ctx context.Context, q store.DBTX, id string) (groupDoc, error) {
	doc := groupDoc{ID: id}
	err := lookup(ctx, q, `SELECT thread_id, status, state FROM approval_groups WHERE id = ?`, id, &doc.Thread, &doc.Status, &doc.State)
	if err != nil {
		return doc, err
	}
	doc.Decisions, err = groupDecisions(ctx, q, id)
	return doc, err
}

func groupDecisions(ctx context.Context, q store.DBTX, group string) ([]contract.Decision, error) {
	return store.Query(ctx, q, scanDecision,
		`SELECT call_id, id, status FROM approvals WHERE group_id = ? AND status != 'pending' ORDER BY created_at, id`, group)
}

func scanDecision(rows *sql.Rows) (contract.Decision, error) {
	var d contract.Decision
	err := rows.Scan(&d.Call, &d.Approval, &d.Decision)
	return d, err
}

func (s *server) handleAnswerApproval(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Decision string `json:"decision"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.Decision != "approved" && body.Decision != "denied" {
		writeError(w, http.StatusBadRequest, "decision must be approved or denied")
		return
	}
	doc, err := s.answerApproval(r.Context(), r.PathValue("id"), body.Decision)
	respond(w, http.StatusOK, doc, err)
}

func (s *server) answerApproval(ctx context.Context, id, decision string) (approvalDoc, error) {
	var doc approvalDoc
	err := s.store.Tx(ctx, func(tx *store.Tx) error {
		var err error
		doc, err = answerApproval(ctx, tx, id, decision)
		return err
	})
	return doc, err
}

func answerApproval(ctx context.Context, tx *store.Tx, id, decision string) (approvalDoc, error) {
	doc, err := approvalByID(ctx, tx, id)
	if err != nil || doc.Status != "pending" {
		return doc, err
	}
	doc.Status = decision
	if err := recordDecision(ctx, tx, doc); err != nil {
		return doc, err
	}
	return doc, settleGroup(ctx, tx, doc)
}

func recordDecision(ctx context.Context, tx *store.Tx, doc approvalDoc) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE approvals SET status = ?, decision_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`,
		doc.Status, doc.ID)
	return err
}

func settleGroup(ctx context.Context, tx *store.Tx, doc approvalDoc) error {
	if doc.Group == "" || doc.Remaining > 0 {
		return nil
	}
	out, err := tx.ExecContext(ctx, `UPDATE approval_groups SET status = 'decided' WHERE id = ? AND status = 'pending'`, doc.Group)
	if err != nil {
		return err
	}
	if n, err := inserted(out); err != nil || !n {
		return err
	}
	decisions, err := groupDecisions(ctx, tx, doc.Group)
	if err != nil {
		return err
	}
	_, err = tx.EnqueueOutbox(ctx, contract.EventApprovalAnswered, contract.ApprovalAnsweredPayload{
		Daemon: doc.Daemon, Thread: doc.Thread, Group: doc.Group, Decisions: decisions,
	})
	return err
}
