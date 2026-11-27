// Worker binary: claims migration jobs from the platform queue and executes
// them against managed target databases, one job at a time.
package main

import (
	"context"
	"database/sql"
	"io"
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
	"Orpbal/internal/observability"
	"Orpbal/internal/queue"
	"Orpbal/internal/security/crypto"
	"Orpbal/internal/target"
	"Orpbal/internal/worker"
)

var logger zerolog.Logger

func initLogger(base io.Writer) {
	if !observability.JSONLogs() {
		base = zerolog.ConsoleWriter{Out: base}
	}
	logger = zerolog.New(base).With().Timestamp().Logger()
}

func main() {
	godotenv.Load()

	telemetry, err := observability.Init(observability.ServiceName("orpbal-worker"), os.Stdout)
	if err != nil {
		// No zerolog logger yet; keep local startup visible.
		_, _ = io.WriteString(os.Stderr, "telemetry init failed: "+err.Error()+"\n")
	}
	defer func() {
		if telemetry != nil {
			_ = telemetry.Shutdown(context.Background())
		}
	}()

	var logOutput io.Writer = os.Stdout
	if telemetry != nil {
		logOutput = telemetry.LogWriter
	}
	initLogger(logOutput)

	logger.Info().Str("event", "startup").Msg("Starting Orpbal worker")

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

	observability.RegisterDB(db, "platform", false)
	metricsAddr := os.Getenv("ORPBAL_METRICS_ADDR")
	if metricsAddr == "" {
		metricsAddr = ":9090"
	}
	if err := observability.StartMetricsServer(ctx, metricsAddr); err != nil {
		logger.Warn().Err(err).Str("event", "metrics_start_error").Msg("metrics endpoint unavailable")
	} else {
		logger.Info().Str("event", "metrics_ready").Str("address", metricsAddr).Msg("Worker metrics ready")
	}

	cipher, err := crypto.NewFromBase64(encodedKey)
	if err != nil {
		logger.Fatal().Err(err).Msg("Invalid ORPBAL_ENCRYPTION_KEY")
	}

	q := queue.New(db, logger)
	targets := target.NewManager(nil, cipher)
	runner := worker.NewRunner(client, q, targets, logger)

	runCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info().Str("event", "startup").Str("worker_id", workerID).Msg("Worker polling for jobs")
	runner.Run(runCtx, workerID, pollInterval)
	logger.Info().Str("event", "shutdown").Msg("Worker stopped")
}
