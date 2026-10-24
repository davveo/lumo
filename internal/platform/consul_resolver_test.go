package platform

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type resolverCatalog struct {
	mu        sync.Mutex
	addresses []string
	failure   bool
	requests  atomic.Int64
}

func (c *resolverCatalog) set(addresses []string, failure bool) {
	c.mu.Lock()
	c.addresses, c.failure = append([]string(nil), addresses...), failure
	c.mu.Unlock()
}

func (c *resolverCatalog) serve(w http.ResponseWriter, r *http.Request) {
	c.requests.Add(1)
	if r.URL.Path != "/v1/health/service/catalog" || r.URL.Query().Get("passing") != "true" {
		http.Error(w, "unexpected discovery request", http.StatusBadRequest)
		return
	}
	c.mu.Lock()
	addresses, failure := append([]string(nil), c.addresses...), c.failure
	c.mu.Unlock()
	if failure {
		http.Error(w, "registry unavailable", http.StatusServiceUnavailable)
		return
	}
	rows := make([]map[string]any, 0, len(addresses))
	for i, address := range addresses {
		host, portText, _ := net.SplitHostPort(address)
		port, _ := strconv.Atoi(portText)
		id := "catalog-" + strconv.Itoa(i)
		rows = append(rows, map[string]any{
			"Node":    map[string]any{"Address": "127.0.0.1"},
			"Service": map[string]any{"ID": id, "Service": "catalog", "Address": host, "Port": port},
			"Checks":  []map[string]any{{"CheckID": "service:" + id, "ServiceID": id, "Status": "passing"}},
		})
	}
	_ = json.NewEncoder(w).Encode(rows)
}

func resolverBackend(t *testing.T, name string) (string, *atomic.Int64) {
	address, calls, _ := controllableResolverBackend(t, name)
	return address, calls
}

