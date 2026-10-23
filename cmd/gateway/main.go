package main

import (
	"context"
	"errors"
	"fmt"
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

var serviceNames = []string{"iam", "member", "address", "catalog", "media", "interaction", "engagement", "search", "recommendation", "content", "analytics", "cart", "pricing", "checkout", "order", "campaign", "coupon", "points", "group", "flash", "inventory", "fulfillment", "logistics", "aftersale", "payment", "refund", "invoice", "reconciliation", "notification", "helpdesk"}

func newHandler(rpc platform.Caller, demo bool) http.Handler {
	return httpgateway.NewHandler(rpc, demo, env("FRONTEND_DIR", "web/dist"))
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	token := os.Getenv("INTERNAL_RPC_TOKEN")
	if token == "" {
		slog.Error("INTERNAL_RPC_TOKEN must be set")
		os.Exit(1)
	}
	shutdown, err := platform.InitTelemetry(ctx, "gateway", os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	if err != nil {
		slog.Error("telemetry init failed", "error", err)
		os.Exit(1)
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdown(c)
	}()
	rpc := platform.NewCaller(targets(), token)
	srv := &http.Server{Addr: env("HTTP_ADDR", ":8080"), Handler: newHandler(rpc, env("DEMO_MODE", "false") == "true"), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(c)
	}()
	slog.Info("gateway listening", "addr", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("gateway failed", "error", err)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
