package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/apperr"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/assignment"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/audit"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/config"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/store"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/worker"
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

	db, err := store.OpenDB(cfg.DBPath())
	if err != nil {
		logger.Error("failed to open database", err)
		os.Exit(1)
	}
	defer db.Close()

	st := store.NewSQLiteStore(db)
	auditRecorder := audit.New(st, clock)

	taskSvc := assignment.New(assignment.Deps{
		Tasks:        st,
		Leases:       st,
		Executions:   st,
		Audit:        auditRecorder,
		Clock:        clock,
		Logger:       logger,
		LeaseTimeout: cfg.Scheduler.LeaseTimeout,
	})

	w := worker.New(worker.Deps{
		TaskService:  taskSvc,
		Clock:        clock,
		Logger:       logger,
		ID:           cfg.Executor.ID,
		PollInterval: cfg.Executor.PollInterval,
		BatchSize:    cfg.Executor.BatchSize,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		logger.Info("shutdown signal received")
		w.Stop()
		cancel()
	}()

	logger.Info("embodied robot fleet executor starting",
		apperr.F("executor_id", cfg.Executor.ID),
		apperr.F("poll_interval", cfg.Executor.PollInterval))

	w.Start(ctx)

	<-ctx.Done()
	time.Sleep(500 * time.Millisecond)
}
