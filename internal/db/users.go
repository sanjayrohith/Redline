package db

import (
	"context"
	"time"
)

// User is one row of the users table. PasswordHash is empty for users
// created via Create (test fixtures, API-key-only accounts) - such a user
// can never satisfy a password login check.
type User struct {
	ID            string
	Email         string
	PasswordHash  string
	TOSAcceptedAt *time.Time
	SuspendedAt   *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Suspended reports whether the account is currently suspended.
func (u User) Suspended() bool {
	return u.SuspendedAt != nil
}

const userColumns = `id::text, email, password_hash, tos_accepted_at, suspended_at, created_at, updated_at`

func scanUser(row interface{ Scan(...any) error }, u *User) error {
	return row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.TOSAcceptedAt, &u.SuspendedAt, &u.CreatedAt, &u.UpdatedAt)
}

// UserRepository performs typed CRUD against the users table.
type UserRepository struct {
	pool *Pool
}

// NewUserRepository returns a UserRepository bound to pool.
func NewUserRepository(pool *Pool) *UserRepository {
	return &UserRepository{pool: pool}
}

// Create inserts a new user with the given email and no password hash.
func (r *UserRepository) Create(ctx context.Context, email string) (*User, error) {
	var u User
	err := scanUser(r.pool.QueryRow(ctx,
		`INSERT INTO users (email) VALUES ($1) RETURNING `+userColumns,
		email,
	), &u)
	if err != nil {
		return nil, mapError(err)
	}
	return &u, nil
}

// CreateWithPassword inserts a new user with the given email and
// already-hashed password.
func (r *UserRepository) CreateWithPassword(ctx context.Context, email, passwordHash string) (*User, error) {
	var u User
	err := scanUser(r.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1, $2) RETURNING `+userColumns,
		email, passwordHash,
	), &u)
	if err != nil {
		return nil, mapError(err)
	}
	return &u, nil
}

// GetByID returns the user with the given id, or ErrNotFound.
func (r *UserRepository) GetByID(ctx context.Context, id string) (*User, error) {
	var u User
	err := scanUser(r.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id), &u)
	if err != nil {
		return nil, mapError(err)
	}
	return &u, nil
}

// GetByEmail returns the user with the given email, or ErrNotFound.
func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*User, error) {
	var u User
	err := scanUser(r.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE email = $1`, email), &u)
	if err != nil {
		return nil, mapError(err)
	}
	return &u, nil
}

// AcceptTOS records that the user accepted the current terms of service,
// now.
func (r *UserRepository) AcceptTOS(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE users SET tos_accepted_at = now(), updated_at = now() WHERE id = $1`, id)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Suspend marks the user suspended, now. It is idempotent: suspending an
// already-suspended user leaves its original suspension time untouched.
func (r *UserRepository) Suspend(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE users SET suspended_at = COALESCE(suspended_at, now()), updated_at = now() WHERE id = $1`, id)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
