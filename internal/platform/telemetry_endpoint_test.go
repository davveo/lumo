package platform

import (
	"context"
	"crypto/tls"
	"encoding/pem"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

type endpointTraceCollector struct {
	collector.UnimplementedTraceServiceServer
	received chan bool
}

func (c *endpointTraceCollector) Export(
	ctx context.Context,
	_ *collector.ExportTraceServiceRequest,
) (*collector.ExportTraceServiceResponse, error) {
	p, _ := peer.FromContext(ctx)
	_, secure := p.AuthInfo.(credentials.TLSInfo)
	c.received <- secure
	return &collector.ExportTraceServiceResponse{}, nil
}

func TestTraceExporterHonorsHTTPSAndLegacyPlaintextCollector(t *testing.T) {
	// Reuse Go's local test certificate; no public network or real collector.
	certServer := httptest.NewTLSServer(nil)
	certificate := certServer.TLS.Certificates[0]
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certServer.Certificate().Raw})
	certServer.Close()
	caFile := filepath.Join(t.TempDir(), "collector-ca.pem")
	if err := os.WriteFile(caFile, certPEM, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OTEL_EXPORTER_OTLP_CERTIFICATE", caFile)
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_CERTIFICATE", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "true")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_INSECURE", "true")
	for _, scheme := range []string{"", "http://", "https://"} {
		t.Run("scheme="+scheme, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			secure := scheme == "https://"
			var serverOptions []grpc.ServerOption
			if secure {
				serverOptions = append(
					serverOptions,
					grpc.Creds(
						credentials.NewTLS(
							&tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12},
						),
					),
				)
			}
			server := grpc.NewServer(serverOptions...)
			service := &endpointTraceCollector{received: make(chan bool, 1)}
			collector.RegisterTraceServiceServer(server, service)
			go func() { _ = server.Serve(listener) }()
			t.Cleanup(server.Stop)
			// CA credentials are only intended for the HTTPS case. Unprefixed
			// and http endpoints retain the existing plaintext deployment path.
			if !secure {
				t.Setenv("OTEL_EXPORTER_OTLP_CERTIFICATE", "")
			}
			options, err := traceExporterOptions(scheme + listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			exporter, err := otlptracegrpc.New(ctx, options...)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = exporter.Shutdown(ctx) }()
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			defer func() { _ = provider.Shutdown(ctx) }()
			_, span := provider.Tracer("audit").Start(ctx, "endpoint-probe")
			span.End()
			if err = exporter.ExportSpans(ctx, recorder.Ended()); err != nil {
				t.Fatal(err)
			}
			select {
			case actualTLS := <-service.received:
				if actualTLS != secure {
					t.Fatalf("OTLP transport TLS=%v, expected %v for %q", actualTLS, secure, scheme)
				}
			case <-ctx.Done():
				t.Fatal("local collector did not receive the span")
			}
		})
	}
}

func TestTraceExporterRejectsMalformedOrAmbiguousEndpoints(t *testing.T) {
	for _, endpoint := range []string{"", "collector", "ftp://collector:4317", "https://", "https://user:secret@collector:4317", "https://collector:4317?key=value", "https://collector:4317#fragment"} {
		if _, err := traceExporterOptions(endpoint); err == nil {
			t.Fatalf("invalid exporter endpoint accepted: %q", endpoint)
		}
	}
	for _, value := range []string{"NaN", "+Inf", "-Inf"} {
		t.Setenv("OTEL_TRACES_SAMPLER_ARG", value)
		if _, err := InitTelemetry(context.Background(), "audit", ""); err == nil {
			t.Fatalf("non-finite sampling ratio accepted: %q", value)
		}
	}
}
