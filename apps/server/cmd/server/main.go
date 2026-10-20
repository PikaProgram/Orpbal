package main

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/rs/zerolog"

	"Orpbal/internal/database"
	"Orpbal/internal/handlers"
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
	// Load environment variables
	godotenv.Load()

	// Initialize logger
	initLogger()

	logger.Info().Msg("Starting Orpbal server")

	// Initialize database (run `go generate ./ent` to use Ent client)
	db, err := database.InitDB()
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to connect to database")
	}
	defer database.Close()
	logger.Info().Msg("Database connected successfully")
	_ = db // Use db in your handlers

	// Get host from environment
	host := os.Getenv("HOST")
	if host == "" {
		host = "0.0.0.0"
	}
	// Get HTTP port from environment
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := host + ":" + port

	logger.Info().Str("address", addr).Msg("Starting HTTP server")

	// CORS: pinned to CORS_ORIGIN when set, permissive otherwise.
	corsOrigin := os.Getenv("CORS_ORIGIN")
	if corsOrigin == "" {
		corsOrigin = "*"
	}

	// Create Gin router
	r := gin.Default()
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", corsOrigin)
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	})

	// Health check endpoint
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"message": "Server is running",
		})
	})

	// Root endpoint
	r.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "Welcome to Orpbal!",
		})
	})

	api := r.Group("/api")
	api.GET("/users", handlers.GetUsers)
	api.POST("/users", handlers.CreateUser)
	api.DELETE("/users/:id", handlers.DeleteUser)
	api.GET("/posts", handlers.GetPosts)
	api.POST("/posts", handlers.CreatePost)

	// Start server
	if err := r.Run(addr); err != nil {
		logger.Fatal().Err(err).Msg("Failed to start server")
	}
}
