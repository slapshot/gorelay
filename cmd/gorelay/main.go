// GoRelay admission and opt-in pre-alpha delivery service.
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

	"github.com/slapshot/gorelay/internal/api"
	"github.com/slapshot/gorelay/internal/config"
	"github.com/slapshot/gorelay/internal/delivery"
	"github.com/slapshot/gorelay/internal/security"
	"github.com/slapshot/gorelay/internal/store/postgres"
	"github.com/slapshot/gorelay/internal/worker"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("fatal", "reason", err.Error())
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return errors.New("invalid startup configuration (check environment and client config)")
	}
	auth, err := security.NewAuthenticator(cfg.Credentials)
	if err != nil {
		return errors.New("invalid client authentication configuration")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	store, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer store.Close()
	if cfg.AllowDeliveryPreview {
		dispatcher, err := delivery.New(cfg.Targets, 10*time.Second)
		if err != nil {
			return errors.New("invalid outbound destination configuration")
		}
		defer dispatcher.Close()
		pool, err := worker.New(store, dispatcher, worker.Config{
			Concurrency: 4,
			Lease:       30 * time.Second,
			HTTPTimeout: 10 * time.Second,
			Poll:        2 * time.Second,
		}, logger)
		if err != nil {
			return errors.New("invalid worker configuration")
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			pool.Run(ctx)
		}()
		defer func() {
			stop()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				logger.Warn("worker drain deadline exceeded")
			}
		}()
	}
	apiServer, err := api.New(store, auth, cfg.MaxBodyBytes, cfg.AllowAdmissionPreview)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           apiServer.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    8192,
	}
	errs := make(chan error, 1)
	go func() { errs <- srv.ListenAndServe() }()
	logger.Info("gorelay started", "listen", cfg.ListenAddr, "status", "pre-alpha", "outbound_preview", cfg.AllowDeliveryPreview)
	select {
	case <-ctx.Done():
		apiServer.Drain()
		drainCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := srv.Shutdown(drainCtx); err != nil {
			return errors.New("HTTP shutdown failed")
		}
		logger.Info("gorelay stopped")
		return nil
	case err := <-errs:
		if !errors.Is(err, http.ErrServerClosed) {
			return errors.New("HTTP listener terminated")
		}
		return nil
	}
}
