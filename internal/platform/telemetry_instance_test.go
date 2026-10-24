package platform

import "testing"

func TestTelemetryIdentityMatchesEachRegisteredReplica(t *testing.T) {
	t.Setenv("CONSUL_SERVICE_ID", "")
	t.Setenv("CONSUL_SERVICE_ADDRESS", "")
	t.Setenv("RPC_ADDR", ":19001")
	t.Setenv("HTTP_ADDR", ":18080")
	first := telemetryInstanceID("catalog")
	for _, service := range []struct{ name, listener, protocol string }{
		{"catalog", ":19001", "grpc"}, {"gateway", ":18080", "http"},
	} {
		spec, err := consulServiceFromEnv(service.name, service.listener, service.protocol)
		if err != nil {
			t.Fatal(err)
		}
		if got := telemetryInstanceID(service.name); got != spec.ID {
			t.Fatalf("trace instance %q differs from Consul instance %q", got, spec.ID)
		}
	}
	t.Setenv("RPC_ADDR", ":019001")
	if got := telemetryInstanceID("catalog"); got != first {
		t.Fatalf("equivalent listener ports have different identities: %q versus %q", got, first)
	}
	t.Setenv("RPC_ADDR", ":19002")
	if second := telemetryInstanceID("catalog"); second == first {
		t.Fatal("replicas on different ports have the same telemetry identity")
	}
	t.Setenv("CONSUL_SERVICE_ID", "catalog-explicit-replica")
	if got := telemetryInstanceID("catalog"); got != "catalog-explicit-replica" {
		t.Fatalf("explicit registration identity not reflected in telemetry: %q", got)
	}
}
