package db

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// advisoryLockKey is an arbitrary, stable identifier for the Postgres
// advisory lock guarding migration application across concurrent replicas.
const advisoryLockKey = 727100

// Migration is one forward-only schema change.
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// LoadMigrations reads every "NNN_name.sql" file from fsys and returns them
// sorted by version, erroring on a malformed filename or a duplicate version.
func LoadMigrations(fsys fs.FS) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("db: read migrations dir: %w", err)
	}

	var migrations []Migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		version, name, err := parseMigrationFilename(entry.Name())
		if err != nil {
			return nil, err
		}

		data, err := fs.ReadFile(fsys, entry.Name())
		if err != nil {
			return nil, fmt.Errorf("db: read migration %s: %w", entry.Name(), err)
		}

		migrations = append(migrations, Migration{Version: version, Name: name, SQL: string(data)})
	}

	sort.Slice(migrations, func(i, j int) bool { return migrations[i].Version < migrations[j].Version })

	for i := 1; i < len(migrations); i++ {
		if migrations[i].Version == migrations[i-1].Version {
			return nil, fmt.Errorf("db: duplicate migration version %d", migrations[i].Version)
		}
	}

	return migrations, nil
}

func parseMigrationFilename(filename string) (int, string, error) {
	base := strings.TrimSuffix(filename, ".sql")
	parts := strings.SplitN(base, "_", 2)
	if len(parts) != 2 {
		return 0, "", fmt.Errorf("db: malformed migration filename %q, want NNN_name.sql", filename)
	}

	version, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, "", fmt.Errorf("db: malformed migration version in %q: %w", filename, err)
	}

	return version, parts[1], nil
}

// Migrator applies pending migrations to a database exactly once, guarded
// by a Postgres advisory lock so concurrent gateway replicas cannot race
// the same migration.
type Migrator struct {
	pool *Pool
}

// NewMigrator returns a Migrator bound to pool.
func NewMigrator(pool *Pool) *Migrator {
	return &Migrator{pool: pool}
}

// Migrate applies every migration in migrations whose version is not yet
// recorded in schema_migrations, each inside its own transaction, and
// returns the versions it applied in order.
func (m *Migrator) Migrate(ctx context.Context, migrations []Migration) ([]int, error) {
	conn, err := m.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("db: acquire conn: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", advisoryLockKey); err != nil {
		return nil, fmt.Errorf("db: acquire advisory lock: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", advisoryLockKey)
	}()

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return nil, fmt.Errorf("db: ensure schema_migrations table: %w", err)
	}

	alreadyApplied, err := appliedVersions(ctx, conn)
	if err != nil {
		return nil, err
	}

	var applied []int
	for _, mig := range migrations {
		if alreadyApplied[mig.Version] {
			continue
		}

		if err := applyMigration(ctx, conn, mig); err != nil {
			return applied, err
		}

		applied = append(applied, mig.Version)
	}

	return applied, nil
}

func appliedVersions(ctx context.Context, conn *pgxpool.Conn) (map[int]bool, error) {
	rows, err := conn.Query(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("db: read applied migrations: %w", err)
	}
	defer rows.Close()

	applied := map[int]bool{}
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("db: scan applied migration: %w", err)
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: iterate applied migrations: %w", err)
	}

	return applied, nil
}

func applyMigration(ctx context.Context, conn *pgxpool.Conn, mig Migration) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: begin tx for migration %d: %w", mig.Version, err)
	}

	if _, err := tx.Exec(ctx, mig.SQL); err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("db: apply migration %d (%s): %w", mig.Version, mig.Name, err)
	}

	if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version, name) VALUES ($1, $2)", mig.Version, mig.Name); err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("db: record migration %d: %w", mig.Version, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: commit migration %d: %w", mig.Version, err)
	}

	return nil
}
