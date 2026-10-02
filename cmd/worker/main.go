package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	"github.com/Adeyinka7789/ordora/internal/config"
	"github.com/Adeyinka7789/ordora/internal/infra/email"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
	"github.com/Adeyinka7789/ordora/internal/jobs"
)

func main() {
	_ = godotenv.Load()
	setupLogging()
	if err := run(); err != nil {
		slog.Error("worker: fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := postgres.Open(ctx, cfg.DB.DSN())
	if err != nil {
		return err
	}
	defer db.Close()
	slog.Info("worker: db connected")

	mailer, err := email.NewFromConfig(cfg.Email)
	if err != nil {
		return err
	}

	renderer, err := email.NewRenderer()
	if err != nil {
		return err
	}

	relay := jobs.NewOutboxRelay(db, jobs.OutboxRelayConfig{})
	worker := jobs.NewNotificationWorker(db, renderer, mailer, jobs.NotificationWorkerConfig{})

	go relay.Run(ctx)
	go worker.Run(ctx)

	<-ctx.Done()
	slog.Info("worker: shutting down")
	return nil
}

func setupLogging() {
	env := os.Getenv("ORDORA_ENV")
	var h slog.Handler
	if env == "production" {
		h = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	} else {
		h = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
	}
	slog.SetDefault(slog.New(h))
}
