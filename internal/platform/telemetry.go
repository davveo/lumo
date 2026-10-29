package platform

import (
	"github.com/davveo/lumo/internal/constants"

	"context"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func InitTelemetry(ctx context.Context, service, endpoint string) (func(context.Context) error, error) {
	ratio := 1.0
	if raw := os.Getenv("OTEL_TRACES_SAMPLER_ARG"); raw != "" {
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(v) || v < 0 || v > 1 {
			return nil, fmt.Errorf("invalid OTEL_TRACES_SAMPLER_ARG")
		}
		ratio = v
	}
	instanceID := telemetryInstanceID(service)
	opts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(
			resource.NewWithAttributes(
				"",
				attribute.String("service.name", service),
				attribute.String("service.instance.id", instanceID),
				attribute.String("service.version", "0.1.0"),
				attribute.String("deployment.environment.name", os.Getenv("APP_ENV")),
			),
		),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))),
	}
	if endpoint != "" {
		exporterOptions, err := traceExporterOptions(endpoint)
		if err != nil {
			return nil, err
		}
		exporter, err := otlptracegrpc.New(ctx, exporterOptions...)
		if err != nil {
			return nil, fmt.Errorf("create OTLP exporter: %w", err)
		}
		opts = append(opts, sdktrace.WithBatcher(exporter))
	}
	tp := sdktrace.NewTracerProvider(opts...)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	slog.SetDefault(
		slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})).
			With("service", service, "instance_id", instanceID),
	)
	return tp.Shutdown, nil
}

func traceExporterOptions(endpoint string) ([]otlptracegrpc.Option, error) {
	endpoint = strings.TrimSpace(endpoint)
	if strings.Contains(endpoint, "://") {
		parsed, err := url.Parse(endpoint)
		if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
			parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return nil, fmt.Errorf("invalid OTLP endpoint URL")
		}
		// WithEndpointURL retains the explicit HTTPS transport and the SDK's
		// configured CA/client certificates. Do not downgrade it to WithInsecure.
		return []otlptracegrpc.Option{otlptracegrpc.WithEndpointURL(endpoint)}, nil
	}
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil || host == "" || port == "" {
		return nil, fmt.Errorf("OTLP endpoint must be an HTTP(S) URL or host:port")
	}
	// The deployment's unprefixed collector:4317 endpoint remains plaintext.
	return []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(endpoint), otlptracegrpc.WithInsecure()}, nil
}

// Use the same identity as registration so traces and logs can distinguish
// replicas and be matched directly to an instance in the Consul catalog.
func telemetryInstanceID(service string) string {
	listener := os.Getenv("RPC_ADDR")
	if listener == "" {
		listener = ":9000"
	}
	if service == constants.ServiceGateway {
		listener = os.Getenv("HTTP_ADDR")
		if listener == "" {
			listener = ":8080"
		}
	}
	_, port, err := net.SplitHostPort(listener)
	if err != nil {
		port = "process-" + strconv.Itoa(os.Getpid())
	} else if numericPort, parseErr := strconv.Atoi(port); parseErr == nil {
		port = strconv.Itoa(numericPort)
	}
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = ID("instance")
	}
	return consulInstanceID(service, hostname, port)
}

// Start adds a code-layer span. Pass the returned context to every RPC,
// repository, transaction and goroutine that is part of this operation.
func Start(ctx context.Context, name string) (context.Context, trace.Span) {
	layer := "application"
	for _, candidate := range []string{"transport", "usecase", "repository", "worker", "rpc", "mq"} {
		if strings.Contains(name, candidate) {
			layer = candidate
			break
		}
	}
	return otel.Tracer("github.com/davveo/lumo").
		Start(ctx, name, trace.WithAttributes(attribute.String("code.layer", layer)))
}

// Log associates a structured log with the active span without logging payloads.
func Log(ctx context.Context, level slog.Level, message string, attrs ...any) {
	sc := trace.SpanContextFromContext(ctx)
	if sc.IsValid() {
		attrs = append(attrs, "trace_id", sc.TraceID().String(), "span_id", sc.SpanID().String())
	}
	slog.Log(ctx, level, message, attrs...)
}
