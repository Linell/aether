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
	schedules, err := s.listSchedules(r.Context(), r.PathValue("name"))
	respond(w, http.StatusOK, schedules, err)
}

func (s *server) listSchedules(ctx context.Context, daemonName string) ([]scheduleDoc, error) {
	daemonID, err := daemonIDByName(ctx, s.store.DB(), daemonName)
	if err != nil {
		return nil, err
	}
	scan := func(rows *sql.Rows) (scheduleDoc, error) { return scanSchedule(rows, daemonName) }
	return store.Query(ctx, s.store.DB(), scan,
		`SELECT id, thread_id, cron, tz, next_run_at, offline_policy, ttl_seconds
		 FROM schedules WHERE daemon_id = ?`, daemonID)
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
	var body schedulePutBody
	if !decodeBody(w, r, &body) {
		return
	}
	nextRunAt, err := validateSchedule(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	doc, err := s.upsertSchedule(r.Context(), r.PathValue("name"), r.PathValue("id"), body, nextRunAt)
	respond(w, http.StatusOK, doc, err)
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
	doc := scheduleDoc{
		ID: id, Daemon: name, Thread: body.Thread, Cron: body.Cron, TZ: body.TZ,
		NextRunAt: nextRunAt, Policy: body.Policy, TTLSeconds: body.TTLSeconds,
	}
	err := s.store.Tx(ctx, func(tx *store.Tx) error {
		daemonID, err := anchoredDaemonID(ctx, tx, name)
		if err != nil {
			return err
		}
		if err := checkScheduleOwnership(ctx, tx, id, daemonID); err != nil {
			return err
		}
		return execUpsertSchedule(ctx, tx, daemonID, doc)
	})
	return doc, err
}

func checkScheduleOwnership(ctx context.Context, q store.DBTX, id, daemonID string) error {
	var foreign int
	err := q.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM schedules WHERE id = ? AND daemon_id != ?`, id, daemonID,
	).Scan(&foreign)
	if err == nil && foreign > 0 {
		return errNotFound
	}
	return err
}

func execUpsertSchedule(ctx context.Context, tx store.DBTX, daemonID string, doc scheduleDoc) error {
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
		doc.ID, daemonID, doc.Thread, doc.Cron, doc.TZ, doc.NextRunAt, doc.Policy, doc.TTLSeconds)
	return err
}

func (s *server) handleDeleteSchedule(w http.ResponseWriter, r *http.Request) {
	err := s.store.Tx(r.Context(), func(tx *store.Tx) error {
		daemonID, err := daemonIDByName(r.Context(), tx, r.PathValue("name"))
		if err != nil {
			return err
		}
		return deleteOwnedSchedule(r.Context(), tx, r.PathValue("id"), daemonID)
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func deleteOwnedSchedule(ctx context.Context, tx store.DBTX, id, daemonID string) error {
	out, err := tx.ExecContext(ctx, `DELETE FROM schedules WHERE id = ? AND daemon_id = ?`, id, daemonID)
	if err != nil {
		return err
	}
	n, err := out.RowsAffected()
	if err == nil && n == 0 {
		return errNotFound
	}
	return err
}
