package httpgateway

import (
	"context"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

func TestClientIPTrustsOnlyVerifiedProxyHops(t *testing.T) {
	t.Setenv("TRUSTED_PROXY_CIDRS", "192.0.2.0/24,2001:db8::/64")
	t.Setenv("TRUSTED_PROXY_HOSTS", "")
	p, err := proxyConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, peer, xff, real, want string }{
		{"direct forged headers", "198.51.100.9:123", "203.0.113.1", "203.0.113.2", "198.51.100.9"},
		{"proxy", "192.0.2.1:123", "203.0.113.1", "", "203.0.113.1"},
		{"forged prefix", "192.0.2.1:123", "1.2.3.4, 203.0.113.1", "", "203.0.113.1"},
		{"proxy chain", "192.0.2.1:123", "203.0.113.1, 192.0.2.2", "", "203.0.113.1"},
		{"invalid chain", "192.0.2.1:123", "invalid", "203.0.113.1", "192.0.2.1"},
		{"legacy real ip", "192.0.2.1:123", "", "203.0.113.1", "203.0.113.1"},
		{"IPv6", "[2001:db8::1]:123", "2001:db8:1::2", "", "2001:db8:1::2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/", nil)
			r.RemoteAddr = tc.peer
			if tc.xff != "" {
				r.Header.Set("X-Forwarded-For", tc.xff)
			}
			if tc.real != "" {
				r.Header.Set("X-Real-IP", tc.real)
			}
			if got := p.clientIP(r); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestProxyDNSRefreshDoesNotTrustOtherBridgeClients(t *testing.T) {
	t.Setenv("TRUSTED_PROXY_CIDRS", "")
	t.Setenv("TRUSTED_PROXY_HOSTS", "web")
	p, err := proxyConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	address := netip.MustParseAddr("172.20.0.8")
	p.lookup = func(context.Context, string) ([]netip.Addr, error) { return []netip.Addr{address}, nil }
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "172.20.0.8:123"
	r.Header.Set("X-Forwarded-For", "203.0.113.1")
	if p.clientIP(r) != "203.0.113.1" {
		t.Fatal("proxy identity not resolved")
	}
	r.RemoteAddr = "172.20.0.1:123"
	if p.clientIP(r) != "172.20.0.1" {
		t.Fatal("direct API caller trusted")
	}
	address = netip.MustParseAddr("172.20.0.9")
	p.refreshAt = time.Time{}
	r.RemoteAddr = "172.20.0.8:123"
	if p.clientIP(r) != "172.20.0.8" {
		t.Fatal("recreated proxy's old address still trusted")
	}
}

func TestInvalidProxyConfigurationFailsClosed(t *testing.T) {
	t.Setenv("TRUSTED_PROXY_CIDRS", "broken")
	t.Setenv("TRUSTED_PROXY_HOSTS", "")
	if ValidateProxyConfiguration() == nil {
		t.Fatal("invalid configuration accepted")
	}
	w := httptest.NewRecorder()
	NewHandler(nil, false, "").ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
}
