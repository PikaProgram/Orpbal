package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/rs/zerolog"

	"Orpbal/internal/api"
	"Orpbal/internal/auth"
	"Orpbal/internal/bootstrap"
	"Orpbal/internal/config"
	"Orpbal/internal/database"
	"Orpbal/internal/handler"
	"Orpbal/internal/observability"
	"Orpbal/internal/security/crypto"
	"Orpbal/internal/security/csrf"
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

	telemetry, err := observability.Init(observability.ServiceName("orpbal-server"), os.Stdout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "telemetry init failed: %v\n", err)
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
	observability.RegisterDB(database.RawDB(), "platform", true)

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
	r.Use(observability.HTTPMiddleware())

	r.GET("/metrics", gin.WrapH(observability.Handler()))

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"message": "Server is running",
		})
	})

	r.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "Welcome to Orpbal!",
		})
	})

	apiGroup := r.Group("/api")
	api.RegisterHandlersWithOptions(apiGroup, apiHandler, api.GinServerOptions{
		ErrorHandler: apiHandler.ParameterError,
		Middlewares: []api.MiddlewareFunc{
			api.MiddlewareFunc(auth.Authenticate(sessions, cfg.CookieSecure, cfg.AppOrigin, logger)),
			api.MiddlewareFunc(auth.RequirePlatformAdmin()),
		},
	})

	logger.Info().Str("address", addr).Msg("Starting HTTP server")
	if err := r.Run(addr); err != nil {
		logger.Fatal().Err(err).Msg("Failed to start server")
	}
}
