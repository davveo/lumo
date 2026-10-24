package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/davveo/lumo/internal/constants"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Consul uses the agent HTTP API. Tokens are sent only in request headers.
type Consul struct {
	base             *url.URL
	token            string
	http             *http.Client
	registerInterval time.Duration
	cleanupTimeout   time.Duration
}

func NewConsul(address, token string) (*Consul, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return nil, fmt.Errorf("Consul endpoint is required")
	}
	if !strings.Contains(address, "://") {
		address = "http://" + address
	}
	base, err := url.Parse(address)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Hostname() == "" || base.User != nil ||
		base.RawQuery != "" ||
		base.Fragment != "" {
		return nil, fmt.Errorf("CONSUL_HTTP_ADDR must be an HTTP(S) endpoint without credentials or query parameters")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = 64
	return &Consul{
		base: base, token: token, registerInterval: 10 * time.Second, cleanupTimeout: 5 * time.Second,
		http: &http.Client{
			Timeout:       3 * time.Second,
			Transport:     transport,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

// An unset endpoint preserves standalone development and test runtimes.
func ConsulFromEnv() (*Consul, error) {
	address := strings.TrimSpace(os.Getenv("CONSUL_HTTP_ADDR"))
	if address == "" {
		return nil, nil
	}
	return NewConsul(address, os.Getenv("CONSUL_HTTP_TOKEN"))
}

func (c *Consul) request(ctx context.Context, method, path string, query url.Values, input, output any) (int, error) {
	endpoint := *c.base
	endpoint.RawPath = strings.TrimRight(endpoint.EscapedPath(), "/") + path
	endpoint.Path, _ = url.PathUnescape(endpoint.RawPath)
	endpoint.RawQuery = query.Encode()
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return 0, fmt.Errorf("invalid Consul request")
	}
	if c.token != "" {
		req.Header.Set("X-Consul-Token", c.token)
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("Consul request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return resp.StatusCode, fmt.Errorf("Consul returned HTTP %d", resp.StatusCode)
	}
	if output != nil {
		if err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(output); err != nil {
			return resp.StatusCode, fmt.Errorf("invalid Consul response")
		}
	} else {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	}
	return resp.StatusCode, nil
}

// Resolve returns only passing instances. An empty successful result must not
// retain previously discovered instances in the caller's resolver.
func (c *Consul) Resolve(ctx context.Context, service string) ([]string, error) {
	if !consulName(service) {
		return nil, fmt.Errorf("invalid Consul service name")
	}
	var rows []struct {
		Node    struct{ Address string }
		Service struct {
			ID, Service, Address string
			Port                 int
		}
		Checks []struct{ CheckID, ServiceID, Status string }
	}
	_, err := c.request(
		ctx,
		http.MethodGet,
		"/v1/health/service/"+url.PathEscape(service),
		url.Values{"passing": {"true"}},
		nil,
		&rows,
	)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, row := range rows {
		passing := true
		serviceCheck := false
		for _, check := range row.Checks {
			if check.Status != constants.ConsulHealthStatusPassing {
				passing = false
			}
			if row.Service.ID != "" && check.ServiceID == row.Service.ID && check.CheckID != "" &&
				check.Status == constants.ConsulHealthStatusPassing {
				serviceCheck = true
			}
		}
		if !passing || !serviceCheck || row.Service.Port < 1 || row.Service.Port > 65535 ||
			(row.Service.Service != "" && row.Service.Service != service) {
			continue
		}
		address := row.Service.Address
		if address == "" {
			address = row.Node.Address
		}
		if address == "" || strings.ContainsAny(address, " /\\\t\r\n") {
			continue
		}
		if ip := net.ParseIP(address); ip != nil && ip.IsUnspecified() {
			continue
		}
		seen[net.JoinHostPort(address, strconv.Itoa(row.Service.Port))] = true
	}
	addresses := make([]string, 0, len(seen))
	for address := range seen {
		addresses = append(addresses, address)
	}
	sort.Strings(addresses)
	return addresses, nil
}

type ConsulService struct {
	ID, Name, Address, Protocol string
	Port                        int
}

type consulCheck struct {
	CheckID, Name                  string
	GRPC                           string `json:",omitempty"`
	HTTP                           string `json:",omitempty"`
	Interval                       string
	Timeout                        string
	DeregisterCriticalServiceAfter string
}

type consulRegistration struct {
	ID, Name, Address string
	Port              int
	Meta              map[string]string
	Check             consulCheck
}

type consulAgentService struct {
	ID, Service, Address string
	Port                 int
	Meta                 map[string]string
}

func consulName(name string) bool {
	if name == "" || len(name) > 256 {
		return false
	}
	for _, c := range name {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' && c != '_' {
			return false
		}
	}
	return true
}

func (c *Consul) registrationSpec(ctx context.Context, service ConsulService) (consulRegistration, error) {
	if !consulName(service.Name) || service.ID == "" || strings.ContainsAny(service.ID, "/\\\r\n") ||
		service.Port < 1 ||
		service.Port > 65535 {
		return consulRegistration{}, fmt.Errorf("valid Consul service name, instance ID and port required")
	}
	address := service.Address
	ip := net.ParseIP(address)
	if ip == nil {
		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, address)
		if err != nil {
			return consulRegistration{}, fmt.Errorf("cannot resolve Consul service address")
		}
		for _, candidate := range addresses {
			if !candidate.IP.IsUnspecified() && (ip == nil || candidate.IP.To4() != nil) {
				ip = candidate.IP
			}
		}
	}
	if ip == nil || ip.IsUnspecified() {
		return consulRegistration{}, fmt.Errorf("Consul service address must identify this instance")
	}
	address = ip.String()
	target := net.JoinHostPort(address, strconv.Itoa(service.Port))
	check := consulCheck{
		CheckID:                        "service:" + service.ID,
		Name:                           service.Name + " readiness",
		Interval:                       "10s",
		Timeout:                        "5s",
		DeregisterCriticalServiceAfter: "2m",
	}
	switch service.Protocol {
	case "grpc":
		check.GRPC = target
	case "http":
		check.HTTP = (&url.URL{Scheme: "http", Host: target, Path: "/readyz"}).String()
	default:
		return consulRegistration{}, fmt.Errorf("Consul service protocol must be grpc or http")
	}
	return consulRegistration{ID: service.ID, Name: service.Name, Address: address, Port: service.Port,
		Meta: map[string]string{
			"logical_service": service.Name,
			"protocol":        service.Protocol,
			"registration_id": ID("consul"),
			"health_target":   check.GRPC + check.HTTP,
		}, Check: check}, nil
}

func (c *Consul) ensureRegistration(ctx context.Context, spec consulRegistration, owned bool) error {
	var existing consulAgentService
	code, err := c.request(ctx, http.MethodGet, "/v1/agent/service/"+url.PathEscape(spec.ID), nil, nil, &existing)
	if err == nil {
		if existing.Service != spec.Name {
			return fmt.Errorf("Consul instance ID belongs to another service")
		}
		if owned && existing.Meta["registration_id"] != spec.Meta["registration_id"] {
			return fmt.Errorf("Consul instance ID was replaced by another process")
		}
		if existing.Address == spec.Address && existing.Port == spec.Port &&
			existing.Meta["registration_id"] == spec.Meta["registration_id"] &&
			existing.Meta["health_target"] == spec.Meta["health_target"] {
			return nil
		}
	} else if code != http.StatusNotFound {
		return err
	}
	_, err = c.request(
		ctx,
		http.MethodPut,
		"/v1/agent/service/register",
		url.Values{"replace-existing-checks": {"true"}},
		spec,
		nil,
	)
	return err
}

// Register installs an active health check and repairs a missing registration.
// The returned cleanup waits for repair to stop before deregistering. Each
// process has an incarnation marker so an old cleanup cannot remove its successor.
func (c *Consul) Register(ctx context.Context, service ConsulService) (func(), error) {
	spec, err := c.registrationSpec(ctx, service)
	if err != nil {
		return nil, err
	}
	workerCtx, cancel := context.WithCancel(ctx)
	if err = c.ensureRegistration(workerCtx, spec, false); err != nil {
		cancel()
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(c.registerInterval)
		defer ticker.Stop()
		for {
			select {
			case <-workerCtx.Done():
				return
			case <-ticker.C:
				if err := c.ensureRegistration(workerCtx, spec, true); err != nil && workerCtx.Err() == nil {
					slog.Warn("Consul registration refresh failed", "service", service.Name)
				}
			}
		}
	}()
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			cancel()
			<-done
			cleanupCtx, finish := context.WithTimeout(context.Background(), c.cleanupTimeout)
			defer finish()
			var existing consulAgentService
			code, err := c.request(
				cleanupCtx,
				http.MethodGet,
				"/v1/agent/service/"+url.PathEscape(spec.ID),
				nil,
				nil,
				&existing,
			)
			if code == http.StatusNotFound {
				return
			}
			if err == nil && existing.Service == spec.Name &&
				existing.Meta["registration_id"] == spec.Meta["registration_id"] {
				_, err = c.request(
					cleanupCtx,
					http.MethodPut,
					"/v1/agent/service/deregister/"+url.PathEscape(spec.ID),
					nil,
					nil,
					nil,
				)
			}
			if err != nil {
				slog.Warn(
					"Consul deregistration failed; critical health check will expire the instance",
					"service",
					service.Name,
				)
			}
		})
	}
	return cleanup, nil
}
