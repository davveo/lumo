package platform

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type consulTestAgent struct {
	mu         sync.Mutex
	stored     *consulRegistration
	puts, gets int
	token      string
	badToken   bool
}

func (a *consulTestAgent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if r.Header.Get("X-Consul-Token") != a.token {
		a.badToken = true
		w.WriteHeader(http.StatusForbidden)
		return
	}
	switch {
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/agent/service/"):
		a.gets++
		if a.stored == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		s := a.stored
		_ = json.NewEncoder(w).Encode(consulAgentService{ID: s.ID, Service: s.Name, Address: s.Address, Port: s.Port, Meta: s.Meta})
	case r.Method == http.MethodPut && r.URL.Path == "/v1/agent/service/register":
		if r.URL.Query().Get("replace-existing-checks") != "true" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var spec consulRegistration
		if json.NewDecoder(r.Body).Decode(&spec) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		a.puts++
		a.stored = &spec
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/v1/agent/service/deregister/"):
		a.stored = nil
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func consulEventually(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("Consul lifecycle did not converge")
}

func TestConsulEnvironmentAndEndpointValidation(t *testing.T) {
	t.Setenv("CONSUL_HTTP_ADDR", "")
	if c, err := ConsulFromEnv(); err != nil || c != nil {
		t.Fatalf("unset Consul must preserve standalone mode: %v, %v", c, err)
	}
	for _, address := range []string{"", "ftp://localhost:8500", "http://user:secret@localhost:8500", "http://localhost:8500?token=secret", "http://localhost:8500#fragment"} {
		if _, err := NewConsul(address, ""); err == nil {
			t.Errorf("invalid endpoint accepted: %q", address)
		}
	}
	t.Setenv("CONSUL_HTTP_ADDR", "localhost:8500")
	t.Setenv("CONSUL_HTTP_TOKEN", "test-acl-token")
	c, err := ConsulFromEnv()
	if err != nil || c.base.String() != "http://localhost:8500" || c.token != "test-acl-token" {
		t.Fatalf("environment configuration not applied: %v", err)
	}
}

func TestConsulResolveUsesPassingInstancesAndACL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/proxy/v1/health/service/catalog" || r.URL.Query().Get("passing") != "true" || r.Header.Get("X-Consul-Token") != "test-acl-token" {
			t.Errorf("incorrect discovery request: %s, token header present=%v", r.URL, r.Header.Get("X-Consul-Token") != "")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`[
{"Service":{"ID":"catalog-instance","Service":"catalog","Address":"192.0.2.1","Port":9000},"Checks":[{"CheckID":"service:catalog-instance","ServiceID":"catalog-instance","Status":"passing"}]},
{"Service":{"ID":"catalog-instance","Service":"catalog","Address":"192.0.2.1","Port":9000},"Checks":[{"CheckID":"service:catalog-instance","ServiceID":"catalog-instance","Status":"passing"}]},
{"Service":{"ID":"catalog-instance","Service":"catalog","Address":"2001:db8::1","Port":9000},"Checks":[{"CheckID":"service:catalog-instance","ServiceID":"catalog-instance","Status":"passing"}]},
{"Node":{"Address":"192.0.2.2"},"Service":{"ID":"catalog-instance","Service":"catalog","Port":9001},"Checks":[{"CheckID":"service:catalog-instance","ServiceID":"catalog-instance","Status":"passing"}]},
{"Service":{"Address":"192.0.2.3","Port":9000},"Checks":[{"Status":"critical"}]},
{"Service":{"Address":"192.0.2.4","Port":9000},"Checks":[{"Status":"warning"}]},
{"Service":{"Address":"192.0.2.5","Port":0}},
{"Service":{"Address":"0.0.0.0","Port":9000}},
{"Service":{"Service":"other","Address":"192.0.2.6","Port":9000}},
{"Service":{"ID":"no-health","Service":"catalog","Address":"192.0.2.7","Port":9000},"Checks":[]},
{"Service":{"ID":"serf-only","Service":"catalog","Address":"192.0.2.8","Port":9000},"Checks":[{"CheckID":"serfHealth","Status":"passing"}]}
]`))
	}))
	defer srv.Close()
	c, _ := NewConsul(srv.URL+"/proxy", "test-acl-token")
	addresses, err := c.Resolve(context.Background(), "catalog")
	if err != nil || strings.Join(addresses, ",") != "192.0.2.1:9000,192.0.2.2:9001,[2001:db8::1]:9000" {
		t.Fatalf("discovery = %v, %v", addresses, err)
	}
	if _, err = c.Resolve(context.Background(), "../catalog"); err == nil {
		t.Fatal("service path traversal accepted")
	}
}

