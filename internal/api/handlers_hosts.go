package api

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/linell/aether/internal/store"
)

type hostDoc struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (s *server) handleCreateHost(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	host, err := s.registerHost(r.Context(), body.Name)
	if err != nil {
		writeTxErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, host)
}

func (s *server) registerHost(ctx context.Context, name string) (hostDoc, error) {
	var host hostDoc
	err := s.store.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO hosts (id, name) VALUES (?, ?)`, store.NewID(), name,
		); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx,
			`SELECT id, name FROM hosts WHERE name = ?`, name,
		).Scan(&host.ID, &host.Name)
	})
	return host, err
}

type daemonSummary struct {
	Name   string `json:"name"`
	Class  string `json:"class"`
	Status string `json:"status"`
}

func (s *server) handleHostDaemons(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	hostID, err := hostIDByName(r.Context(), s.store.DB(), name)
	if err != nil {
		writeTxErr(w, err)
		return
	}
	daemons, err := listHostDaemons(r.Context(), s.store.DB(), hostID)
	if err != nil {
		writeTxErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, daemons)
}

func listHostDaemons(ctx context.Context, db *sql.DB, hostID string) ([]daemonSummary, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT name, class, status FROM daemons WHERE host_id = ? AND status = 'active'`, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []daemonSummary{}
	for rows.Next() {
		var d daemonSummary
		if err := rows.Scan(&d.Name, &d.Class, &d.Status); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
