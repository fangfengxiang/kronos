package kronos

import (
	"context"
	"slices"
	"time"

	"github.com/fangfengxiang/kronos/framework/rpc"
	"github.com/fangfengxiang/kronos/platform/kronos/internal/transport"
)

const (
	discoveryBudget = 2 * time.Second
	discoveryPeriod = 500 * time.Millisecond
	unavailableFor  = time.Second
)

type serviceView struct {
	addresses    []string
	connections  map[string]*peer
	next         int
	refreshAfter time.Time
	lookupFailed bool
	lookupDone   chan struct{}
}

type peer struct {
	address      string
	connection   transport.Client
	uses         int
	retired      bool
	blockedUntil time.Time
}

func (client *Client) resolve(ctx context.Context, serviceName string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	client.mu.Lock()
	if client.ctx.Err() != nil {
		client.mu.Unlock()
		return ErrClientClosed
	}
	view := client.views[serviceName]
	if view == nil {
		view = &serviceView{connections: make(map[string]*peer)}
		client.views[serviceName] = view
	}
	if time.Now().Before(view.refreshAfter) {
		client.mu.Unlock()
		return ctx.Err()
	}
	if view.lookupDone == nil {
		view.lookupDone = make(chan struct{})
		go client.refreshService(serviceName, view)
	}
	lookupDone := view.lookupDone
	client.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-client.ctx.Done():
		return ErrClientClosed
	case <-lookupDone:
		return ctx.Err()
	}
}

func (client *Client) refreshService(serviceName string, view *serviceView) {
	ctx, cancel := context.WithTimeout(client.ctx, discoveryBudget)
	defer cancel()
	started := time.Now()
	addresses, err := client.config.registry.Lookup(ctx, serviceName, string(client.config.protocol))
	if err != nil && client.ctx.Err() == nil {
		rpcLogger(client.config.logger).WarnContext(ctx, "RPC discovery failed", "event", "rpc_discovery_failed", "service", serviceName,
			"protocol", client.config.protocol, "duration_us", time.Since(started).Microseconds(), "error", err)
	}
	addresses = slices.Clone(addresses)
	slices.Sort(addresses)
	var staleConnections []transport.Client
	var instanceCount, pooledTransportCount int
	snapshotApplied := false
	client.mu.Lock()
	if client.ctx.Err() == nil {
		view.refreshAfter = time.Now().Add(discoveryPeriod)
		view.lookupFailed = err != nil
		if err == nil {
			view.addresses = slices.Compact(addresses)
			for address, peer := range view.connections {
				if _, found := slices.BinarySearch(view.addresses, address); found {
					continue
				}
				delete(view.connections, address)
				peer.retired = true
				if peer.uses == 0 {
					delete(client.peers, peer)
					staleConnections = append(staleConnections, peer.connection)
				}
			}
			snapshotApplied = true
			instanceCount, pooledTransportCount = len(view.addresses), len(view.connections)
		}
	}
	close(view.lookupDone)
	view.lookupDone = nil
	client.mu.Unlock()
	for _, connection := range staleConnections {
		client.closeIdle(connection)
	}
	if snapshotApplied {
		rpcLogger(client.config.logger).DebugContext(ctx, "RPC discovery snapshot applied", "event", "rpc_discovery_snapshot", "service", serviceName,
			"protocol", client.config.protocol, "discovered_instances", instanceCount, "pooled_transports", pooledTransportCount)
	}
}

func (client *Client) acquire(ctx context.Context, serviceName, previousAddress string) (*peer, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.ctx.Err() != nil {
		return nil, ErrClientClosed
	}
	view := client.views[serviceName]
	if view == nil || view.lookupFailed {
		return nil, &rpc.Error{Code: rpc.Unavailable, Reason: "DISCOVERY_FAILED", Message: "service discovery failed"}
	}
	availableAt := time.Now()
	count := len(view.addresses)
	for offset := range count {
		index := (view.next + offset) % count
		address := view.addresses[index]
		if address == previousAddress {
			continue
		}
		selected := view.connections[address]
		if selected == nil {
			connection, err := transport.Dial(ctx, client.config.protocol, address)
			if err != nil {
				return nil, &rpc.Error{Code: rpc.Unavailable, Reason: rpc.ServiceUnavailableReason, Message: "unable to connect to rpc instance"}
			}
			selected = &peer{address: address, connection: connection}
			view.connections[address] = selected
			client.peers[selected] = struct{}{}
		}
		if availableAt.Before(selected.blockedUntil) {
			continue
		}
		view.next = (index + 1) % count
		selected.uses++
		return selected, nil
	}
	return nil, &rpc.Error{Code: rpc.Unavailable, Reason: "NO_INSTANCES", Message: "no available rpc instances"}
}

func (client *Client) release(peer *peer, unavailable bool) {
	client.mu.Lock()
	peer.uses--
	if unavailable {
		peer.blockedUntil = time.Now().Add(unavailableFor)
	}
	_, owned := client.peers[peer]
	shouldClose := owned && peer.retired && peer.uses == 0
	if shouldClose {
		delete(client.peers, peer)
	}
	client.mu.Unlock()
	if shouldClose {
		client.closeIdle(peer.connection)
	}
}

func (client *Client) closeIdle(connection transport.Client) {
	if err := connection.Close(); err != nil {
		rpcLogger(client.config.logger).Warn("close rpc instance connection", "error", err)
	}
}
