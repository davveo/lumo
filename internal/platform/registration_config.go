package platform

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

// RegisterConsulService is optional for standalone tests and local devstack.
// The advertised address must be reachable from the Consul agent, rather than
// a wildcard listener or the host-side published port.
func RegisterConsulService(ctx context.Context, consul *Consul, name, listenAddress, protocol string) (func(), error) {
	if consul == nil {
		return func() {}, nil
	}
	spec, err := consulServiceFromEnv(name, listenAddress, protocol)
	if err != nil {
		return nil, err
	}
	return consul.Register(ctx, spec)
}

func consulServiceFromEnv(name, listenAddress, protocol string) (ConsulService, error) {
	host, rawPort, err := net.SplitHostPort(listenAddress)
	if err != nil {
		return ConsulService{}, fmt.Errorf("invalid service listener: %w", err)
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil || port < 1 || port > 65535 {
		return ConsulService{}, fmt.Errorf("service listener requires a port between 1 and 65535")
	}
	hostname, err := os.Hostname()
	if err != nil {
		return ConsulService{}, fmt.Errorf("service hostname: %w", err)
	}
	if address := strings.TrimSpace(os.Getenv("CONSUL_SERVICE_ADDRESS")); address != "" {
		host = address
	} else if host == "" || host == "0.0.0.0" || host == "::" {
		host = hostname
	}
	id := consulInstanceID(name, hostname, strconv.Itoa(port))
	return ConsulService{ID: id, Name: name, Address: host, Port: port, Protocol: protocol}, nil
}

func consulInstanceID(name, hostname, port string) string {
	if id := strings.TrimSpace(os.Getenv("CONSUL_SERVICE_ID")); id != "" {
		return id
	}
	// The hostname distinguishes containers/pods; the listener port also
	// distinguishes independently launched instances sharing one host.
	return name + "-" + hostname + "-" + port
}
