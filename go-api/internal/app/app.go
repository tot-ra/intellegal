package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"legal-doc-intel/go-api/internal/ai"
	"legal-doc-intel/go-api/internal/config"
	"legal-doc-intel/go-api/internal/db"
	"legal-doc-intel/go-api/internal/externalcopy"
	"legal-doc-intel/go-api/internal/http/handlers"
	httprouter "legal-doc-intel/go-api/internal/http/router"
	"legal-doc-intel/go-api/internal/logging"
	"legal-doc-intel/go-api/internal/storage"
)

type App struct {
	cfg     config.Config
	logger  logging.Logger
	server  *http.Server
	db      *db.Postgres
	storage storage.Adapter
}

func New(cfg config.Config, logger logging.Logger) (*App, error) {
	store, err := storage.NewAdapter(storage.FactoryConfig{
		Provider:           cfg.StorageProvider,
		LocalPath:          cfg.LocalStoragePath,
		AzureAccountName:   cfg.AzureStorageAccount,
		AzureBlobContainer: cfg.AzureBlobContainer,
		MinIOEndpoint:      cfg.MinIOEndpoint,
		MinIOAccessKey:     cfg.MinIOAccessKey,
		MinIOSecretKey:     cfg.MinIOSecretKey,
		MinIOBucket:        cfg.MinIOBucket,
		MinIOUseSSL:        cfg.MinIOUseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize storage adapter: %w", err)
	}

	pg, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	if err := pg.PingWithTimeout(cfg.DatabasePingTimeout); err != nil {
		_ = pg.Close()
		return nil, fmt.Errorf("postgres ping failed: %w", err)
	}

	aiClient := ai.NewClient(cfg.InternalAIBaseURL, cfg.InternalServiceToken, cfg.InternalAITimeout)
	copyClient := externalcopy.NewClient(cfg.ExternalCopyBaseURL, cfg.ExternalCopyToken, cfg.ExternalCopyTimeout, cfg.ExternalCopyRetries)
	api := handlers.NewAPI(logger, aiClient, store, copyClient)
	router := httprouter.New(logger, api, func(ctx context.Context) error {
		pingCtx, cancel := context.WithTimeout(ctx, cfg.DatabasePingTimeout)
		defer cancel()
		return pg.Ping(pingCtx)
	}, cfg.CORSAllowedOrigins)

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	logger.Info("storage adapter configured", "provider", cfg.StorageProvider)

	return &App{cfg: cfg, logger: logger, server: server, db: pg, storage: store}, nil
}

func (a *App) Run(ctx context.Context) error {
	errCh := make(chan error, 1)

	go func() {
		a.logger.Info("go-api listening", "addr", a.server.Addr)
		if err := a.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), a.cfg.ShutdownGracePeriod)
		defer cancel()
		a.logger.Info("shutdown signal received")
		err := a.server.Shutdown(shutdownCtx)
		closeErr := a.db.Close()
		if err != nil {
			return err
		}
		return closeErr
	case err := <-errCh:
		_ = a.db.Close()
		return err
	}
}
