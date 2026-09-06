package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"

	"github.com/linell/aether/internal/contract"
	"github.com/linell/aether/internal/store"
)

var (
	daemonNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
	errConflict       = errors.New("conflict")
)

type daemonDoc struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Host          string `json:"host"`
	Class         string `json:"class"`
	OfflinePolicy string `json:"offline_policy"`
	Status        string `json:"status"`
}

type createDaemonBody struct {
	Name          string `json:"name"`
	Host          string `json:"host"`
	Class         string `json:"class"`
	OfflinePolicy string `json:"offline_policy"`
}

func (s *server) handleCreateDaemon(w http.ResponseWriter, r *http.Request) {
	var body createDaemonBody
	if !decodeBody(w, r, &body) {
		return
	}
	if err := validateDaemon(body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	doc, created, err := s.createDaemon(r.Context(), body)
	respond(w, statusCreated(created), doc, err)
}

func statusCreated(created bool) int {
	if created {
		return http.StatusCreated
	}
	return http.StatusOK
}

func validateDaemon(body createDaemonBody) error {
	if !daemonNamePattern.MatchString(body.Name) {
		return fmt.Errorf("invalid name: %q", body.Name)
	}
	if body.Host == "" {
		return errors.New("host is required")
	}
	if body.Class != "anchored" && body.Class != "opportunistic" {
		return fmt.Errorf("invalid class: %q", body.Class)
	}
	return validatePolicy(body.OfflinePolicy, nil)
}

func (s *server) createDaemon(ctx context.Context, body createDaemonBody) (daemonDoc, bool, error) {
	var doc daemonDoc
	created := false
	err := s.store.Tx(ctx, func(tx *store.Tx) error {
		hostID, err := hostIDByName(ctx, tx, body.Host)
		if err != nil {
			return err
		}
		if created, err = insertDaemon(ctx, tx, body, hostID); err != nil {
			return err
		}
		if doc, err = daemonByName(ctx, tx, body.Name); err != nil {
			return err
		}
		return enqueueConjure(ctx, tx, doc, body.Host, created)
	})
	return doc, created, err
}

func insertDaemon(ctx context.Context, tx store.DBTX, body createDaemonBody, hostID string) (bool, error) {
	out, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO daemons (id, name, host_id, class, offline_policy) VALUES (?, ?, ?, ?, ?)`,
		store.NewID(), body.Name, hostID, body.Class, body.OfflinePolicy)
	if err != nil {
		return false, err
	}
	n, err := out.RowsAffected()
	return n == 1, err
}

func enqueueConjure(ctx context.Context, tx *store.Tx, doc daemonDoc, host string, created bool) error {
	if !created {
		if doc.Host != host {
			return errConflict
		}
		return nil
	}
	_, err := tx.EnqueueOutbox(ctx, contract.EventConjureRequested, contract.ConjureRequestedPayload{
		Daemon:   doc.Name,
		Host:     host,
		Language: "ts",
	})
	return err
}

func daemonByName(ctx context.Context, q store.DBTX, name string) (daemonDoc, error) {
	var d daemonDoc
	err := lookup(ctx, q,
		`SELECT d.id, d.name, h.name, d.class, d.offline_policy, d.status
		 FROM daemons d JOIN hosts h ON h.id = d.host_id WHERE d.name = ?`,
		name, &d.ID, &d.Name, &d.Host, &d.Class, &d.OfflinePolicy, &d.Status)
	return d, err
}
