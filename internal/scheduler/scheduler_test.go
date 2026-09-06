package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/linell/aether/internal/store"
	"github.com/linell/aether/internal/store/storetest"
)

func seed(t *testing.T, st *store.Store, now time.Time) {
	t.Helper()
	storetest.Exec(t, st, `INSERT INTO hosts (id, name) VALUES ('h1', 'droplet')`)
	storetest.Exec(t, st, `INSERT INTO daemons (id, name, host_id, class, offline_policy) VALUES ('d1', 'bar', 'h1', 'anchored', 'queue')`)
	schedules := map[string]struct {
		policy string
		due    time.Time
	}{
		"s-queue": {"queue", now.Add(-time.Minute)},
		"s-skip":  {"skip", now.Add(-time.Hour)},
		"s-later": {"queue", now.Add(time.Hour)},
	}
	for id, s := range schedules {
		storetest.Exec(t, st,
			`INSERT INTO schedules (id, daemon_id, cron, tz, next_run_at, offline_policy, thread_id) VALUES (?, 'd1', '0 7 * * *', 'UTC', ?, ?, 't1')`,
			id, store.FormatTime(s.due), s.policy)
	}
}

func TestTickFiresDueAndMarksStale(t *testing.T) {
	st := storetest.Open(t)
	now := time.Date(2026, 9, 5, 8, 0, 0, 0, time.UTC)
	seed(t, st, now)

	res, err := Tick(context.Background(), st, now)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if res != (Result{Fired: 1, Skipped: 1}) {
		t.Fatalf("result = %+v, want 1 fired 1 skipped", res)
	}
	if n := storetest.Count(t, st, `SELECT COUNT(1) FROM outbox WHERE event_name = 'aether/schedule.fired'`); n != 1 {
		t.Errorf("fired events = %d, want 1", n)
	}
	if n := storetest.Count(t, st, `SELECT COUNT(1) FROM markers WHERE kind = ? AND ref_id = 's-skip'`, MarkerSkipped); n != 1 {
		t.Errorf("skip markers = %d, want 1", n)
	}
	if n := storetest.Count(t, st, `SELECT COUNT(1) FROM schedules WHERE next_run_at <= ?`, store.FormatTime(now)); n != 0 {
		t.Errorf("stale next_run_at rows = %d, want 0", n)
	}
	res, err = Tick(context.Background(), st, now)
	if err != nil || res != (Result{}) {
		t.Fatalf("second tick = %+v, %v; want nothing", res, err)
	}
}
