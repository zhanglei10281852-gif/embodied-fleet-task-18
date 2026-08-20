package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/apperr"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/config"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/server"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/store"
)

func main() {
	cfg := config.FromEnv()
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}
	if err := cfg.EnsureDataDir(); err != nil {
		fmt.Fprintf(os.Stderr, "data dir error: %v\n", err)
		os.Exit(1)
	}

	logger := apperr.NewLogger(cfg.Logging.Level, cfg.Logging.Pretty)
	clock := apperr.Default()

	db, err := store.Open(cfg.DBPath(), cfg.MigrationsSource())
	if err != nil {
		logger.Error("failed to open database", err)
		os.Exit(1)
	}
	defer db.Close()

	st := store.NewSQLiteStore(db)

	srv, err := server.New(server.Deps{
		Cfg:    cfg,
		Store:  st,
		Clock:  clock,
		Logger: logger,
	})
	if err != nil {
		logger.Error("failed to initialize services", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle OS signals for graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		logger.Info("shutdown signal received")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
		defer shutdownCancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("shutdown error", err)
		}
		cancel()
	}()

	logger.Info("embodied robot fleet scheduler starting",
		apperr.F("port", cfg.Server.Port),
		apperr.F("data_dir", cfg.Database.DataDir))

	if err := srv.Start(ctx); err != nil {
		logger.Error("server error", err)
		os.Exit(1)
	}

	// Give background goroutines a moment to finish.
	time.Sleep(500 * time.Millisecond)
}
