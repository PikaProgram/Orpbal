package main

import (
	"context"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/rs/zerolog"

	"Orpbal/internal/bootstrap"
	"Orpbal/internal/database"
)

var logger zerolog.Logger

func initLogger() {
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "production" {
		logger = zerolog.New(os.Stdout).With().Timestamp().Logger()
	} else {
		logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()
	}
}

func main() {
	godotenv.Load()

	initLogger()

	logger.Info().Msg("Starting Orpbal server")

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

	host := os.Getenv("HOST")
	if host == "" {
		host = "0.0.0.0"
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := host + ":" + port

	logger.Info().Str("address", addr).Msg("Starting HTTP server")

	r := gin.Default()

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

	if err := r.Run(addr); err != nil {
		logger.Fatal().Err(err).Msg("Failed to start server")
	}
}
