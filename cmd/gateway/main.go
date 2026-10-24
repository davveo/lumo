package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/httpgateway"
	"github.com/davveo/lumo/internal/platform"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

var serviceNames = []string{
	"iam", "member", "address", "catalog",
	"media", "interaction", "engagement",
	"search", "recommendation", "content",
	"analytics", "cart", "pricing",
	"checkout", "order", "campaign",
	"coupon", "points", "group", "flash",
	"inventory", "fulfillment", "logistics",
	"aftersale", "payment", "refund", "invoice",
	"reconciliation", "notification", "helpdesk",
}

func newHandler(rpc platform.Caller, demo bool) http.Handler {
	return httpgateway.NewHandler(rpc, demo, "")
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
func targets() map[string]string {
	out := map[string]string{}
	for _, s := range serviceNames {
		out[s] = s + ":9000"
		if strings.TrimSpace(os.Getenv("CONSUL_HTTP_ADDR")) != "" {
			out[s] = "consul:///" + s
		}
	}
	if raw := os.Getenv("RPC_TARGETS"); raw != "" {
		for _, entry := range strings.Split(raw, ",") {
			p := strings.SplitN(entry, "=", 2)
			if len(p) == 2 {
				out[p[0]] = p[1]
			}
		}
	}
	return out
}

func healthcheckURL(addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("invalid HTTP_ADDR: %w", err)
	}
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	} else if host == "::" {
		host = "::1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/readyz", nil
}

func healthcheck() error {
	target, err := healthcheckURL(env("HTTP_ADDR", ":8080"))
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(target)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gateway not ready: %s", resp.Status)
	}
	return nil
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := healthcheck(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		slog.Error("gateway stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	token := os.Getenv("INTERNAL_RPC_TOKEN")
	if token == "" {
		return fmt.Errorf("INTERNAL_RPC_TOKEN must be set")
	}
	shutdown, err := platform.InitTelemetry(ctx, constants.ServiceGateway, os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	if err != nil {
		return fmt.Errorf("telemetry init failed: %w", err)
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdown(c)
	}()
	consul, err := platform.ConsulFromEnv()
	if err != nil {
		return fmt.Errorf("Consul configuration: %w", err)
	}
	rpc := platform.NewCallerWithConsul(targets(), token, consul)
	defer func() {
		if closer, ok := rpc.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}()
	unregister, err := platform.RegisterConsulService(ctx,
		consul, constants.ServiceGateway, env("HTTP_ADDR", ":8080"), "http")
	if err != nil {
		return fmt.Errorf("Consul registration: %w", err)
	}
	defer unregister()
	srv := &http.Server{
		Addr:              env("HTTP_ADDR", ":8080"),
		Handler:           newHandler(rpc, env("DEMO_MODE", "false") == "true"),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx }}
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(c)
	}()
	slog.Info("gateway listening", "addr", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
