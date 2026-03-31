package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	logger "github.com/Gratheon/log-lib-go"

	"legal-doc-intel/go-api/internal/app"
	"legal-doc-intel/go-api/internal/config"
	"legal-doc-intel/go-api/internal/logging"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", err)
		os.Exit(1)
	}

	appLogger := logging.New(logger.Configure(logger.LoggerConfig{
		LogLevel: cfg.LogLevel,
	}))

	application, err := app.New(cfg, appLogger)
	if err != nil {
		appLogger.Error("failed to initialize app", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := application.Run(ctx); err != nil {
		appLogger.Error("application exited with error", "error", err)
		os.Exit(1)
	}
}
