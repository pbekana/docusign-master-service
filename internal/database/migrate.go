// Package database provides PostgreSQL connection pool management and migrations.
package database

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RunMigrations reads every *.sql file from migrationsDir in lexicographic order
// and executes each inside its own transaction. Files must use IF NOT EXISTS so
// the runner is idempotent — re-running the same migration is safe.
func RunMigrations(ctx context.Context, pool *pgxpool.Pool, migrationsDir string) error {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("read migrations directory %q: %w", migrationsDir, err)
	}

	// Collect only .sql files and sort them lexicographically.
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, name := range files {
		path := filepath.Join(migrationsDir, name)
		if err := runMigrationFile(ctx, pool, path, name); err != nil {
			return err
		}
	}
	return nil
}

func runMigrationFile(ctx context.Context, pool *pgxpool.Pool, path, name string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read migration file %q: %w", name, err)
	}

	sql := strings.TrimSpace(string(data))
	if sql == "" {
		slog.InfoContext(ctx, "skipping empty migration file", "file", name)
		return nil
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction for %q: %w", name, err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck — rollback is best-effort cleanup

	if _, err := tx.Exec(ctx, sql); err != nil {
		return fmt.Errorf("execute migration %q: %w", name, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration %q: %w", name, err)
	}

	slog.InfoContext(ctx, "applied migration", "file", name)
	return nil
}
