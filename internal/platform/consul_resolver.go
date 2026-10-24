package platform

import (
	"context"
	"fmt"
	"strings"
	"time"

	_ "google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/resolver"
)

const consulResolverScheme = "consul"

type consulResolverBuilder struct {
	client       *Consul
	pollInterval time.Duration
}

func (*consulResolverBuilder) Scheme() string { return consulResolverScheme }

func (b *consulResolverBuilder) Build(target resolver.Target, cc resolver.ClientConn, _ resolver.BuildOptions) (resolver.Resolver, error) {
	name := target.Endpoint()
	if name == "" || target.URL.Host != "" || target.URL.RawQuery != "" || target.URL.Fragment != "" || strings.ContainsAny(name, "/?#") {
		return nil, fmt.Errorf("invalid Consul service target")
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &consulResolver{client: b.client, service: name, cc: cc, ctx: ctx, cancel: cancel,
		pollInterval: b.pollInterval, refresh: make(chan struct{}, 1), done: make(chan struct{})}
	go r.run()
	return r, nil
}

type consulResolver struct {
	client       *Consul
	service      string
	cc           resolver.ClientConn
	ctx          context.Context
	cancel       context.CancelFunc
	pollInterval time.Duration
	refresh      chan struct{}
	done         chan struct{}
}

func (r *consulResolver) ResolveNow(resolver.ResolveNowOptions) {
	select {
	case r.refresh <- struct{}{}:
	default:
	}
}

func (r *consulResolver) Close() {
	r.cancel()
	<-r.done
}

func (r *consulResolver) run() {
	defer close(r.done)
	interval := r.pollInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	var published string
	initialized := false
	for {
		ctx, cancel := context.WithTimeout(r.ctx, 3*time.Second)
		addresses, err := r.client.Resolve(ctx, r.service)
		cancel()
		if r.ctx.Err() != nil {
			return
		}
		if err != nil {
			// An unavailable registry must not leave an unbounded stale set
			// usable, or silently fall back to the deployment's DNS addresses.
			addresses = nil
		}
		key := strings.Join(addresses, "\n")
		if !initialized || key != published {
			state := resolver.State{Addresses: make([]resolver.Address, 0, len(addresses)), Endpoints: make([]resolver.Endpoint, 0, len(addresses))}
			for _, address := range addresses {
				entry := resolver.Address{Addr: address}
				state.Addresses = append(state.Addresses, entry)
				state.Endpoints = append(state.Endpoints, resolver.Endpoint{Addresses: []resolver.Address{entry}})
			}
			// round_robin removes the previous SubConns even when the new
			// endpoint set is empty and UpdateState returns ErrBadResolverState.
			_ = r.cc.UpdateState(state)
			published, initialized = key, true
		}
		if err != nil {
			r.cc.ReportError(err)
		} else if len(addresses) == 0 {
			r.cc.ReportError(fmt.Errorf("Consul has no passing instances for %s", r.service))
		}
		timer := time.NewTimer(interval)
		select {
		case <-r.ctx.Done():
			timer.Stop()
			return
		case <-r.refresh:
			timer.Stop()
		case <-timer.C:
		}
	}
}
