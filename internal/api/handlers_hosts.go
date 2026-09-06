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
		Root string `json:"root"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	host, err := s.registerHost(r.Context(), body.Name, body.Root)
	respond(w, http.StatusOK, host, err)
}

func (s *server) registerHost(ctx context.Context, name, root string) (hostDoc, error) {
	var host hostDoc
	err := s.store.Tx(ctx, func(tx *store.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO hosts (id, name, root) VALUES (?, ?, ?)
			 ON CONFLICT(name) DO UPDATE SET root = excluded.root`, store.NewID(), name, root,
		); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `SELECT id, name FROM hosts WHERE name = ?`, name).Scan(&host.ID, &host.Name)
	})
	return host, err
}

type daemonSummary struct {
	Name   string `json:"name"`
	Class  string `json:"class"`
	Status string `json:"status"`
}

func (s *server) handleHostDaemons(w http.ResponseWriter, r *http.Request) {
	daemons, err := s.hostDaemons(r.Context(), r.PathValue("name"))
	respond(w, http.StatusOK, daemons, err)
}

func (s *server) hostDaemons(ctx context.Context, hostName string) ([]daemonSummary, error) {
	hostID, err := hostIDByName(ctx, s.store.DB(), hostName)
	if err != nil {
		return nil, err
	}
	return store.Query(ctx, s.store.DB(), scanDaemonSummary,
		`SELECT name, class, status FROM daemons WHERE host_id = ? AND status = 'active'`, hostID)
}

func scanDaemonSummary(rows *sql.Rows) (daemonSummary, error) {
	var d daemonSummary
	err := rows.Scan(&d.Name, &d.Class, &d.Status)
	return d, err
}
