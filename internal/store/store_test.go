package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(context.Background(), filepath.Join(dir, "aether.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpenAppliesMigrationsAndWAL(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	var mode string
	if err := s.DB().QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}

	tables := []string{
		"hosts", "daemons", "souls", "memory", "threads", "messages",
		"approvals", "schedules", "operations", "outbox", "schema_migrations",
	}
	for _, tbl := range tables {
		var name string
		err := s.DB().QueryRowContext(ctx,
			`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, tbl,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q missing after migration: %v", tbl, err)
		}
	}

	var migrationCount int
	if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(1) FROM schema_migrations`).Scan(&migrationCount); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if migrationCount == 0 {
		t.Error("expected at least one recorded migration")
	}
}

func TestEnqueueOutboxRollbackLeavesNoRow(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	sentinelErr := errors.New("boom")
	var id string
	err := s.Tx(ctx, func(tx *sql.Tx) error {
		var err error
		id, err = s.EnqueueOutbox(ctx, tx, "test/event.happened", map[string]string{"a": "b"})
		if err != nil {
			return err
		}
		return sentinelErr
	})
	if !errors.Is(err, sentinelErr) {
		t.Fatalf("Tx error = %v, want sentinel", err)
	}

	var count int
	if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(1) FROM outbox WHERE id = ?`, id).Scan(&count); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if count != 0 {
		t.Errorf("outbox row %q exists after rollback, want none", id)
	}
}

func TestEnqueueOutboxCommitPersistsRow(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	var id string
	err := s.Tx(ctx, func(tx *sql.Tx) error {
		var err error
		id, err = s.EnqueueOutbox(ctx, tx, "test/event.happened", map[string]string{"a": "b"})
		return err
	})
	if err != nil {
		t.Fatalf("Tx: %v", err)
	}

	var name string
	if err := s.DB().QueryRowContext(ctx, `SELECT event_name FROM outbox WHERE id = ?`, id).Scan(&name); err != nil {
		t.Fatalf("query outbox row: %v", err)
	}
	if name != "test/event.happened" {
		t.Errorf("event_name = %q, want test/event.happened", name)
	}
}

type fakePublisher struct {
	mu       sync.Mutex
	fail     map[string]bool
	attempts map[string]int
}

func newFakePublisher() *fakePublisher {
	return &fakePublisher{fail: map[string]bool{}, attempts: map[string]int{}}
}

func (f *fakePublisher) Publish(_ context.Context, id, _ string, _ json.RawMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts[id]++
	if f.fail[id] {
		return errors.New("publish failed")
	}
	return nil
}

func (f *fakePublisher) count(id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.attempts[id]
}

func enqueue(t *testing.T, s *Store, eventName string) string {
	t.Helper()
	var id string
	err := s.Tx(context.Background(), func(tx *sql.Tx) error {
		var err error
		id, err = s.EnqueueOutbox(context.Background(), tx, eventName, map[string]string{"k": "v"})
		return err
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	return id
}

func TestDrainOutboxPublishesOnceAndIsIdempotent(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	id := enqueue(t, s, "test/event.happened")

	pub := newFakePublisher()

	n, err := s.DrainOutbox(ctx, pub, 10)
	if err != nil {
		t.Fatalf("DrainOutbox: %v", err)
	}
	if n != 1 {
		t.Fatalf("published = %d, want 1", n)
	}
	if got := pub.count(id); got != 1 {
		t.Fatalf("publish attempts for %s = %d, want 1", id, got)
	}

	var publishedAt sql.NullString
	if err := s.DB().QueryRowContext(ctx, `SELECT published_at FROM outbox WHERE id = ?`, id).Scan(&publishedAt); err != nil {
		t.Fatalf("query published_at: %v", err)
	}
	if !publishedAt.Valid {
		t.Fatal("published_at not set after successful publish")
	}
	n, err = s.DrainOutbox(ctx, pub, 10)
	if err != nil {
		t.Fatalf("second DrainOutbox: %v", err)
	}
	if n != 0 {
		t.Fatalf("second drain published = %d, want 0", n)
	}
	if got := pub.count(id); got != 1 {
		t.Fatalf("publish attempts for %s after second drain = %d, want still 1", id, got)
	}
}

func TestDrainOutboxFailedPublishIncrementsAttempts(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	id := enqueue(t, s, "test/event.failed")

	pub := newFakePublisher()
	pub.fail[id] = true

	n, err := s.DrainOutbox(ctx, pub, 10)
	if err == nil {
		t.Fatal("expected error from failed publish")
	}
	if n != 0 {
		t.Fatalf("published = %d, want 0", n)
	}

	var attempts int
	var publishedAt sql.NullString
	if err := s.DB().QueryRowContext(ctx, `SELECT attempts, published_at FROM outbox WHERE id = ?`, id).Scan(&attempts, &publishedAt); err != nil {
		t.Fatalf("query outbox row: %v", err)
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1", attempts)
	}
	if publishedAt.Valid {
		t.Error("published_at set despite failed publish")
	}
	pub.fail[id] = false
	n, err = s.DrainOutbox(ctx, pub, 10)
	if err != nil {
		t.Fatalf("retry DrainOutbox: %v", err)
	}
	if n != 1 {
		t.Fatalf("retry published = %d, want 1", n)
	}
}

func TestEnqueueWakesDrainAfterCommit(t *testing.T) {
	s := openTestStore(t)
	err := s.Tx(context.Background(), func(tx *sql.Tx) error {
		_, err := s.EnqueueOutbox(context.Background(), tx, "test/event.happened", nil)
		if err != nil {
			return err
		}
		select {
		case <-s.Wake():
			return errors.New("woke before commit")
		default:
			return nil
		}
	})
	if err != nil {
		t.Fatalf("Tx: %v", err)
	}
	select {
	case <-s.Wake():
	default:
		t.Fatal("no wake after commit")
	}
}
