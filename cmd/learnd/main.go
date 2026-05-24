package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/drywaters/learnd/internal/config"
	"github.com/drywaters/learnd/internal/enricher"
	"github.com/drywaters/learnd/internal/repository"
	"github.com/drywaters/learnd/internal/server"
	"github.com/drywaters/learnd/internal/summarizer"
	"github.com/drywaters/learnd/internal/worker"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	version  = "dev"
	revision = "unknown"
)

func main() {
	if err := run(); err != nil {
		slog.Error("application error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Set up logging
	logLevel := slog.LevelInfo
	switch cfg.LogLevel {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	slog.Info("starting learnd", "port", cfg.Port, "version", version, "revision", revision)

	// Connect to database
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer pool.Close()

	// Verify database connection
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}
	slog.Info("connected to database")

	// Initialize repositories
	entryRepo := repository.NewEntryRepository(pool)
	summaryCacheRepo := repository.NewSummaryCacheRepository(pool)

	// Initialize enrichers
	webEnricher := enricher.NewWebEnricher()
	enrichRegistry := enricher.NewRegistry(webEnricher)

	// Register YouTube enricher if API key is available
	if cfg.YouTubeAPIKey != "" {
		youtubeEnricher := enricher.NewYouTubeEnricher(cfg.YouTubeAPIKey)
		enrichRegistry.Register(youtubeEnricher)
		slog.Info("YouTube enricher enabled")
	} else {
		slog.Warn("YouTube API key not configured, YouTube enrichment disabled")
	}

	// Register podcast enricher
	podcastEnricher := enricher.NewPodcastEnricher()
	enrichRegistry.Register(podcastEnricher)

	// Initialize summarizer
	var sum summarizer.Summarizer
	if cfg.GeminiAPIKey != "" {
		var err error
		sum, err = summarizer.NewGeminiSummarizerWithModel(ctx, cfg.GeminiAPIKey, cfg.GeminiModel)
		if err != nil {
			slog.Warn("failed to initialize Gemini summarizer", "error", err)
		} else {
			slog.Info("Gemini summarizer enabled", "model", sum.Model())
		}
	} else {
		slog.Warn("Gemini API key not configured, summarization disabled")
	}

	// Initialize and start background worker
	bgWorker := worker.New(entryRepo, summaryCacheRepo, enrichRegistry, sum, worker.Config{
		Interval:  10 * time.Second,
		BatchSize: 5,
	})
	bgWorker.Start(ctx)

	// Create server
	srv := server.New(cfg, entryRepo, summaryCacheRepo)

	// Start HTTP server
	httpServer := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      srv.Router(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	shutdownChan := make(chan os.Signal, 1)
	signal.Notify(shutdownChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		slog.Info("server listening", "addr", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
		}
	}()

	<-shutdownChan
	slog.Info("shutting down...")

	// Stop background worker
	bgWorker.Stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server shutdown error: %w", err)
	}

	slog.Info("server stopped")
	return nil
}