func TestConsulRegistrationChecksRepairAndCleanup(t *testing.T) {
	for _, protocol := range []string{"grpc", "http"} {
		t.Run(protocol, func(t *testing.T) {
			agent := &consulTestAgent{token: "test-acl-token"}
			srv := httptest.NewServer(agent)
			defer srv.Close()
			c, _ := NewConsul(srv.URL, agent.token)
			c.registerInterval = 10 * time.Millisecond
			cleanup, err := c.Register(context.Background(), ConsulService{ID: "catalog-instance", Name: "catalog", Address: "::1", Port: 9000, Protocol: protocol})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(cleanup)
			agent.mu.Lock()
			check := agent.stored.Check
			agent.mu.Unlock()
			if check.Interval != "10s" || check.Timeout != "5s" || check.DeregisterCriticalServiceAfter != "2m" {
				t.Fatalf("active health check missing: %+v", check)
			}
			if protocol == "grpc" && check.GRPC != "[::1]:9000" || protocol == "http" && check.HTTP != "http://[::1]:9000/readyz" {
				t.Fatalf("wrong health target: %+v", check)
			}
			consulEventually(t, func() bool { agent.mu.Lock(); defer agent.mu.Unlock(); return agent.gets >= 3 })
			agent.mu.Lock()
			if agent.puts != 1 {
				t.Error("healthy registration must not be rewritten on refresh")
			}
			agent.stored = nil // Agent restart or external deletion.
			agent.mu.Unlock()
			consulEventually(t, func() bool { agent.mu.Lock(); defer agent.mu.Unlock(); return agent.puts == 2 })
			cleanup()
			cleanup() // Idempotent shutdown.
			time.Sleep(30 * time.Millisecond)
			agent.mu.Lock()
			defer agent.mu.Unlock()
			if agent.stored != nil || agent.puts != 2 || agent.badToken {
				t.Fatalf("shutdown resurrected instance or dropped ACL: %+v", agent)
			}
		})
	}
}

func TestConsulOldProcessCannotRemoveOrOverwriteItsSuccessor(t *testing.T) {
	agent := &consulTestAgent{}
	srv := httptest.NewServer(agent)
	defer srv.Close()
	c, _ := NewConsul(srv.URL, "")
	c.registerInterval = 10 * time.Millisecond
	service := ConsulService{ID: "catalog-instance", Name: "catalog", Address: "127.0.0.1", Port: 9000, Protocol: "grpc"}
	oldCleanup, err := c.Register(context.Background(), service)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(oldCleanup)
	newCleanup, err := c.Register(context.Background(), service)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(newCleanup)
	time.Sleep(30 * time.Millisecond)
	oldCleanup()
	agent.mu.Lock()
	if agent.stored == nil || agent.puts != 2 {
		t.Fatal("old process overwrote or deregistered newer registration")
	}
	agent.mu.Unlock()
	newCleanup()
}

func TestConsulFailuresEmptyResultsAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
		body string
	}{
		{"empty", 200, `[]`},
		{"unavailable", 503, `an arbitrary response must not enter error logs`},
		{"malformed", 200, `{`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.code); _, _ = w.Write([]byte(tc.body)) }))
			defer srv.Close()
			c, _ := NewConsul(srv.URL, "")
			addresses, err := c.Resolve(context.Background(), "catalog")
			if tc.name == "empty" {
				if err != nil || len(addresses) != 0 {
					t.Fatalf("empty discovery = %v, %v", addresses, err)
				}
			} else if err == nil || strings.Contains(err.Error(), tc.body) {
				t.Fatalf("missing or unsafe API error: %v", err)
			}
			if tc.code == 503 {
				cleanup, err := c.Register(context.Background(), ConsulService{ID: "catalog-1", Name: "catalog", Address: "127.0.0.1", Port: 9000, Protocol: "grpc"})
				if err == nil || cleanup != nil {
					t.Fatal("initial registration failure was ignored")
				}
			}
		})
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer srv.Close()
	c, _ := NewConsul(srv.URL, "")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := c.Resolve(ctx, "catalog"); err == nil || time.Since(started) > time.Second {
		t.Fatalf("request cancellation failed: %v", err)
	}
}

func TestConsulDoesNotForwardACLTokensThroughRedirects(t *testing.T) {
	forwarded := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded = true }))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer srv.Close()
	c, _ := NewConsul(srv.URL, "test-acl-token")
	if _, err := c.Resolve(context.Background(), "catalog"); err == nil {
		t.Fatal("redirect should fail")
	}
	if forwarded {
		t.Fatal("ACL token followed a redirect")
	}
}
