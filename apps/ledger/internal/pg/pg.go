package pg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Consultador interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func Abrir(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parsear DB_URL: %w", err)
	}
	cfg.MinConns = 2
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("conectar a la base: %w", err)
	}
	return pool, nil
}

func EnTx(ctx context.Context, pool *pgxpool.Pool, fn func(tx pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {

		_ = tx.Rollback(ctx)
	}()

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func EsViolacionUnica(err error, restriccion string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return false
	}
	return restriccion == "" || pgErr.ConstraintName == restriccion
}

func InicioDeMes(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func NombreParticion(periodo time.Time) string {
	return fmt.Sprintf("asientos_%s", periodo.Format("2006_01"))
}

func AsegurarParticion(ctx context.Context, c Consultador, periodo time.Time) error {
	periodo = InicioDeMes(periodo)
	siguiente := periodo.AddDate(0, 1, 0)
	sql := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF asientos FOR VALUES FROM ('%s') TO ('%s')`,
		NombreParticion(periodo), periodo.Format("2006-01-02"), siguiente.Format("2006-01-02"),
	)

	if _, err := c.Exec(ctx, sql); err != nil && !esTablaDuplicada(err) {
		return fmt.Errorf("crear particion %s: %w", NombreParticion(periodo), err)
	}
	return nil
}

func esTablaDuplicada(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42P07"
}

func AsegurarParticiones(ctx context.Context, c Consultador, desde, hasta int) error {
	base := InicioDeMes(time.Now())
	for m := desde; m <= hasta; m++ {
		if err := AsegurarParticion(ctx, c, base.AddDate(0, m, 0)); err != nil {
			return err
		}
	}
	return nil
}
