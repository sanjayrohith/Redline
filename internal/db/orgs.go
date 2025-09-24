package db

import (
	"context"
	"time"
)

// Org is one row of the orgs table.
type Org struct {
	ID        string
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// OrgRepository performs typed CRUD against the orgs table.
type OrgRepository struct {
	pool *Pool
}

// NewOrgRepository returns an OrgRepository bound to pool.
func NewOrgRepository(pool *Pool) *OrgRepository {
	return &OrgRepository{pool: pool}
}

// Create inserts a new org with the given name.
func (r *OrgRepository) Create(ctx context.Context, name string) (*Org, error) {
	var o Org
	err := r.pool.QueryRow(ctx,
		`INSERT INTO orgs (name) VALUES ($1)
		 RETURNING id::text, name, created_at, updated_at`,
		name,
	).Scan(&o.ID, &o.Name, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return nil, mapError(err)
	}
	return &o, nil
}

// GetByID returns the org with the given id, or ErrNotFound.
func (r *OrgRepository) GetByID(ctx context.Context, id string) (*Org, error) {
	var o Org
	err := r.pool.QueryRow(ctx,
		`SELECT id::text, name, created_at, updated_at FROM orgs WHERE id = $1`,
		id,
	).Scan(&o.ID, &o.Name, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return nil, mapError(err)
	}
	return &o, nil
}

// List returns every org, ordered by creation time.
func (r *OrgRepository) List(ctx context.Context) ([]Org, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id::text, name, created_at, updated_at FROM orgs ORDER BY created_at`,
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var orgs []Org
	for rows.Next() {
		var o Org
		if err := rows.Scan(&o.ID, &o.Name, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, mapError(err)
		}
		orgs = append(orgs, o)
	}
	if err := rows.Err(); err != nil {
		return nil, mapError(err)
	}

	return orgs, nil
}
