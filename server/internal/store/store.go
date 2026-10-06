// Package store holds the control-plane models and every SQL query against
// the control database. Each query is a small method that owns its SQL.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

// DBTX is satisfied by both the pool and a transaction.
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Store struct {
	Pool *pgxpool.Pool
	q    DBTX
}

func New(pool *pgxpool.Pool) *Store { return &Store{Pool: pool, q: pool} }

// Tx runs fn inside a transaction with a Store bound to it.
func (s *Store) Tx(ctx context.Context, fn func(tx *Store) error) error {
	return pgx.BeginFunc(ctx, s.Pool, func(t pgx.Tx) error {
		return fn(&Store{Pool: s.Pool, q: t})
	})
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// IsUniqueViolation reports a unique constraint error.
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func one[T any](ctx context.Context, q DBTX, sql string, args ...any) (*T, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	v, err := pgx.CollectExactlyOneRow(rows, pgx.RowToAddrOfStructByNameLax[T])
	if err != nil {
		return nil, notFound(err)
	}
	return v, nil
}

func many[T any](ctx context.Context, q DBTX, sql string, args ...any) ([]T, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByNameLax[T])
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []T{}
	}
	return out, nil
}

func exec(ctx context.Context, q DBTX, sql string, args ...any) (int64, error) {
	tag, err := q.Exec(ctx, sql, args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// Now is the database clock, so tests and hosts agree on time.
func (s *Store) Now(ctx context.Context) (time.Time, error) {
	var t time.Time
	err := s.q.QueryRow(ctx, `select now()`).Scan(&t)
	return t, err
}

// XactLock takes a transaction-scoped advisory lock. Call it inside Tx.
func (s *Store) XactLock(ctx context.Context, key int64) error {
	_, err := s.q.Exec(ctx, `select pg_advisory_xact_lock($1)`, key)
	return err
}
