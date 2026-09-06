package api

import (
	"context"
	"database/sql"
	"errors"
)

var (
	errNotFound    = errors.New("not found")
	errNotAnchored = errors.New("daemon is not anchored")
)

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func daemonIDByName(ctx context.Context, q queryer, name string) (string, error) {
	var id string
	err := q.QueryRowContext(ctx, `SELECT id FROM daemons WHERE name = ?`, name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errNotFound
	}
	return id, err
}

func daemonIDAndClass(ctx context.Context, q queryer, name string) (string, string, error) {
	var id, class string
	err := q.QueryRowContext(ctx, `SELECT id, class FROM daemons WHERE name = ?`, name).Scan(&id, &class)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", errNotFound
	}
	return id, class, err
}

func threadDaemonName(ctx context.Context, q queryer, threadID string) (string, error) {
	var name string
	err := q.QueryRowContext(ctx,
		`SELECT daemons.name FROM threads JOIN daemons ON daemons.id = threads.daemon_id WHERE threads.id = ?`,
		threadID,
	).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errNotFound
	}
	return name, err
}

func hostIDByName(ctx context.Context, q queryer, name string) (string, error) {
	var id string
	err := q.QueryRowContext(ctx, `SELECT id FROM hosts WHERE name = ?`, name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errNotFound
	}
	return id, err
}

func scheduleOwner(ctx context.Context, q queryer, id string) (string, bool, error) {
	var owner string
	err := q.QueryRowContext(ctx, `SELECT daemon_id FROM schedules WHERE id = ?`, id).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return owner, true, nil
}
