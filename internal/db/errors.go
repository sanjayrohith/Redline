package db

import (
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrNotFound is returned when a lookup matches no row.
var ErrNotFound = errors.New("db: not found")

// ErrConflict is returned when a write violates a uniqueness constraint.
var ErrConflict = errors.New("db: conflict")

const uniqueViolationCode = "23505"

func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
		return ErrConflict
	}

	return err
}
