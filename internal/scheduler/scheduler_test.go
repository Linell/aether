package scheduler

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/linell/aether/internal/store"
)

func seed(t *testing.T, st *store.Store, now time.Time) {
	t.Helper()
	ctx := context.Background()
	stmts := []string{
		`INSERT INTO hosts (id, name) VALUES ('h1', 'droplet')`,
		`INSERT INTO daemons (id, name, host_id, class, offline_policy) VALUES ('d1', 'bar', 'h1', 'anchored', 'queue')`,
		`INSERT INTO schedules (id, daemon_id, cron, tz, next_run_at, offline_policy, thread_id) VALUES ('s-queue', 'd1', '0 7 * * *', 'UTC', '` + store.FormatTime(now.Add(-time.Minute)) + `', 'queue', 't1')`,
		`INSERT INTO schedules (id, daemon_id, cron, tz, next_run_at, offline_policy, thread_id) VALUES ('s-skip', 'd1', '0 7 * * *', 'UTC', '` + store.FormatTime(now.Add(-time.Hour)) + `', 'skip', 't1')`,
		`INSERT INTO schedules (id, daemon_id, cron, tz, next_run_at, offline_policy, thread_id) VALUES ('s-later', 'd1', '0 7 * * *', 'UTC', '` + store.FormatTime(now.Add(time.Hour)) + `', 'queue', 't1')`,
	}
	for _, q := range stmts {
		if _, err := st.DB().ExecContext(ctx, q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
}

func count(t *testing.T, st *store.Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := st.DB().QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func TestTickFiresDueAndMarksStale(t *testing.T) {
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Date(2026, 9, 5, 7, 0, 0, 0, time.UTC).Add(time.Hour)
	seed(t, st, now)

	res, err := Tick(context.Background(), st, now)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if res != (Result{Fired: 1, Skipped: 1}) {
		t.Fatalf("result = %+v, want 1 fired 1 skipped", res)
	}
	if n := count(t, st, `SELECT COUNT(1) FROM outbox WHERE event_name = 'aether/schedule.fired'`); n != 1 {
		t.Errorf("fired events = %d, want 1", n)
	}
	if n := count(t, st, `SELECT COUNT(1) FROM markers WHERE kind = ? AND ref_id = 's-skip'`, MarkerSkipped); n != 1 {
		t.Errorf("skip markers = %d, want 1", n)
	}
	if n := count(t, st, `SELECT COUNT(1) FROM schedules WHERE next_run_at <= ?`, store.FormatTime(now)); n != 0 {
		t.Errorf("stale next_run_at rows = %d, want 0", n)
	}
	res, err = Tick(context.Background(), st, now)
	if err != nil || res != (Result{}) {
		t.Fatalf("second tick = %+v, %v; want nothing", res, err)
	}
}