func controllableResolverBackend(t *testing.T, name string) (string, *atomic.Int64, func()) {
	t.Helper()
	calls := &atomic.Int64{}
	server, err := NewRPCServer(
		"resolver-test-token",
		Module{Name: "catalog", Operations: map[string]Operation{"get": func(context.Context, Request) (Result, error) {
			calls.Add(1)
			return Result{"backend": name}, nil
		}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	return listener.Addr().String(), calls, server.Stop
}

func resolverCaller(t *testing.T, endpoint string, targets map[string]string) *rpcCaller {
	t.Helper()
	consul, err := NewConsul(endpoint, "")
	if err != nil {
		t.Fatal(err)
	}
	caller := NewCallerWithConsul(targets, "resolver-test-token", consul).(*rpcCaller)
	caller.consul.pollInterval = 20 * time.Millisecond
	t.Cleanup(func() { _ = caller.Close() })
	return caller
}

func callResolverBackend(caller Caller) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	result, err := caller.Call(ctx, "catalog", Command{Action: "get"})
	if err != nil {
		return "", err
	}
	return Text(result, "backend"), nil
}

func awaitResolver(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for " + what)
}

func TestConsulRPCRefreshesAndRemovesPreviouslyPassingInstances(t *testing.T) {
	first, firstCalls := resolverBackend(t, "first")
	second, secondCalls := resolverBackend(t, "second")
	catalog := &resolverCatalog{}
	catalog.set([]string{first}, false)
	registry := httptest.NewServer(http.HandlerFunc(catalog.serve))
	defer registry.Close()
	caller := resolverCaller(t, registry.URL, map[string]string{"catalog": "consul:///catalog"})
	awaitResolver(
		t,
		"initial Consul backend",
		func() bool { got, err := callResolverBackend(caller); return err == nil && got == "first" },
	)

	// The old TCP endpoint remains alive: switching must follow Consul rather
	// than merely relying on connection failure to discover a replacement.
	catalog.set([]string{second}, false)
	awaitResolver(
		t,
		"replacement Consul backend",
		func() bool { got, err := callResolverBackend(caller); return err == nil && got == "second" },
	)
	for range 5 {
		got, err := callResolverBackend(caller)
		if err != nil || got != "second" {
			t.Fatalf("removed backend was still callable: %q, %v", got, err)
		}
	}

	for _, registryFailure := range []bool{false, true} {
		// Both a passing=[] result and an HTTP failure must withdraw the
		// previously connected endpoint. The real RPC must stop reaching it.
		catalog.set(nil, registryFailure)
		awaitResolver(
			t,
			"no usable discovered endpoint",
			func() bool { _, err := callResolverBackend(caller); return status.Code(err) == codes.Unavailable },
		)
		before := firstCalls.Load() + secondCalls.Load()
		for range 5 {
			if _, err := callResolverBackend(caller); status.Code(err) != codes.Unavailable {
				t.Fatalf("discovery failure did not fail closed: %v", err)
			}
		}
		if after := firstCalls.Load() + secondCalls.Load(); before != after {
			t.Fatalf("removed instances received calls: before=%d after=%d", before, after)
		}
		catalog.set([]string{first}, false)
		awaitResolver(
			t,
			"registry recovery",
			func() bool { got, err := callResolverBackend(caller); return err == nil && got == "first" },
		)
	}

	catalog.set([]string{first, second}, false)
	seen := map[string]bool{}
	awaitResolver(t, "round robin over both passing instances", func() bool {
		got, err := callResolverBackend(caller)
		if err == nil {
			seen[got] = true
		}
		return seen["first"] && seen["second"]
	})
}

func TestConsulCallerPreservesExplicitStaticTargets(t *testing.T) {
	address, _ := resolverBackend(t, "explicit-static")
	catalog := &resolverCatalog{}
	registry := httptest.NewServer(http.HandlerFunc(catalog.serve))
	defer registry.Close()
	caller := resolverCaller(t, registry.URL, map[string]string{"catalog": address})
	got, err := callResolverBackend(caller)
	if err != nil || got != "explicit-static" {
		t.Fatalf("static override failed: %q, %v", got, err)
	}
	if catalog.requests.Load() != 0 {
		t.Fatal("explicit override queried Consul")
	}
	_ = caller.Close()
	if _, err := callResolverBackend(caller); status.Code(err) != codes.Unavailable {
		t.Fatalf("closed caller reopened a connection: %v", err)
	}
	missingClient := NewCaller(map[string]string{"catalog": "consul:///catalog"}, "resolver-test-token")
	if _, err := callResolverBackend(missingClient); status.Code(err) != codes.Unavailable {
		t.Fatalf("unconfigured discovery unexpectedly succeeded: %v", err)
	}
}

func TestConsulRPCBalancesScaleOutWithdrawalAndConnectionFailure(t *testing.T) {
	first, firstCalls := resolverBackend(t, "first")
	second, _, stopSecond := controllableResolverBackend(t, "second")
	third, thirdCalls := resolverBackend(t, "third")
	catalog := &resolverCatalog{}
	catalog.set([]string{first, second}, false)
	registry := httptest.NewServer(http.HandlerFunc(catalog.serve))
	defer registry.Close()
	caller := resolverCaller(t, registry.URL, map[string]string{"catalog": "consul:///catalog"})

	warm := func(names ...string) {
		t.Helper()
		seen := map[string]bool{}
		awaitResolver(t, "ready replicas", func() bool {
			name, err := callResolverBackend(caller)
			if err == nil {
				seen[name] = true
			}
			for _, name := range names {
				if !seen[name] {
					return false
				}
			}
			return true
		})
	}
	assertDistribution := func(total int, want map[string]int) {
		t.Helper()
		got := map[string]int{}
		for range total {
			name, err := callResolverBackend(caller)
			if err != nil {
				t.Fatalf("ready replica call failed: %v", err)
			}
			got[name]++
		}
		if len(got) != len(want) {
			t.Fatalf("unexpected replica distribution: got=%v want=%v", got, want)
		}
		for name, count := range want {
			if got[name] != count {
				t.Fatalf("round robin did not distribute evenly: got=%v want=%v", got, want)
			}
		}
	}

	warm("first", "second")
	assertDistribution(20, map[string]int{"first": 10, "second": 10})
	if thirdCalls.Load() != 0 {
		t.Fatal("unregistered third replica received traffic")
	}

	// Scale out without recreating the caller or its cached gRPC connection.
	catalog.set([]string{first, second, third}, false)
	warm("first", "second", "third")
	assertDistribution(30, map[string]int{"first": 10, "second": 10, "third": 10})

	// Keep the withdrawn server listening. It must disappear from the picker
	// because Consul removed it, rather than because its TCP connection broke.
	catalog.set([]string{second, third}, false)
	awaitResolver(t, "withdrawn replica to stop receiving traffic", func() bool {
		before := firstCalls.Load()
		for range 6 {
			if _, err := callResolverBackend(caller); err != nil {
				return false
			}
		}
		return before == firstCalls.Load()
	})
	before := firstCalls.Load()
	assertDistribution(20, map[string]int{"second": 10, "third": 10})
	if firstCalls.Load() != before {
		t.Fatal("withdrawn live replica received another request")
	}

	// A disconnected endpoint stops being READY even before the next Consul
	// health check has removed its otherwise still-passing registration.
	stopSecond()
	awaitResolver(t, "remaining connected replica", func() bool {
		for range 6 {
			name, err := callResolverBackend(caller)
			if err != nil || name != "third" {
				return false
			}
		}
		return true
	})
	assertDistribution(20, map[string]int{"third": 20})
}

func TestConsulCallerCloseCancelsInFlightResolution(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(cancelled)
	}))
	defer registry.Close()
	caller := resolverCaller(t, registry.URL, map[string]string{"catalog": "consul:///catalog"})
	callDone := make(chan struct{})
	go func() { _, _ = callResolverBackend(caller); close(callDone) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("resolution did not start")
	}
	closeDone := make(chan struct{})
	go func() { _ = caller.Close(); close(closeDone) }()
	for _, done := range []<-chan struct{}{cancelled, closeDone, callDone} {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("close did not stop outstanding discovery/RPC work")
		}
	}
}
