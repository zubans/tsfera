package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zubans/tsfera/backend/pkg/tsfera"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dsn := envOr("DATABASE_URL", "postgres://tsfera:tsfera@localhost:5432/tsfera?sslmode=disable")
	pool, err := tsfera.OpenPool(ctx, dsn)
	if err != nil {
		logger.Error("failed to connect to postgres", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	cfg := tsfera.Config{
		Pool:                   pool,
		Logger:                 logger,
		StorageDir:             envOr("TSFERA_STORAGE_DIR", "./data/attachments"),
		SessionTTL:             30 * 24 * time.Hour,
		MTSWebhookSecret:       os.Getenv("MTS_WEBHOOK_SECRET"),
		MTSBaseURL:             os.Getenv("MTS_BASE_URL"),
		MTSAPIToken:            os.Getenv("MTS_API_TOKEN"),
		DaDataAPIKey:           os.Getenv("DADATA_API_KEY"),
		DaDataBaseURL:          os.Getenv("DADATA_BASE_URL"),
		CloudPaymentsPublicID:  os.Getenv("CLOUDPAYMENTS_PUBLIC_ID"),
		CloudPaymentsAPISecret: os.Getenv("CLOUDPAYMENTS_API_SECRET"),
		CloudPaymentsBaseURL:   os.Getenv("CLOUDPAYMENTS_BASE_URL"),
		EnableDocsAndAI:        envOr("TSFERA_ENABLE_DOCS_AI", "true") != "false",
	}
	if adminPass := envOr("TSFERA_ADMIN_PASSWORD", "admin12345"); adminPass != "" {
		cfg.BootstrapAdmin = &tsfera.BootstrapAdminConfig{
			Username: envOr("TSFERA_ADMIN_USER", "admin"),
			Password: adminPass,
		}
	}

	plat, err := tsfera.NewPlatform(ctx, cfg)
	if err != nil {
		logger.Error("failed to initialize platform", "error", err)
		os.Exit(1)
	}

	if err := plat.Migrate(ctx); err != nil {
		logger.Error("migration failed", "error", err)
		os.Exit(1)
	}

	plat.StartWorkers(ctx)

	addr := envOr("HTTP_ADDR", ":8080")
	srv := &http.Server{
		Addr:              addr,
		Handler:           plat.Handler(os.Getenv("TSFERA_SPA_DIR")),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("tsfera server listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server failed", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down gracefully")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	plat.WaitWorkers()
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
