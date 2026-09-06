package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/linell/aether/internal/store"
)

type versionedDoc struct {
	Content string `json:"content"`
	Version int    `json:"version"`
}

func (s *server) handleGetSoul(w http.ResponseWriter, r *http.Request) {
	daemonID, err := daemonIDByName(r.Context(), s.store.DB(), r.PathValue("name"))
	if err != nil {
		writeErr(w, err)
		return
	}
	doc, err := latestDoc(r.Context(), s.store.DB(), "souls", daemonID)
	respond(w, http.StatusOK, doc, err)
}

func (s *server) handleGetMemory(w http.ResponseWriter, r *http.Request) {
	daemonID, err := daemonIDByName(r.Context(), s.store.DB(), r.PathValue("name"))
	if err != nil {
		writeErr(w, err)
		return
	}
	doc, err := currentMemory(r.Context(), s.store.DB(), daemonID)
	respond(w, http.StatusOK, doc, err)
}

type putMemoryBody struct {
	Content         string `json:"content"`
	ExpectedVersion int    `json:"expected_version"`
}

func (s *server) handlePutMemory(w http.ResponseWriter, r *http.Request) {
	var body putMemoryBody
	if !decodeBody(w, r, &body) {
		return
	}
	doc, conflict, err := s.putMemory(r.Context(), r.PathValue("name"), body)
	if conflict {
		writeJSON(w, http.StatusConflict, doc)
		return
	}
	respond(w, http.StatusOK, doc, err)
}

func (s *server) putMemory(ctx context.Context, name string, body putMemoryBody) (versionedDoc, bool, error) {
	var doc versionedDoc
	conflict := false
	err := s.store.Tx(ctx, func(tx *store.Tx) error {
		daemonID, err := daemonIDByName(ctx, tx, name)
		if err != nil {
			return err
		}
		current, err := currentMemory(ctx, tx, daemonID)
		if err != nil {
			return err
		}
		if current.Version != body.ExpectedVersion {
			doc, conflict = current, true
			return nil
		}
		doc = versionedDoc{Content: body.Content, Version: current.Version + 1}
		_, err = tx.ExecContext(ctx,
			`INSERT INTO memory (id, daemon_id, version, body) VALUES (?, ?, ?, ?)`,
			store.NewID(), daemonID, doc.Version, doc.Content,
		)
		return err
	})
	return doc, conflict, err
}

func currentMemory(ctx context.Context, q store.DBTX, daemonID string) (versionedDoc, error) {
	doc, err := latestDoc(ctx, q, "memory", daemonID)
	if errors.Is(err, errNotFound) {
		return versionedDoc{}, nil
	}
	return doc, err
}

func latestDoc(ctx context.Context, q store.DBTX, table, daemonID string) (versionedDoc, error) {
	var doc versionedDoc
	err := lookup(ctx, q,
		`SELECT body, version FROM `+table+` WHERE daemon_id = ? ORDER BY version DESC LIMIT 1`,
		daemonID, &doc.Content, &doc.Version)
	return doc, err
}
