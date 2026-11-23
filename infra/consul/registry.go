// Package consul registers and discovers gRPC instances through a Consul agent.
package consul

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/hashicorp/consul/api"
)

const grpcTag = "grpc"

// Instance identifies one process and its reachable gRPC address.
type Instance struct {
	ID      string
	Name    string
	Address string
}

// Registry owns an HTTP client for one Consul agent.
type Registry struct {
	client     *api.Client
	httpClient *http.Client
}

// New configures an HTTP(S) agent endpoint. Consul's standard environment
// variables supply ACL and TLS settings.
func New(endpoint string) (*Registry, error) {
	address, err := url.Parse(endpoint)
	if err != nil || address.Hostname() == "" || (address.Scheme != "http" && address.Scheme != "https") ||
		address.User != nil || address.RawQuery != "" || address.Fragment != "" || (address.Path != "" && address.Path != "/") {
		return nil, errors.New("Consul endpoint must be an HTTP(S) agent URL")
	}
	config := api.DefaultConfig()
	config.Address = address.Host
	config.Scheme = address.Scheme
	client, err := api.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("configure Consul client: %w", err)
	}
	return &Registry{client: client, httpClient: config.HttpClient}, nil
}

// Close releases idle HTTP connections. Cancel active watches before closing.
func (registry *Registry) Close() {
	registry.httpClient.CloseIdleConnections()
}

// Register publishes an instance with a plaintext gRPC health check. The
// instance enters discovery results after the check passes.
func (registry *Registry) Register(ctx context.Context, instance Instance) error {
	if !validIdentifier(instance.ID) || !validIdentifier(instance.Name) {
		return errors.New("Consul instance requires a service name and an instance ID valid in a URL path segment")
	}
	host, portText, err := net.SplitHostPort(instance.Address)
	port, portError := strconv.Atoi(portText)
	if err != nil || portError != nil || port <= 0 || port > 65535 || !usableHost(host) {
		return errors.New("Consul instance requires a reachable gRPC host and port")
	}
	registration := &api.AgentServiceRegistration{
		ID:      instance.ID,
		Name:    instance.Name,
		Address: host,
		Port:    port,
		Tags:    []string{grpcTag},
		Check: &api.AgentServiceCheck{
			GRPC:                           net.JoinHostPort(host, strconv.Itoa(port)),
			Status:                         api.HealthCritical,
			Interval:                       "2s",
			Timeout:                        "1s",
			DeregisterCriticalServiceAfter: "1m",
		},
	}
	options := api.ServiceRegisterOpts{ReplaceExistingChecks: true}
	if err := registry.client.Agent().ServiceRegisterOpts(registration, options.WithContext(ctx)); err != nil {
		return fmt.Errorf("register Consul instance %q: %w", instance.ID, err)
	}
	return nil
}

// Deregister withdraws an instance. A missing instance is already withdrawn.
func (registry *Registry) Deregister(ctx context.Context, instanceID string) error {
	if !validIdentifier(instanceID) {
		return errors.New("Consul deregistration requires an instance ID valid in a URL path segment")
	}
	err := registry.client.Agent().ServiceDeregisterOpts(instanceID, (&api.QueryOptions{}).WithContext(ctx))
	var status api.StatusError
	if err != nil && (!errors.As(err, &status) || status.Code != http.StatusNotFound) {
		return fmt.Errorf("deregister Consul instance %q: %w", instanceID, err)
	}
	return nil
}

// Lookup returns sorted addresses for the currently healthy gRPC instances.
func (registry *Registry) Lookup(ctx context.Context, name string) ([]string, error) {
	if !validIdentifier(name) {
		return nil, errors.New("Consul lookup requires a service name valid in a URL path segment")
	}
	entries, _, err := registry.client.Health().Service(name, grpcTag, true, (&api.QueryOptions{}).WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("look up Consul service %q: %w", name, err)
	}
	return addresses(entries), nil
}

func validIdentifier(value string) bool {
	return value != "" && value != "." && value != ".." && url.PathEscape(value) == value
}

func usableHost(host string) bool {
	if host == "" || strings.ContainsAny(host, " \t\r\n/?#[]") {
		return false
	}
	if address, err := netip.ParseAddr(host); err == nil {
		return !address.Unmap().IsUnspecified()
	}
	return !strings.Contains(host, ":")
}

func addresses(entries []*api.ServiceEntry) []string {
	endpoints := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry == nil || entry.Service == nil || entry.Service.Port <= 0 || entry.Service.Port > 65535 {
			continue
		}
		host := entry.Service.Address
		if host == "" && entry.Node != nil {
			host = entry.Node.Address
		}
		if usableHost(host) {
			endpoints = append(endpoints, net.JoinHostPort(host, strconv.Itoa(entry.Service.Port)))
		}
	}
	slices.Sort(endpoints)
	return slices.Compact(endpoints)
}
