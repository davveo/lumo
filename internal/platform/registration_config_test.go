package platform

import (
	"context"
	"os"
	"testing"
)

func TestRegistrationUsesReachableAdvertisedAddressAndListenerPort(t *testing.T) {
	t.Setenv("CONSUL_SERVICE_ADDRESS", "catalog-instance.internal")
	t.Setenv("CONSUL_SERVICE_ID", "catalog-instance")
	spec, err := consulServiceFromEnv("catalog", "0.0.0.0:9000", "grpc")
	if err != nil || spec.Address != "catalog-instance.internal" || spec.Port != 9000 ||
		spec.ID != "catalog-instance" ||
		spec.Protocol != "grpc" {
		t.Fatalf("unexpected advertised service: %+v, %v", spec, err)
	}
	t.Setenv("CONSUL_SERVICE_ADDRESS", "")
	t.Setenv("CONSUL_SERVICE_ID", "")
	hostname, _ := os.Hostname()
	spec, err = consulServiceFromEnv("gateway", ":8080", "http")
	if err != nil || spec.Address != hostname || spec.ID != "gateway-"+hostname+"-8080" {
		t.Fatalf("wildcard listener was not replaced by hostname: %+v, %v", spec, err)
	}
	spec, err = consulServiceFromEnv("gateway", "[::1]:8081", "http")
	if err != nil || spec.Address != "::1" || spec.Port != 8081 {
		t.Fatalf("IPv6 listener not preserved: %+v, %v", spec, err)
	}
	if spec.ID != "gateway-"+hostname+"-8081" {
		t.Fatalf("instances sharing a hostname need port-specific IDs: %+v", spec)
	}
}

func TestRegistrationRejectsUnusableListenerPorts(t *testing.T) {
	for _, addr := range []string{"missing-port", ":0", ":65536", ":http"} {
		if _, err := consulServiceFromEnv("catalog", addr, "grpc"); err == nil {
			t.Errorf("accepted unusable listener %q", addr)
		}
	}
	cleanup, err := RegisterConsulService(context.Background(), nil, "catalog", ":0", "grpc")
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
}
