package main

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/rs/zerolog"

	"Orpbal/internal/api"
	"Orpbal/internal/auth"
	"Orpbal/internal/bootstrap"
	"Orpbal/internal/config"
	"Orpbal/internal/database"
	"Orpbal/internal/handler"
	"Orpbal/internal/metrics"
	"Orpbal/internal/security/crypto"
	"Orpbal/internal/security/csrf"
	"Orpbal/internal/sqllog"
	"Orpbal/internal/tracing"
)

var logger zerolog.Logger

// initLogger emits structured JSON to stdout in every environment (the M2
// spec requires machine-readable activity logs); the human-friendly console
// format is opt-in via LOG_FORMAT=console for local development.
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

	// OpenTelemetry tracer provider: server spans join the same trace the
	// worker resumes from the job's traceparent, closing the API -> queue ->
	// worker -> lock -> batch chain.
	shutdownTracing, err := tracing.Init("orpbal-api")
	if err != nil {
		logger.Warn().Err(err).Msg("Tracing disabled; continuing without spans")
	} else {
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = shutdownTracing(ctx)
		}()
	}

	logger.Info().Msg("Starting Orpbal server")

	cfg, err := config.Load()
	if err != nil {
		logger.Fatal().Err(err).Msg("Invalid configuration")
	}

	ctx := context.Background()
	db, err := database.InitDB(ctx)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to initialize database")
	}
	defer func() {
		if err := database.Close(); err != nil {
			logger.Error().Err(err).Msg("Failed to close database")
		}
	}()
	logger.Info().Msg("Platform database ready")

	if err := bootstrap.EnsureAdmin(ctx, db, logger); err != nil {
		logger.Fatal().Err(err).Msg("Failed to bootstrap platform admin")
	}

	cipher, err := crypto.New(cfg.EncryptionKey)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to initialize credential cipher")
	}
	csrfManager, err := csrf.New(cfg.EncryptionKey)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to initialize CSRF manager")
	}
	sessions, err := auth.NewService(db, cfg.Session, csrfManager)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to initialize session service")
	}
	apiHandler := handler.New(db, sessions, cipher, cfg.CookieSecure, logger)

	host := os.Getenv("HOST")
	if host == "" {
		host = "0.0.0.0"
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := host + ":" + port

	r := gin.Default()
	_ = r.SetTrustedProxies(nil)

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"message": "Server is running",
		})
	})

	// Prometheus scrape endpoint (unauthenticated by design: bind the port to
	// an internal network — compose keeps it off the published ports).
	r.GET("/metrics", gin.WrapH(metrics.Handler()))

	// Refresh the migration_jobs state gauge periodically from the platform DB.
	go refreshJobGauge(logger)

	r.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "Welcome to Orpbal!",
		})
	})

	apiGroup := r.Group("/api")
	api.RegisterHandlersWithOptions(apiGroup, apiHandler, api.GinServerOptions{
		ErrorHandler: apiHandler.ParameterError,
		Middlewares: []api.MiddlewareFunc{
			// Per-request OTel span + request log (traceparent extraction
			// included) must run first so downstream code shares the trace.
			api.MiddlewareFunc(tracing.GinMiddleware(logger)),
			// Prometheus request counters + latency histogram (route template
			// label keeps cardinality bounded).
			api.MiddlewareFunc(metrics.GinMiddleware()),
			api.MiddlewareFunc(auth.Authenticate(sessions, cfg.CookieSecure, cfg.AppOrigin, logger)),
			api.MiddlewareFunc(auth.RequirePlatformAdmin()),
			// SQL audit logger: every SQL the request triggers (read-only
			// queries, row mutations, EXPLAIN, introspection, migration
			// writes) carries the actor + trace_id/span_id correlation.
			api.MiddlewareFunc(func(c *gin.Context) {
				base := logger.With().Str("component", "api").Logger()
				if user := auth.CurrentUser(c); user != nil {
					base = base.With().
						Int("actor_id", user.ID).
						Str("actor", user.Username).
						Bool("is_platform_admin", user.IsPlatformAdmin).
						Logger()
				}
				c.Request = c.Request.WithContext(sqllog.Attach(c.Request.Context(), base))
				c.Next()
			}),
		},
	})

	logger.Info().Str("address", addr).Msg("Starting HTTP server")
	if err := r.Run(addr); err != nil {
		logger.Fatal().Err(err).Msg("Failed to start server")
	}
}

// refreshJobGauge polls the migration_jobs status counts into the
// orpbal_migration_jobs gauge every 15 seconds until the process exits.
func refreshJobGauge(log zerolog.Logger) {
	pool := database.SQLDB()
	if pool == nil {
		return
	}
	for {
		counts := map[string]int{}
		rows, err := pool.QueryContext(context.Background(),
			"SELECT status, count(*) FROM migration_jobs GROUP BY status")
		if err == nil {
			for rows.Next() {
				var status string
				var n int
				if err := rows.Scan(&status, &n); err == nil {
					counts[status] = n
				}
			}
			_ = rows.Close()
			metrics.SetJobStatusGauge(counts)
		} else if ctxErr := context.Background().Err(); ctxErr == nil {
			log.Warn().Err(err).Str("event", "metrics_gauge_error").Msg("job gauge refresh failed")
		}
		time.Sleep(15 * time.Second)
	}
}
