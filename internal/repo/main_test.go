package repo_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Um container por PACOTE, não por teste. Sobe em ~2s e vale muito mais que
// mockar pgx: mockando pgx você testaria o mock, não o ON CONFLICT.
var pool *pgxpool.Pool

func TestMain(m *testing.M) {
	code, err := run(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, "setup:", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func run(m *testing.M) (int, error) {
	ctx := context.Background()

	pg, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("fera"),
		postgres.WithUsername("fera"),
		postgres.WithPassword("fera"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(90*time.Second)),
	)
	if err != nil {
		return 0, fmt.Errorf("subir postgres: %w", err)
	}
	defer func() { _ = testcontainers.TerminateContainer(pg) }()

	dsn, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return 0, err
	}

	pool, err = pgxpool.New(ctx, dsn)
	if err != nil {
		return 0, err
	}
	defer pool.Close()

	// Migrações rodam no setup do container, não no boot da aplicação, e
	// TODAS elas em ordem: rodar só a 0001 faria o teste validar um schema
	// que não é o que vai pra produção.
	migs, err := filepath.Glob("../../migrations/*.sql")
	if err != nil {
		return 0, err
	}
	sort.Strings(migs)
	if len(migs) == 0 {
		return 0, fmt.Errorf("nenhuma migração encontrada")
	}
	for _, m := range migs {
		sql, err := os.ReadFile(m)
		if err != nil {
			return 0, err
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			return 0, fmt.Errorf("migração %s: %w", filepath.Base(m), err)
		}
	}

	return m.Run(), nil
}
