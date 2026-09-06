package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/linell/aether/internal/store"
	"github.com/linell/aether/internal/store/storetest"
)

func TestOpenAppliesMigrationsAndWAL(t *testing.T) {
	s := storetest.Open(t)

	var mode string
	if err := s.DB().QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
	tables := []string{
		"hosts", "daemons", "souls", "memory", "threads", "messages",
		"approvals", "schedules", "operations", "outbox", "markers", "schema_migrations",
	}
	for _, tbl := range tables {
		if n := storetest.Count(t, s, `SELECT COUNT(1) FROM sqlite_master WHERE type = 'table' AND name = ?`, tbl); n != 1 {
			t.Errorf("table %q missing after migration", tbl)
		}
	}
	if storetest.Count(t, s, `SELECT COUNT(1) FROM schema_migrations`) == 0 {
		t.Error("expected at least one recorded migration")
	}
}

func TestEnqueueOutboxRollbackLeavesNoRow(t *testing.T) {
	s := storetest.Open(t)
	ctx := context.Background()

	sentinelErr := errors.New("boom")
	var id string
	err := s.Tx(ctx, func(tx *store.Tx) error {
		var err error
		id, err = tx.EnqueueOutbox(ctx, "test/event.happened", map[string]string{"a": "b"})
		if err != nil {
			return err
		}
		return sentinelErr
	})
	if !errors.Is(err, sentinelErr) {
		t.Fatalf("Tx error = %v, want sentinel", err)
	}
	if n := storetest.Count(t, s, `SELECT COUNT(1) FROM outbox WHERE id = ?`, id); n != 0 {
		t.Errorf("outbox row %q exists after rollback, want none", id)
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

func enqueue(t *testing.T, s *store.Store, eventName string) string {
	t.Helper()
	var id string
	err := s.Tx(context.Background(), func(tx *store.Tx) error {
		var err error
		id, err = tx.EnqueueOutbox(context.Background(), eventName, map[string]string{"k": "v"})
		return err
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	return id
}

func drain(t *testing.T, s *store.Store, pub *fakePublisher, want int) error {
	t.Helper()
	n, err := s.DrainOutbox(context.Background(), pub, 10)
	if n != want {
		t.Fatalf("published = %d, want %d", n, want)
	}
	return err
}

func publishedAt(t *testing.T, s *store.Store, id string) sql.NullString {
	t.Helper()
	var at sql.NullString
	if err := s.DB().QueryRow(`SELECT published_at FROM outbox WHERE id = ?`, id).Scan(&at); err != nil {
		t.Fatalf("query published_at: %v", err)
	}
	return at
}

func TestDrainOutboxPublishesOnceAndIsIdempotent(t *testing.T) {
	s := storetest.Open(t)
	id := enqueue(t, s, "test/event.happened")
	pub := newFakePublisher()

	if err := drain(t, s, pub, 1); err != nil {
		t.Fatalf("DrainOutbox: %v", err)
	}
	if !publishedAt(t, s, id).Valid {
		t.Fatal("published_at not set after successful publish")
	}
	if err := drain(t, s, pub, 0); err != nil {
		t.Fatalf("second DrainOutbox: %v", err)
	}
	if got := pub.count(id); got != 1 {
		t.Fatalf("publish attempts for %s = %d, want 1", id, got)
	}
}

func TestDrainOutboxFailedPublishIncrementsAttempts(t *testing.T) {
	s := storetest.Open(t)
	id := enqueue(t, s, "test/event.failed")
	pub := newFakePublisher()
	pub.fail[id] = true

	if err := drain(t, s, pub, 0); err == nil {
		t.Fatal("expected error from failed publish")
	}
	if n := storetest.Count(t, s, `SELECT attempts FROM outbox WHERE id = ?`, id); n != 1 {
		t.Errorf("attempts = %d, want 1", n)
	}
	if publishedAt(t, s, id).Valid {
		t.Error("published_at set despite failed publish")
	}
	pub.fail[id] = false
	if err := drain(t, s, pub, 1); err != nil {
		t.Fatalf("retry DrainOutbox: %v", err)
	}
}

func TestEnqueueWakesDrainAfterCommit(t *testing.T) {
	s := storetest.Open(t)
	err := s.Tx(context.Background(), func(tx *store.Tx) error {
		if _, err := tx.EnqueueOutbox(context.Background(), "test/event.happened", nil); err != nil {
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
