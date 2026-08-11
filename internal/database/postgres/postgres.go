package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresClient interface {
	Ping(ctx context.Context) error
	Pool() *pgxpool.Pool
	Close() error
}

type Postgres struct {
	pool *pgxpool.Pool
}

var _ PostgresClient = (*Postgres)(nil)

func New(ctx context.Context, dsn string) (PostgresClient, error) {
	pg := &Postgres{}
	if err := pg.connect(ctx, dsn); err != nil {
		return nil, err
	}
	return pg, nil
}

func (p *Postgres) connect(ctx context.Context, dsn string) error {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("failed to parse postgres dsn: %w", err)
	}

	if config.MaxConns == 0 {
		config.MaxConns = 10
	}
	if config.MinConns == 0 {
		config.MinConns = 5
	}
	if config.MaxConnLifetime == 0 {
		config.MaxConnLifetime = time.Hour
	}
	if config.MaxConnIdleTime == 0 {
		config.MaxConnIdleTime = 30 * time.Minute
	}
	if config.HealthCheckPeriod == 0 {
		config.HealthCheckPeriod = 5 * time.Minute
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to create postgres pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return fmt.Errorf("failed to ping postgres: %w", err)
	}

	p.pool = pool
	return nil
}

func (p *Postgres) Pool() *pgxpool.Pool {
	return p.pool
}

func (p *Postgres) Ping(ctx context.Context) error {
	if p.pool == nil {
		return fmt.Errorf("pool not connected")
	}
	return p.pool.Ping(ctx)
}

func (p *Postgres) Close() error {
	if p.pool == nil {
		return nil
	}
	p.pool.Close()
	return nil
}
