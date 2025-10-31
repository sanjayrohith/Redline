package db

import (
	"context"
	"time"
)

// User is one row of the users table. PasswordHash is empty for users
// created via Create (test fixtures, API-key-only accounts) - such a user
// can never satisfy a password login check.
type User struct {
	ID           string
	Email        string
	PasswordHash string
	CreatedAt    time.Time
	UpdatedAt    time.Time
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
	err := r.pool.QueryRow(ctx,
		`INSERT INTO users (email) VALUES ($1)
		 RETURNING id::text, email, password_hash, created_at, updated_at`,
		email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, mapError(err)
	}
	return &u, nil
}

// CreateWithPassword inserts a new user with the given email and
// already-hashed password.
func (r *UserRepository) CreateWithPassword(ctx context.Context, email, passwordHash string) (*User, error) {
	var u User
	err := r.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1, $2)
		 RETURNING id::text, email, password_hash, created_at, updated_at`,
		email, passwordHash,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, mapError(err)
	}
	return &u, nil
}

// GetByID returns the user with the given id, or ErrNotFound.
func (r *UserRepository) GetByID(ctx context.Context, id string) (*User, error) {
	var u User
	err := r.pool.QueryRow(ctx,
		`SELECT id::text, email, password_hash, created_at, updated_at FROM users WHERE id = $1`,
		id,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, mapError(err)
	}
	return &u, nil
}

// GetByEmail returns the user with the given email, or ErrNotFound.
func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*User, error) {
	var u User
	err := r.pool.QueryRow(ctx,
		`SELECT id::text, email, password_hash, created_at, updated_at FROM users WHERE email = $1`,
		email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, mapError(err)
	}
	return &u, nil
}
