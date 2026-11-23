// Package etcd publishes and resolves Kronos service addresses in etcd v3.
package etcd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

var (
	// ErrInvalidConfig indicates an invalid endpoint, service name or protocol.
	ErrInvalidConfig = errors.New("invalid etcd registry configuration")
	// ErrUnavailable indicates that an etcd operation failed.
	ErrUnavailable = errors.New("etcd registry unavailable")
)

const (
	grpcPrefix  = "/kronos/registry/grpc/"
	microPrefix = "/micro/registry/"
)

var serviceNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_.-]*$`)

// Registry owns an independent client. Withdraw its publications before Close.
type Registry struct {
	client          *clientv3.Client
	closeClient     func() error
	requestTimeout  time.Duration
	leaseTTLSeconds int64
	renewInterval   time.Duration
}

// New constructs a registry for one or more etcd TCP endpoints.
// Endpoints can be host:port, HTTP origins or HTTPS origins.
func New(endpoints ...string) (*Registry, error) {
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("%w: provide at least one etcd endpoint", ErrInvalidConfig)
	}
	for index, endpoint := range endpoints {
		if !validEndpoint(endpoint) {
			return nil, fmt.Errorf("%w: endpoint %d requires a TCP address or HTTP(S) origin", ErrInvalidConfig, index+1)
		}
	}
	const requestTimeout = 2 * time.Second
	client, err := clientv3.New(clientv3.Config{
		Endpoints: slices.Clone(endpoints), DialTimeout: requestTimeout,
	})
	if err != nil {
		return nil, operationError(context.Background(), "open etcd client", err)
	}
	return &Registry{
		client: client, closeClient: sync.OnceValue(client.Close),
		requestTimeout: requestTimeout, leaseTTLSeconds: 15, renewInterval: 5 * time.Second,
	}, nil
}

// Close releases the client and stops its lease renewal tasks.
func (registry *Registry) Close() error {
	return registry.closeClient()
}

// Lookup returns the registered addresses for grpc or micro-v2. A service
// without instances has an empty result.
func (registry *Registry) Lookup(ctx context.Context, serviceName, protocol string) ([]string, error) {
	if !serviceNamePattern.MatchString(serviceName) {
		return nil, fmt.Errorf("%w: invalid service name", ErrInvalidConfig)
	}
	var prefix string
	switch protocol {
	case "grpc":
		prefix = grpcPrefix
	case "micro-v2":
		prefix = microPrefix
	default:
		return nil, fmt.Errorf("%w: unsupported discovery protocol %q", ErrInvalidConfig, protocol)
	}
	ctx, cancel := context.WithTimeout(ctx, registry.requestTimeout)
	defer cancel()
	recordPrefix := prefix + serviceName + "/"
	response, err := registry.client.Get(ctx, recordPrefix, clientv3.WithPrefix())
	if err != nil {
		return nil, operationError(ctx, "lookup service in etcd", err)
	}
	addresses := make([]string, 0, len(response.Kvs))
	for _, entry := range response.Kvs {
		if prefix == grpcPrefix {
			if address := string(entry.Value); validAddress(address) {
				addresses = append(addresses, address)
			}
			continue
		}
		var service microService
		if json.Unmarshal(entry.Value, &service) != nil || service.Name != serviceName {
			continue
		}
		for _, node := range service.Nodes {
			if validAddress(node.Address) {
				addresses = append(addresses, node.Address)
			}
		}
	}
	slices.Sort(addresses)
	return slices.Compact(addresses), nil
}

type microService struct {
	Name    string      `json:"name"`
	Version string      `json:"version"`
	Nodes   []microNode `json:"nodes"`
}

type microNode struct {
	ID      string `json:"id"`
	Address string `json:"address"`
}

func validEndpoint(endpoint string) bool {
	if strings.Contains(endpoint, "://") {
		origin, err := url.Parse(endpoint)
		if err != nil || (origin.Scheme != "http" && origin.Scheme != "https") || origin.User != nil ||
			(origin.Path != "" && origin.Path != "/") || origin.ForceQuery || origin.RawQuery != "" || origin.Fragment != "" {
			return false
		}
		endpoint = origin.Host
	}
	return validAddress(endpoint)
}

func validAddress(address string) bool {
	host, portText, err := net.SplitHostPort(address)
	port, portError := strconv.ParseUint(portText, 10, 16)
	if err != nil || portError != nil || port == 0 || host == "" || strings.ContainsAny(host, " \t\r\n/\\@?#*") {
		return false
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return !ip.Unmap().IsUnspecified()
	}
	return true
}

func operationError(ctx context.Context, operation string, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return fmt.Errorf("%s: %w: %v", operation, ErrUnavailable, err)
}
