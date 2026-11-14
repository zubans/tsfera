package platform

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Querier абстрагирует *pgxpool.Pool и pgx.Tx для единообразной работы репозиториев в транзакциях и без них.
type Querier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// TxManager управляет границами транзакций в сервисном слое.
type TxManager interface {
	InTx(ctx context.Context, fn func(q Querier) error) error
}

// PgxTxManager реализует TxManager поверх *pgxpool.Pool.
type PgxTxManager struct {
	Pool *pgxpool.Pool
}

func (m *PgxTxManager) InTx(ctx context.Context, fn func(q Querier) error) error {
	tx, err := m.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
	}()
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

// ExecExpectingOne проверяет, что UPDATE/DELETE затронул ровно одну строку; иначе возвращает ErrConflict.
func ExecExpectingOne(ctx context.Context, q Querier, sql string, conflictMsg string, args ...any) error {
	tag, err := q.Exec(ctx, sql, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return NewConflict(conflictMsg)
	}
	return nil
}

// MigrationSource связывает имя модуля с встроенной ФС его SQL-миграций.
type MigrationSource struct {
	Module string
	FS     fs.FS
}

// Migrate последовательно применяет встроенные SQL-миграции каждого модуля.
func Migrate(ctx context.Context, pool *pgxpool.Pool, sources ...MigrationSource) error {
	if pool == nil {
		return fmt.Errorf("nil pgx pool")
	}
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS platform_schema_migrations (
			module TEXT NOT NULL,
			version TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (module, version)
		)
	`)
	if err != nil {
		return fmt.Errorf("create platform_schema_migrations: %w", err)
	}

	txMgr := &PgxTxManager{Pool: pool}
	for _, src := range sources {
		if src.FS == nil {
			continue
		}
		var files []string
		err := fs.WalkDir(src.FS, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(d.Name(), ".sql") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("walk migrations for %s: %w", src.Module, err)
		}
		sort.Strings(files)

		for _, file := range files {
			var exists bool
			if err := pool.QueryRow(ctx,
				`SELECT EXISTS(SELECT 1 FROM platform_schema_migrations WHERE module = $1 AND version = $2)`,
				src.Module, file,
			).Scan(&exists); err != nil {
				return fmt.Errorf("check migration %s/%s: %w", src.Module, file, err)
			}
			if exists {
				continue
			}

			raw, err := fs.ReadFile(src.FS, file)
			if err != nil {
				return fmt.Errorf("read migration %s/%s: %w", src.Module, file, err)
			}
			if err := txMgr.InTx(ctx, func(q Querier) error {
				if _, err := q.Exec(ctx, string(raw)); err != nil {
					return err
				}
				_, err := q.Exec(ctx,
					`INSERT INTO platform_schema_migrations (module, version) VALUES ($1, $2)`,
					src.Module, file,
				)
				return err
			}); err != nil {
				return fmt.Errorf("apply migration %s/%s: %w", src.Module, file, err)
			}
		}
	}
	return nil
}
