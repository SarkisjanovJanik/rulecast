package store

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

func Migrate(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) error {
	const createLedger = `
		CREATE TABLE IF NOT EXISTS schema_migration (
			name       TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`

	if _, err := pool.Exec(ctx, createLedger); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}

	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		applied, err := alreadyApplied(ctx, pool, name)
		if err != nil {
			return err
		}
		if applied {
			continue
		}

		script, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		if err := apply(ctx, pool, name, string(script)); err != nil {
			return err
		}
		logger.Info("applied migration", "name", name)
	}

	return nil
}

func alreadyApplied(ctx context.Context, pool *pgxpool.Pool, name string) (bool, error) {
	const query = `SELECT exists(SELECT 1 FROM schema_migration WHERE name = $1)`

	var applied bool
	if err := pool.QueryRow(ctx, query, name).Scan(&applied); err != nil {
		return false, fmt.Errorf("check migration %s: %w", name, err)
	}
	return applied, nil
}

func apply(ctx context.Context, pool *pgxpool.Pool, name, script string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", name, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, script); err != nil {
		return fmt.Errorf("run migration %s: %w", name, err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO schema_migration (name) VALUES ($1)`, name); err != nil {
		return fmt.Errorf("record migration %s: %w", name, err)
	}

	return tx.Commit(ctx)
}
