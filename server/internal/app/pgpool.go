package app

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func initPgPool(dsn string) (*pgxpool.Pool, error) {
	pgConf, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.ParseConfig: %w", err)
	}

	pgConf.MaxConns = 10
	pgConf.MinConns = 2
	pgConf.MaxConnLifetime = time.Hour
	pgConf.MaxConnIdleTime = 5 * time.Minute
	pgConf.HealthCheckPeriod = 15 * time.Second

	pgpool, err := pgxpool.NewWithConfig(context.Background(), pgConf)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.NewWithConfig: %w", err)
	}

	return pgpool, nil
}
