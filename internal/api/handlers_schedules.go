package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/linell/aether/internal/scheduler"
	"github.com/linell/aether/internal/store"
)

type scheduleDoc struct {
	ID         string `json:"id"`
	Daemon     string `json:"daemon"`
	Thread     string `json:"thread"`
	Cron       string `json:"cron"`
	TZ         string `json:"tz"`
	NextRunAt  string `json:"next_run_at"`
	Policy     string `json:"policy"`
	TTLSeconds *int64 `json:"ttl_seconds"`
}

func (s *server) handleListSchedules(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	daemonID, err := daemonIDByName(r.Context(), s.store.DB(), name)
	if err != nil {
		writeTxErr(w, err)
		return
	}
	schedules, err := listSchedules(r.Context(), s.store.DB(), daemonID, name)
	if err != nil {
		writeTxErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, schedules)
}

func listSchedules(ctx context.Context, db *sql.DB, daemonID, daemonName string) ([]scheduleDoc, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, thread_id, cron, tz, next_run_at, offline_policy, ttl_seconds
		 FROM schedules WHERE daemon_id = ?`, daemonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []scheduleDoc{}
	for rows.Next() {
		doc, err := scanSchedule(rows, daemonName)
		if err != nil {
			return nil, err
		}
		out = append(out, doc)
	}
	return out, rows.Err()
}

func scanSchedule(rows *sql.Rows, daemonName string) (scheduleDoc, error) {
	doc := scheduleDoc{Daemon: daemonName}
	var ttl sql.NullInt64
	err := rows.Scan(&doc.ID, &doc.Thread, &doc.Cron, &doc.TZ, &doc.NextRunAt, &doc.Policy, &ttl)
	if ttl.Valid {
		doc.TTLSeconds = &ttl.Int64
	}
	return doc, err
}

type schedulePutBody struct {
	Thread     string `json:"thread"`
	Cron       string `json:"cron"`
	TZ         string `json:"tz"`
	Policy     string `json:"policy"`
	TTLSeconds *int64 `json:"ttl_seconds"`
}

func (s *server) handlePutSchedule(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	id := r.PathValue("id")
	var body schedulePutBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	nextRunAt, err := validateSchedule(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	doc, err := s.upsertSchedule(r.Context(), name, id, body, nextRunAt)
	if err != nil {
		writeTxErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func validateSchedule(body schedulePutBody) (string, error) {
	sched, err := scheduler.Parse(body.Cron, body.TZ)
	if err != nil {
		return "", err
	}
	if err := validatePolicy(body.Policy, body.TTLSeconds); err != nil {
		return "", err
	}
	return store.FormatTime(sched.Next(time.Now())), nil
}

func validatePolicy(policy string, ttl *int64) error {
	switch policy {
	case "queue", "skip":
		return nil
	case "ttl":
		if ttl == nil || *ttl <= 0 {
			return errors.New("ttl policy requires ttl_seconds > 0")
		}
		return nil
	default:
		return fmt.Errorf("invalid policy: %s", policy)
	}
}

func (s *server) upsertSchedule(ctx context.Context, name, id string, body schedulePutBody, nextRunAt string) (scheduleDoc, error) {
	var doc scheduleDoc
	err := s.store.Tx(ctx, func(tx *sql.Tx) error {
		daemonID, class, err := daemonIDAndClass(ctx, tx, name)
		if err != nil {
			return err
		}
		if class != "anchored" {
			return errNotAnchored
		}
		if err := checkScheduleWriteOwnership(ctx, tx, id, daemonID); err != nil {
			return err
		}
		if err := execUpsertSchedule(ctx, tx, id, daemonID, body, nextRunAt); err != nil {
			return err
		}
		doc = scheduleDoc{
			ID: id, Daemon: name, Thread: body.Thread, Cron: body.Cron, TZ: body.TZ,
			NextRunAt: nextRunAt, Policy: body.Policy, TTLSeconds: body.TTLSeconds,
		}
		return nil
	})
	return doc, err
}

func checkScheduleWriteOwnership(ctx context.Context, tx *sql.Tx, id, daemonID string) error {
	owner, found, err := scheduleOwner(ctx, tx, id)
	if err != nil {
		return err
	}
	if found && owner != daemonID {
		return errNotFound
	}
	return nil
}

func execUpsertSchedule(ctx context.Context, tx *sql.Tx, id, daemonID string, body schedulePutBody, nextRunAt string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO schedules (id, daemon_id, thread_id, cron, tz, next_run_at, offline_policy, ttl_seconds)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			daemon_id = excluded.daemon_id,
			thread_id = excluded.thread_id,
			cron = excluded.cron,
			tz = excluded.tz,
			next_run_at = excluded.next_run_at,
			offline_policy = excluded.offline_policy,
			ttl_seconds = excluded.ttl_seconds`,
		id, daemonID, body.Thread, body.Cron, body.TZ, nextRunAt, body.Policy, body.TTLSeconds)
	return err
}

func (s *server) handleDeleteSchedule(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	id := r.PathValue("id")
	err := s.store.Tx(r.Context(), func(tx *sql.Tx) error {
		daemonID, err := daemonIDByName(r.Context(), tx, name)
		if err != nil {
			return err
		}
		return deleteOwnedSchedule(r.Context(), tx, id, daemonID)
	})
	if err != nil {
		writeTxErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func deleteOwnedSchedule(ctx context.Context, tx *sql.Tx, id, daemonID string) error {
	owner, found, err := scheduleOwner(ctx, tx, id)
	if err != nil {
		return err
	}
	if !found || owner != daemonID {
		return errNotFound
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM schedules WHERE id = ?`, id)
	return err
}
