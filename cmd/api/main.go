// Command api é o binário do backend da FERA. Um binário, não cinco.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	httpapi "github.com/ale/fera/internal/http"
	"github.com/ale/fera/internal/repo"
	"github.com/ale/fera/internal/service"
	"github.com/ale/fera/internal/sim"
)

func main() {
	if err := run(); err != nil {
		slog.Error("api encerrou com erro", "erro", err)
		os.Exit(1)
	}
}

// Todo o wiring mora aqui, explícito, sem framework de DI. Dá pra ler de cima
// a baixo e saber exatamente o que depende de quê.
func run() error {
	addr := flag.String("addr", ":8080", "endereço de escuta")
	dsn := flag.String("dsn", os.Getenv("FERA_DSN"), "DSN do Postgres")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	if *dsn == "" {
		return errors.New("faltou -dsn ou FERA_DSN")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := novoPool(ctx, *dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	pets := service.New(
		repo.NewEventRepo(pool),
		repo.NewSnapshotRepo(pool),
		time.Now,
		sim.DefaultTuning(),
	)
	devices := service.NewDeviceService(repo.NewDeviceRepo(pool), time.Now, uuid.NewString)

	router := httpapi.NewRouter(
		httpapi.NewPetHandler(pets, time.Now),
		httpapi.NewDeviceHandler(devices),
		devices,
		time.Now,
		pool,
	)

	srv := &http.Server{
		Addr:    *addr,
		Handler: router,
		// Sem ReadHeaderTimeout é Slowloris na veia: um cliente segura a
		// conexão mandando header byte a byte e nunca solta.
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	erroServidor := make(chan error, 1)
	go func() {
		slog.Info("api ouvindo", "addr", *addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			erroServidor <- err
		}
	}()

	select {
	case err := <-erroServidor:
		return fmt.Errorf("servidor: %w", err)
	case <-ctx.Done():
		slog.Info("sinal recebido, drenando")
	}

	// Drena as requisições em voo antes de fechar o pool. Ordem importa: fechar
	// o pool primeiro mataria quem ainda está no meio de uma query.
	shutCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	slog.Info("api encerrada")
	return nil
}

func novoPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("dsn inválido: %w", err)
	}
	cfg.MaxConns = int32(runtime.NumCPU() * 4)
	cfg.MinConns = 2
	// Menor que qualquer timeout de proxy no meio, senão você pega conexão
	// que o outro lado já derrubou.
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("abrir pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("banco inacessível: %w", err)
	}
	return pool, nil
}
