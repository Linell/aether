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
	ID     string        `json:"id"`
	Daemon string        `json:"daemon"`
	Thread string        `json:"thread"`
	Status string        `json:"status"`
	Call   contract.Call `json:"call"`
	State  string        `json:"state,omitempty"`
}

func (s *server) handleGetApproval(w http.ResponseWriter, r *http.Request) {
	doc, err := approvalByID(r.Context(), s.store.DB(), r.PathValue("id"))
	respond(w, http.StatusOK, doc, err)
}

func approvalByID(ctx context.Context, q store.DBTX, id string) (approvalDoc, error) {
	doc := approvalDoc{ID: id}
	var args, callCtx string
	var state sql.NullString
	err := lookup(ctx, q,
		`SELECT d.name, a.thread_id, a.status, a.call_id, a.tool, a.args, a.context, a.state
		 FROM approvals a JOIN threads t ON t.id = a.thread_id JOIN daemons d ON d.id = t.daemon_id
		 WHERE a.id = ?`,
		id, &doc.Daemon, &doc.Thread, &doc.Status, &doc.Call.ID, &doc.Call.Tool, &args, &callCtx, &state)
	doc.Call.Args, doc.Call.Context, doc.State = json.RawMessage(args), json.RawMessage(callCtx), state.String
	return doc, err
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
	return doc, recordDecision(ctx, tx, doc)
}

func recordDecision(ctx context.Context, tx *store.Tx, doc approvalDoc) error {
	if _, err := tx.ExecContext(ctx,
		`UPDATE approvals SET status = ?, decision_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`,
		doc.Status, doc.ID); err != nil {
		return err
	}
	_, err := tx.EnqueueOutbox(ctx, contract.EventApprovalAnswered, contract.ApprovalAnsweredPayload{
		Daemon: doc.Daemon, Thread: doc.Thread, Call: doc.Call.ID, Approval: doc.ID, Decision: doc.Status,
	})
	return err
}
