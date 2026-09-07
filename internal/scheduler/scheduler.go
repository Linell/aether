package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/linell/aether/internal/contract"
	"github.com/linell/aether/internal/store"
	"github.com/robfig/cron/v3"
)

const (
	SkipGrace     = 5 * time.Minute
	MarkerSkipped = "schedule.skipped"
)

type Result struct {
	Fired   int `json:"fired"`
	Skipped int `json:"skipped"`
}

type row struct {
	id, daemonID, daemon, thread, cron, tz, policy, prompt string
	ttl                                                    sql.NullInt64
	due                                                    time.Time
}

func Tick(ctx context.Context, st *store.Store, now time.Time) (Result, error) {
	rows, err := store.Query(ctx, st.DB(), scanRow,
		`SELECT s.id, s.daemon_id, d.name, s.thread_id, s.cron, s.tz, s.offline_policy, s.ttl_seconds, s.prompt, s.next_run_at
		 FROM schedules s JOIN daemons d ON d.id = s.daemon_id
		 WHERE s.enabled = 1 AND s.next_run_at <= ?
		 ORDER BY s.next_run_at, s.id`, store.FormatTime(now))
	if err != nil {
		return Result{}, fmt.Errorf("scheduler: query due: %w", err)
	}
	var res Result
	var errs []error
	for _, r := range rows {
		errs = append(errs, st.Tx(ctx, func(tx *store.Tx) error {
			return handle(ctx, tx, r, now, &res)
		}))
	}
	return res, errors.Join(errs...)
}

func scanRow(rows *sql.Rows) (row, error) {
	var r row
	var due string
	if err := rows.Scan(&r.id, &r.daemonID, &r.daemon, &r.thread, &r.cron, &r.tz, &r.policy, &r.ttl, &r.prompt, &due); err != nil {
		return row{}, err
	}
	t, err := store.ParseTime(due)
	if err != nil {
		return row{}, fmt.Errorf("parse next_run_at for %s: %w", r.id, err)
	}
	r.due = t
	return r, nil
}

func handle(ctx context.Context, tx *store.Tx, r row, now time.Time, res *Result) error {
	sched, err := Parse(r.cron, r.tz)
	if err != nil {
		return fmt.Errorf("scheduler: %s: %w", r.id, err)
	}
	advanced, err := advance(ctx, tx, r, sched.Next(now))
	if err != nil || !advanced {
		return err
	}
	deadline := deadlineFor(r, sched)
	if now.After(deadline) {
		res.Skipped++
		return markSkipped(ctx, tx, r, deadline)
	}
	res.Fired++
	return fire(ctx, tx, r, deadline)
}

func Parse(expr, tz string) (cron.Schedule, error) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("invalid tz %q: %w", tz, err)
	}
	sched, err := cron.ParseStandard(expr)
	if err != nil {
		return nil, fmt.Errorf("invalid cron %q: %w", expr, err)
	}
	return inLocation{sched, loc}, nil
}

type inLocation struct {
	cron.Schedule
	loc *time.Location
}

func (s inLocation) Next(t time.Time) time.Time { return s.Schedule.Next(t.In(s.loc)).UTC() }

func advance(ctx context.Context, tx *store.Tx, r row, next time.Time) (bool, error) {
	out, err := tx.ExecContext(ctx,
		`UPDATE schedules SET next_run_at = ? WHERE id = ? AND next_run_at = ?`,
		store.FormatTime(next), r.id, store.FormatTime(r.due))
	if err != nil {
		return false, fmt.Errorf("scheduler: advance %s: %w", r.id, err)
	}
	n, err := out.RowsAffected()
	return n == 1, err
}

func deadlineFor(r row, sched cron.Schedule) time.Time {
	switch r.policy {
	case "skip":
		return r.due.Add(SkipGrace)
	case "ttl":
		return r.due.Add(time.Duration(r.ttl.Int64) * time.Second)
	default:
		return sched.Next(r.due)
	}
}

func fire(ctx context.Context, tx *store.Tx, r row, deadline time.Time) error {
	_, err := tx.EnqueueOutbox(ctx, contract.EventScheduleFired, contract.ScheduleFiredPayload{
		Daemon:     r.daemon,
		Thread:     r.thread,
		Schedule:   r.id,
		DueAt:      r.due,
		DeadlineAt: deadline,
		Prompt:     r.prompt,
	})
	return err
}

func markSkipped(ctx context.Context, tx *store.Tx, r row, deadline time.Time) error {
	detail := fmt.Sprintf(`{"policy":%q,"due_at":%q,"deadline_at":%q}`,
		r.policy, store.FormatTime(r.due), store.FormatTime(deadline))
	_, err := tx.ExecContext(ctx,
		`INSERT INTO markers (id, kind, daemon_id, thread_id, ref_id, detail) VALUES (?, ?, ?, ?, ?, ?)`,
		store.NewID(), MarkerSkipped, r.daemonID, r.thread, r.id, detail)
	if err != nil {
		return fmt.Errorf("scheduler: mark skipped %s: %w", r.id, err)
	}
	return nil
}
