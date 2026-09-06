package api

import (
	"context"
	"database/sql"
	"errors"

	"github.com/linell/aether/internal/store"
)

var (
	errNotFound    = errors.New("not found")
	errNotAnchored = errors.New("daemon is not anchored")
)

func lookup(ctx context.Context, q store.DBTX, query string, arg any, dest ...any) error {
	err := q.QueryRowContext(ctx, query, arg).Scan(dest...)
	if errors.Is(err, sql.ErrNoRows) {
		return errNotFound
	}
	return err
}

func scanString(rows *sql.Rows) (string, error) {
	var s string
	err := rows.Scan(&s)
	return s, err
}

func inserted(out sql.Result) (bool, error) {
	n, err := out.RowsAffected()
	return n == 1, err
}

func withID(id string) string {
	if id != "" {
		return id
	}
	return store.NewID()
}

func daemonIDByName(ctx context.Context, q store.DBTX, name string) (string, error) {
	var id string
	err := lookup(ctx, q, `SELECT id FROM daemons WHERE name = ?`, name, &id)
	return id, err
}

func anchoredDaemonID(ctx context.Context, q store.DBTX, name string) (string, error) {
	var id, class string
	if err := lookup(ctx, q, `SELECT id, class FROM daemons WHERE name = ?`, name, &id, &class); err != nil {
		return "", err
	}
	if class != "anchored" {
		return "", errNotAnchored
	}
	return id, nil
}

func threadDaemonName(ctx context.Context, q store.DBTX, threadID string) (string, error) {
	var name string
	err := lookup(ctx, q,
		`SELECT daemons.name FROM threads JOIN daemons ON daemons.id = threads.daemon_id WHERE threads.id = ?`,
		threadID, &name)
	return name, err
}

func hostIDByName(ctx context.Context, q store.DBTX, name string) (string, error) {
	var id string
	err := lookup(ctx, q, `SELECT id FROM hosts WHERE name = ?`, name, &id)
	return id, err
}
