package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

var pragmas = []string{
	"PRAGMA journal_mode = WAL",
	"PRAGMA foreign_keys = ON",
	"PRAGMA busy_timeout = 5000",
	"PRAGMA synchronous = NORMAL",
}

type Store struct {
	db   *sql.DB
	wake chan struct{}
}

type Tx struct {
	*sql.Tx
	enqueued bool
}

type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)

	s := &Store{db: db, wake: make(chan struct{}, 1)}
	if err := s.init(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) init(ctx context.Context) error {
	for _, p := range pragmas {
		if _, err := s.db.ExecContext(ctx, p); err != nil {
			return fmt.Errorf("store: %s: %w", p, err)
		}
	}
	return s.migrate(ctx)
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) Tx(ctx context.Context, fn func(*Tx) error) error {
	inner, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin tx: %w", err)
	}
	tx := &Tx{Tx: inner}
	if err := fn(tx); err != nil {
		return errors.Join(err, rollback(inner))
	}
	if err := inner.Commit(); err != nil {
		return fmt.Errorf("store: commit tx: %w", err)
	}
	if tx.enqueued {
		s.notify()
	}
	return nil
}

func (s *Store) Wake() <-chan struct{} { return s.wake }

func (s *Store) notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func rollback(tx *sql.Tx) error {
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		return fmt.Errorf("store: rollback: %w", err)
	}
	return nil
}

func Query[T any](ctx context.Context, q DBTX, scan func(*sql.Rows) (T, error), query string, args ...any) ([]T, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
		)`); err != nil {
		return fmt.Errorf("store: create schema_migrations: %w", err)
	}
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("store: read migrations: %w", err)
	}
	for _, e := range entries {
		if err := s.applyOnce(ctx, e.Name()); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) applyOnce(ctx context.Context, name string) error {
	var applied int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM schema_migrations WHERE version = ?`, name,
	).Scan(&applied); err != nil {
		return fmt.Errorf("store: check migration %s: %w", name, err)
	}
	if applied > 0 {
		return nil
	}
	body, err := migrationsFS.ReadFile("migrations/" + name)
	if err != nil {
		return fmt.Errorf("store: read migration %s: %w", name, err)
	}
	return s.Tx(ctx, func(tx *Tx) error {
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("store: apply migration %s: %w", name, err)
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES (?)`, name)
		return err
	})
}

func (tx *Tx) EnqueueOutbox(ctx context.Context, eventName string, payload any) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("store: marshal outbox payload: %w", err)
	}
	id := NewID()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO outbox (id, event_name, payload) VALUES (?, ?, ?)`,
		id, eventName, string(data),
	); err != nil {
		return "", fmt.Errorf("store: insert outbox row: %w", err)
	}
	tx.enqueued = true
	return id, nil
}

type Publisher interface {
	Publish(ctx context.Context, id, name string, payload json.RawMessage) error
}

type outboxRow struct {
	id      string
	name    string
	payload json.RawMessage
}

func (s *Store) DrainOutbox(ctx context.Context, pub Publisher, limit int) (int, error) {
	pending, err := Query(ctx, s.db, scanOutboxRow,
		`SELECT id, event_name, payload FROM outbox
		 WHERE published_at IS NULL
		 ORDER BY created_at, id
		 LIMIT ?`, limit)
	if err != nil {
		return 0, fmt.Errorf("store: query outbox: %w", err)
	}
	published := 0
	var errs []error
	for _, r := range pending {
		if err := s.publishRow(ctx, pub, r); err != nil {
			errs = append(errs, err)
			continue
		}
		published++
	}
	return published, errors.Join(errs...)
}

func scanOutboxRow(rows *sql.Rows) (outboxRow, error) {
	var r outboxRow
	var payload string
	if err := rows.Scan(&r.id, &r.name, &payload); err != nil {
		return outboxRow{}, err
	}
	r.payload = json.RawMessage(payload)
	return r, nil
}

func (s *Store) publishRow(ctx context.Context, pub Publisher, r outboxRow) error {
	if err := pub.Publish(ctx, r.id, r.name, r.payload); err != nil {
		return errors.Join(
			fmt.Errorf("store: publish %s (%s): %w", r.id, r.name, err),
			s.exec(ctx, `UPDATE outbox SET attempts = attempts + 1 WHERE id = ?`, r.id),
		)
	}
	return s.exec(ctx,
		`UPDATE outbox
		 SET published_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), attempts = attempts + 1
		 WHERE id = ?`, r.id)
}

func (s *Store) exec(ctx context.Context, query string, args ...any) error {
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}
