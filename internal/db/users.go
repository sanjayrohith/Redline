package db

import (
	"context"
	"time"
)

// User is one row of the users table.
type User struct {
	ID        string
	Email     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// UserRepository performs typed CRUD against the users table.
type UserRepository struct {
	pool *Pool
}

// NewUserRepository returns a UserRepository bound to pool.
func NewUserRepository(pool *Pool) *UserRepository {
	return &UserRepository{pool: pool}
}

// Create inserts a new user with the given email.
func (r *UserRepository) Create(ctx context.Context, email string) (*User, error) {
	var u User
	err := r.pool.QueryRow(ctx,
		`INSERT INTO users (email) VALUES ($1)
		 RETURNING id::text, email, created_at, updated_at`,
		email,
	).Scan(&u.ID, &u.Email, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, mapError(err)
	}
	return &u, nil
}

// GetByID returns the user with the given id, or ErrNotFound.
func (r *UserRepository) GetByID(ctx context.Context, id string) (*User, error) {
	var u User
	err := r.pool.QueryRow(ctx,
		`SELECT id::text, email, created_at, updated_at FROM users WHERE id = $1`,
		id,
	).Scan(&u.ID, &u.Email, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, mapError(err)
	}
	return &u, nil
}

// GetByEmail returns the user with the given email, or ErrNotFound.
func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*User, error) {
	var u User
	err := r.pool.QueryRow(ctx,
		`SELECT id::text, email, created_at, updated_at FROM users WHERE email = $1`,
		email,
	).Scan(&u.ID, &u.Email, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, mapError(err)
	}
	return &u, nil
}
