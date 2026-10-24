// devstack is a frontend integration runtime, not a substitute for Docker
// infrastructure verification. It uses 30 isolated SQLite databases, real TCP
// gRPC and a durable local event dispatcher. Run Docker Compose for MySQL,
// Redis, RabbitMQ, private MinIO and Collector/Jaeger.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"github.com/davveo/lumo/internal/devstack"
	"github.com/davveo/lumo/internal/httpgateway"
	"github.com/davveo/lumo/internal/platform"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func main() {
	if err := run(); err != nil {
		slog.Error("devstack stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// This binary explicitly enables seeded demo providers; it is never used by
	// the Docker entrypoint or a production service process.
	if err := os.Setenv("DEMO_MODE", "true"); err != nil {
		return err
	}
	shutdown, err := platform.InitTelemetry(ctx, "devstack", os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	if err != nil {
		return err
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdown(c)
	}()
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return err
	}
	stack, err := devstack.Start(ctx, env("DEVSTACK_DATA_DIR", ".local/devstack"), hex.EncodeToString(b))
	if err != nil {
		return err
	}
	defer stack.Close()
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		tickCount := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := stack.Pump(ctx); err != nil && ctx.Err() == nil {
					slog.Warn("local outbox retry", "error", err)
				}
				tickCount++
				if tickCount%4 == 0 {
					if err := stack.Tick(ctx); err != nil && ctx.Err() == nil {
						slog.Warn("local scheduled retry", "error", err)
					}
				}
			}
		}
	}()
	srv := &http.Server{
		Addr:              env("HTTP_ADDR", "127.0.0.1:8080"),
		Handler:           httpgateway.NewHandler(stack.RPC, true, env("FRONTEND_DIR", "web/dist")),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(c)
	}()
	slog.Warn(
		"DEVELOPMENT runtime: SQLite + TCP gRPC + durable local dispatcher; external infrastructure is not running",
		"addr",
		srv.Addr,
	)
	err = srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
