package httpgateway

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"sync"
	"time"
)

// Trust is attached to the connecting proxy, never to a forwarding header.
// Docker uses the web service's DNS identity rather than trusting its entire
// bridge network (which would also trust direct requests to the API port).
type clientIPResolver struct {
	prefixes  []netip.Prefix
	hosts     []string
	lookup    func(context.Context, string) ([]netip.Addr, error)
	mu        sync.Mutex
	addresses []netip.Addr
	refreshAt time.Time
}

func proxyConfiguration() (*clientIPResolver, error) {
	r := &clientIPResolver{lookup: func(ctx context.Context, host string) ([]netip.Addr, error) {
		return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	}}
	for _, value := range strings.Split(os.Getenv("TRUSTED_PROXY_CIDRS"), ",") {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return nil, fmt.Errorf("invalid TRUSTED_PROXY_CIDRS entry %q", value)
		}
		r.prefixes = append(r.prefixes, prefix.Masked())
	}
	for _, host := range strings.Split(os.Getenv("TRUSTED_PROXY_HOSTS"), ",") {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		if len(host) > 253 || strings.ContainsAny(host, "/:@ \t\r\n") {
			return nil, fmt.Errorf("invalid TRUSTED_PROXY_HOSTS entry %q", host)
		}
		r.hosts = append(r.hosts, host)
	}
	return r, nil
}

func ValidateProxyConfiguration() error { _, err := proxyConfiguration(); return err }

func (p *clientIPResolver) trusted(ctx context.Context, address netip.Addr) bool {
	address = address.Unmap()
	for _, prefix := range p.prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	if len(p.hosts) == 0 {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if time.Now().After(p.refreshAt) {
		p.addresses = nil
		for _, host := range p.hosts {
			lookupCtx, cancel := context.WithTimeout(ctx, time.Second)
			addresses, err := p.lookup(lookupCtx, host)
			cancel()
			if err == nil {
				p.addresses = append(p.addresses, addresses...)
			}
		}
		p.refreshAt = time.Now().Add(5 * time.Second)
	}
	for _, trusted := range p.addresses {
		if trusted.Unmap() == address {
			return true
		}
	}
	return false
}

func (p *clientIPResolver) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	peer = peer.Unmap()
	if !p.trusted(r.Context(), peer) {
		return peer.String()
	}
	// Walk from the verified peer towards the client. The first untrusted hop
	// is authoritative; any client-supplied prefix to its left is ignored.
	if raw := r.Header.Values("X-Forwarded-For"); len(raw) > 0 {
		chain := strings.Split(strings.Join(raw, ","), ",")
		if len(chain) > 32 {
			return peer.String()
		}
		current := peer
		for i := len(chain) - 1; i >= 0; i-- {
			if !p.trusted(r.Context(), current) {
				return current.String()
			}
			next, err := netip.ParseAddr(strings.TrimSpace(chain[i]))
			if err != nil || next.Zone() != "" {
				return peer.String()
			}
			current = next.Unmap()
		}
		return current.String()
	}
	if forwarded, err := netip.ParseAddr(strings.TrimSpace(r.Header.Get("X-Real-IP"))); err == nil &&
		forwarded.Zone() == "" {
		return forwarded.Unmap().String()
	}
	return peer.String()
}
