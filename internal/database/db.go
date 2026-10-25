// Package database provides PostgreSQL connection pool management.
package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/your-org/docusign-master-service/internal/config"
)

// Pool is an alias for pgxpool.Pool so callers can import only this package.
type Pool = pgxpool.Pool

// NewPool constructs a pgxpool.Pool from the supplied configuration.
// The caller is responsible for calling pool.Close() when done.
func NewPool(ctx context.Context, cfg *config.Config) (*Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", err)
	}

	poolCfg.MaxConns = cfg.DBMaxConns
	poolCfg.MinConns = cfg.DBMinConns

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	return pool, nil
}

// Ping verifies the pool can reach the database.
func Ping(ctx context.Context, pool *Pool) error {
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("database ping: %w", err)
	}
	return nil
}
