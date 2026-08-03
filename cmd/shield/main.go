// Package main provides the entry point for the Gremlyn Shield server.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gremlyn-ai/gremlyn/pkg/config"
	"github.com/gremlyn-ai/gremlyn/pkg/datadir"
	"github.com/gremlyn-ai/gremlyn/internal/shield/alert"
	"github.com/gremlyn-ai/gremlyn/internal/shield/api"
	"github.com/gremlyn-ai/gremlyn/internal/shield/service"
	"github.com/gremlyn-ai/gremlyn/internal/shield/storage/postgres"
	sqlitestore "github.com/gremlyn-ai/gremlyn/internal/shield/storage/sqlite"
	"github.com/rs/zerolog"
)

func main() {
	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}).
		With().Timestamp().Str("service", "shield").Logger()

	if err := run(logger); err != nil {
		logger.Fatal().Err(err).Msg("shield exited with error")
	}
}

func run(logger zerolog.Logger) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Load gremlyn.yaml config.
	cfgPath := os.Getenv("GREMLYN_CONFIG")
	if cfgPath == "" {
		cfgPath = "gremlyn.yaml"
	}
	cfg, err := config.LoadConfig(ctx, cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Connect to database: SQLite by default, PostgreSQL when DATABASE_URL is set.
	var (
		events  service.EventStore
		rules   service.RuleStore
		alerts  service.AlertStore
		servers service.ServerStore
		closer  func()
	)

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL != "" {
		db, err := postgres.New(ctx, dbURL, logger)
		if err != nil {
			return fmt.Errorf("connecting to postgres: %w", err)
		}
		events, rules, alerts, servers = db.Events(), db.Rules(), db.Alerts(), db.Servers()
		closer = func() { db.Close() }
	} else {
		dbPath, err := datadir.DBPath("shield.db")
		if err != nil {
			return fmt.Errorf("resolving data dir: %w", err)
		}
		db, err := sqlitestore.New(dbPath, logger)
		if err != nil {
			return fmt.Errorf("opening sqlite: %w", err)
		}
		events, rules, alerts = db.Events(), db.Rules(), db.Alerts()
		servers = db.Servers()
		closer = func() { db.Close() } //nolint:errcheck // db close error on shutdown is non-critical
	}
	defer closer()

	// Build alert notifiers from config.
	notifiers := buildNotifiers(cfg, logger)
	dispatcher := alert.NewDispatcher(notifiers, logger)

	// Create the Shield service.
	shield := service.NewShield(cfg, events, rules, alerts, servers, dispatcher, logger)

	// Start the shield engine.
	if err := shield.Start(ctx); err != nil {
		return fmt.Errorf("starting shield: %w", err)
	}

	// Start the HTTP API server.
	listenAddr := os.Getenv("SHIELD_LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = ":8081"
	}

	router := api.NewRouter(shield, logger)
	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		logger.Info().Str("addr", listenAddr).Msg("shield API server starting")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error().Err(err).Msg("API server error")
		}
	}()

	sig := <-sigCh
	logger.Info().Str("signal", sig.String()).Msg("shutting down")

	shield.Stop()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutting down server: %w", err)
	}

	logger.Info().Msg("shield stopped cleanly")
	return nil
}

func buildNotifiers(cfg *config.Config, logger zerolog.Logger) []alert.Notifier {
	var notifiers []alert.Notifier

	for _, ch := range cfg.Global.AlertChannels {
		switch ch.Type {
		case "slack":
			webhook := config.ExpandEnvVars(ch.Webhook)
			if webhook != "" {
				notifiers = append(notifiers, alert.NewSlackNotifier(webhook, logger))
			}
		case "webhook":
			url := config.ExpandEnvVars(ch.URL)
			if url != "" {
				notifiers = append(notifiers, alert.NewWebhookNotifier(url, logger))
			}
		}
	}
	return notifiers
}
