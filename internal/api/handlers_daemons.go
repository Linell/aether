package api

import (
	"context"
	"database/sql"
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
		writeTxErr(w, err)
		return
	}
	doc, err := latestSoul(r.Context(), s.store.DB(), daemonID)
	if err != nil {
		writeTxErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *server) handleGetMemory(w http.ResponseWriter, r *http.Request) {
	daemonID, err := daemonIDByName(r.Context(), s.store.DB(), r.PathValue("name"))
	if err != nil {
		writeTxErr(w, err)
		return
	}
	doc, err := latestMemory(r.Context(), s.store.DB(), daemonID)
	if errors.Is(err, errNotFound) {
		writeJSON(w, http.StatusOK, versionedDoc{})
		return
	}
	if err != nil {
		writeTxErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

type putMemoryBody struct {
	Content         string `json:"content"`
	ExpectedVersion int    `json:"expected_version"`
}

func (s *server) handlePutMemory(w http.ResponseWriter, r *http.Request) {
	var body putMemoryBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	doc, conflict, err := s.putMemory(r.Context(), r.PathValue("name"), body)
	if err != nil {
		writeTxErr(w, err)
		return
	}
	if conflict {
		writeJSON(w, http.StatusConflict, doc)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *server) putMemory(ctx context.Context, name string, body putMemoryBody) (versionedDoc, bool, error) {
	var doc versionedDoc
	conflict := false
	err := s.store.Tx(ctx, func(tx *sql.Tx) error {
		daemonID, err := daemonIDByName(ctx, tx, name)
		if err != nil {
			return err
		}
		current, err := latestMemory(ctx, tx, daemonID)
		if err != nil && !errors.Is(err, errNotFound) {
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

func latestSoul(ctx context.Context, q queryer, daemonID string) (versionedDoc, error) {
	return scanLatestDoc(q.QueryRowContext(ctx,
		`SELECT body, version FROM souls WHERE daemon_id = ? ORDER BY version DESC LIMIT 1`, daemonID))
}

func latestMemory(ctx context.Context, q queryer, daemonID string) (versionedDoc, error) {
	return scanLatestDoc(q.QueryRowContext(ctx,
		`SELECT body, version FROM memory WHERE daemon_id = ? ORDER BY version DESC LIMIT 1`, daemonID))
}

func scanLatestDoc(row *sql.Row) (versionedDoc, error) {
	var doc versionedDoc
	if err := row.Scan(&doc.Content, &doc.Version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return versionedDoc{}, errNotFound
		}
		return versionedDoc{}, err
	}
	return doc, nil
}
