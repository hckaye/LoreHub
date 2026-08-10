package database

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lorehub/lorehub/services/api/migrations"
)

const migrationLockID int64 = 4_708_312_024

type migration struct {
	version int64
	name    string
	sql     string
}

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	connection, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer connection.Release()

	if _, err := connection.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}
	defer func() {
		_, _ = connection.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", migrationLockID)
	}()

	if _, err := connection.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version bigint PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		)
	`); err != nil {
		return fmt.Errorf("prepare migration table: %w", err)
	}

	loaded, err := loadMigrations()
	if err != nil {
		return err
	}
	for _, item := range loaded {
		applied, err := migrationApplied(ctx, connection, item.version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		if err := applyMigration(ctx, connection, item); err != nil {
			return err
		}
	}

	return nil
}

// MigrationsReady verifies that every migration embedded in this binary has
// been applied. It deliberately checks each version instead of only looking
// at the highest version, so a partially restored database cannot pass health.
func MigrationsReady(ctx context.Context, pool *pgxpool.Pool) error {
	loaded, err := loadMigrations()
	if err != nil {
		return err
	}
	rows, err := pool.Query(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return fmt.Errorf("read applied migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[int64]struct{}, len(loaded))
	for rows.Next() {
		var version int64
		if err := rows.Scan(&version); err != nil {
			return fmt.Errorf("scan applied migration: %w", err)
		}
		applied[version] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read applied migrations: %w", err)
	}
	for _, item := range loaded {
		if _, ok := applied[item.version]; !ok {
			return fmt.Errorf("migration %s has not been applied", item.name)
		}
	}
	return nil
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrations.Files, ".")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}

	loaded := make([]migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("migration %q has no numeric prefix", entry.Name())
		}
		version, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse migration %q: %w", entry.Name(), err)
		}
		contents, err := migrations.Files.ReadFile(entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", entry.Name(), err)
		}
		loaded = append(loaded, migration{version: version, name: entry.Name(), sql: string(contents)})
	}
	sort.Slice(loaded, func(i int, j int) bool { return loaded[i].version < loaded[j].version })
	return loaded, nil
}

func migrationApplied(ctx context.Context, connection *pgxpool.Conn, version int64) (bool, error) {
	var applied bool
	err := connection.QueryRow(
		ctx,
		"SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)",
		version,
	).Scan(&applied)
	if err != nil {
		return false, fmt.Errorf("check migration %d: %w", version, err)
	}
	return applied, nil
}

func applyMigration(ctx context.Context, connection *pgxpool.Conn, item migration) error {
	transaction, err := connection.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin migration %q: %w", item.name, err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()

	if _, err := transaction.Exec(ctx, item.sql); err != nil {
		return fmt.Errorf("apply migration %q: %w", item.name, err)
	}
	if _, err := transaction.Exec(
		ctx,
		"INSERT INTO schema_migrations (version) VALUES ($1)",
		item.version,
	); err != nil {
		return fmt.Errorf("record migration %q: %w", item.name, err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration %q: %w", item.name, err)
	}
	return nil
}
