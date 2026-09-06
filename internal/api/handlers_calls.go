package api

import (
	"context"
	"net/http"

	"github.com/linell/aether/internal/contract"
	"github.com/linell/aether/internal/policy"
	"github.com/linell/aether/internal/store"
)

type threadDoc struct {
	ID        string `json:"id"`
	Daemon    string `json:"daemon"`
	Host      string `json:"host"`
	Directory string `json:"directory"`
}

func (s *server) handleGetThread(w http.ResponseWriter, r *http.Request) {
	doc, err := threadByID(r.Context(), s.store.DB(), r.PathValue("id"))
	respond(w, http.StatusOK, doc, err)
}

func threadByID(ctx context.Context, q store.DBTX, id string) (threadDoc, error) {
	doc := threadDoc{ID: id}
	err := lookup(ctx, q,
		`SELECT d.name, h.name, t.directory FROM threads t
		 JOIN daemons d ON d.id = t.daemon_id JOIN hosts h ON h.id = t.host_id WHERE t.id = ?`,
		id, &doc.Daemon, &doc.Host, &doc.Directory)
	return doc, err
}

type matchBody struct {
	Thread string        `json:"thread"`
	Call   contract.Call `json:"call"`
}

type matchDoc struct {
	Allowed bool   `json:"allowed"`
	Rule    string `json:"rule,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

func (s *server) handleMatchCall(w http.ResponseWriter, r *http.Request) {
	var body matchBody
	if !decodeBody(w, r, &body) {
		return
	}
	thread, err := threadByID(r.Context(), s.store.DB(), body.Thread)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, matchCall(s.rules, r.PathValue("name"), thread, body.Call))
}

func matchCall(rules []policy.Rule, daemon string, thread threadDoc, call contract.Call) matchDoc {
	if thread.Daemon != daemon {
		return matchDoc{Reason: "thread belongs to another daemon"}
	}
	var callCtx struct {
		Cwd string `json:"cwd"`
	}
	if err := decodeRaw(call.Context, &callCtx); err != nil || callCtx.Cwd != thread.Directory {
		return matchDoc{Reason: "call context does not match the thread directory"}
	}
	rule, ok := policy.Match(rules, policy.Request{Daemon: daemon, Tool: call.Tool, Args: call.Args, Cwd: thread.Directory})
	if !ok {
		return matchDoc{Reason: "no rule matched"}
	}
	return matchDoc{Allowed: true, Rule: rule.String()}
}

func (s *server) handleClaimOperation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID   string `json:"id"`
		Kind string `json:"kind"`
	}
	if !decodeBody(w, r, &body) || !requireFields(w, body.ID, body.Kind) {
		return
	}
	claimed := false
	err := s.store.Tx(r.Context(), func(tx *store.Tx) error {
		out, err := tx.ExecContext(r.Context(), `INSERT OR IGNORE INTO operations (id, kind) VALUES (?, ?)`, body.ID, body.Kind)
		if err != nil {
			return err
		}
		claimed, err = inserted(out)
		return err
	})
	respond(w, http.StatusOK, map[string]bool{"claimed": claimed}, err)
}
