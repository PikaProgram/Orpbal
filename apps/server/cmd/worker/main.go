// Worker binary: claims migration jobs from the platform queue and executes
// them against managed target databases, one job at a time.
package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/rs/zerolog"

	"Orpbal/ent"
	"Orpbal/ent/migrate"
	"Orpbal/internal/metrics"
	"Orpbal/internal/queue"
	"Orpbal/internal/security/crypto"
	"Orpbal/internal/target"
	"Orpbal/internal/tracing"
	"Orpbal/internal/worker"
)

var logger zerolog.Logger

// initLogger emits structured JSON to stdout in every environment (M2
// requires machine-readable activity logs); console format is opt-in via
// LOG_FORMAT=console for local development.
func initLogger() {
	if os.Getenv("LOG_FORMAT") == "console" {
		logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()
	} else {
		logger = zerolog.New(os.Stdout).With().Timestamp().Logger()
	}
}

func main() {
	godotenv.Load()

	initLogger()

	logger.Info().Str("event", "startup").Msg("Starting Orpbal worker")

	shutdownTracing, err := tracing.Init("orpbal-worker")
	if err != nil {
		logger.Warn().Err(err).Msg("Tracing init failed; continuing without spans")
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		logger.Fatal().Msg("DATABASE_URL is required")
	}
	encodedKey := os.Getenv("ORPBAL_ENCRYPTION_KEY")
	if encodedKey == "" {
		logger.Fatal().Msg("ORPBAL_ENCRYPTION_KEY is required")
	}

	workerID := os.Getenv("WORKER_ID")
	if workerID == "" {
		hostname, hostErr := os.Hostname()
		if hostErr != nil {
			hostname = "worker"
		}
		workerID = hostname
	}
	pollInterval := 1 * time.Second
	if raw := os.Getenv("ORPBAL_POLL_INTERVAL"); raw != "" {
		if seconds, pErr := strconv.Atoi(raw); pErr == nil && seconds > 0 {
			pollInterval = time.Duration(seconds) * time.Second
		}
	}

	ctx := context.Background()

	// Same schema bootstrap as the server so a demo env needs no extra step.
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to open platform database")
	}
	client, err := ent.Open("postgres", dsn)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to open ent client")
	}
	defer client.Close()
	defer func() { _ = db.Close() }()
	if err := client.Schema.Create(ctx, migrate.WithDropColumn(false), migrate.WithDropIndex(false)); err != nil {
		logger.Fatal().Err(err).Msg("Failed to migrate platform schema")
	}
	logger.Info().Str("event", "startup").Msg("Platform database ready")

	cipher, err := crypto.NewFromBase64(encodedKey)
	if err != nil {
		logger.Fatal().Err(err).Msg("Invalid ORPBAL_ENCRYPTION_KEY")
	}

	q := queue.New(db, logger)
	targets := target.NewManager(nil, cipher)
	runner := worker.NewRunner(client, q, targets, logger)

	runCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Prometheus scrape endpoint for the worker (claims, outcomes, batch
	// throughput). WORKER_METRICS_ADDR defaults to :9102.
	metricsAddr := os.Getenv("WORKER_METRICS_ADDR")
	if metricsAddr == "" {
		metricsAddr = ":9102"
	}
	metricsServer := &http.Server{Addr: metricsAddr, Handler: metrics.Handler()}
	go func() {
		logger.Info().Str("event", "startup").Str("address", metricsAddr).Msg("Worker metrics server listening")
		if err := metricsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error().Err(err).Str("event", "metrics_server_error").Msg("Worker metrics server failed")
		}
	}()
	go func() {
		<-runCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = metricsServer.Shutdown(shutdownCtx)
	}()

	go func() {
		if err := shutdownTracing(runCtx); err != nil {
			logger.Warn().Err(err).Msg("Tracing shutdown failed")
		}
	}()

	logger.Info().Str("event", "startup").Str("worker_id", workerID).Msg("Worker polling for jobs")
	runner.Run(runCtx, workerID, pollInterval)
	logger.Info().Str("event", "shutdown").Msg("Worker stopped")
}
