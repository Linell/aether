package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"

	"github.com/linell/aether/internal/store"
)

var markerKinds = []string{"schedule.stale", "turn.failed", "memory.conflict"}

type markerBody struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Thread string `json:"thread"`
	Ref    string `json:"ref"`
	Detail string `json:"detail"`
}

func (s *server) handleCreateMarker(w http.ResponseWriter, r *http.Request) {
	var body markerBody
	if !decodeBody(w, r, &body) {
		return
	}
	if !slices.Contains(markerKinds, body.Kind) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid kind: %q", body.Kind))
		return
	}
	if body.ID == "" {
		body.ID = store.NewID()
	}
	err := s.createMarker(r.Context(), r.PathValue("name"), body)
	respond(w, http.StatusOK, map[string]string{"marker": body.ID}, err)
}

func (s *server) createMarker(ctx context.Context, name string, body markerBody) error {
	return s.store.Tx(ctx, func(tx *store.Tx) error {
		daemonID, err := daemonIDByName(ctx, tx, name)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO markers (id, kind, daemon_id, thread_id, ref_id, detail) VALUES (?, ?, ?, ?, ?, ?)`,
			body.ID, body.Kind, daemonID, body.Thread, body.Ref, body.Detail)
		return err
	})
}
