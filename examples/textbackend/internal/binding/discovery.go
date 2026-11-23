package binding

import (
	"context"
	"errors"
	"time"

	wire "github.com/fangfengxiang/kronos/examples/textbackend/internal/wire/kronos/textbackend/v1"
	"github.com/fangfengxiang/kronos/framework/rpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/resolver"
)

const discoveryScheme = "kronos-discovery"

type watchEndpoints func(context.Context, string, func([]string)) error

func NewDiscoveredClient(ctx context.Context, serviceName string, defaultTimeout time.Duration, watch watchEndpoints) (*rpc.UnaryClient[wire.GenerateRequest, wire.GenerateResponse], error) {
	return newClient(ctx, discoveryScheme+":///"+serviceName, defaultTimeout, `{"loadBalancingConfig":[{"round_robin":{}}]}`,
		grpc.WithResolvers(resolverBuilder{watch: watch}), grpc.WithNoProxy())
}

type resolverBuilder struct {
	watch watchEndpoints
}

func (resolverBuilder) Scheme() string { return discoveryScheme }

func (builder resolverBuilder) Build(target resolver.Target, connection resolver.ClientConn, _ resolver.BuildOptions) (resolver.Resolver, error) {
	ctx, cancel := context.WithCancel(context.Background())
	watcher := &discoveryResolver{cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(watcher.done)
		err := builder.watch(ctx, target.Endpoint(), func(addresses []string) {
			endpoints := make([]resolver.Endpoint, 0, len(addresses))
			for _, address := range addresses {
				endpoints = append(endpoints, resolver.Endpoint{Addresses: []resolver.Address{{Addr: address}}})
			}
			// An empty update removes old endpoints even though gRPC returns ErrBadResolverState.
			if err := connection.UpdateState(resolver.State{Endpoints: endpoints}); err != nil && len(endpoints) > 0 {
				connection.ReportError(err)
			}
		})
		if ctx.Err() == nil {
			_ = connection.UpdateState(resolver.State{})
			if err == nil {
				err = errors.New("text backend discovery ended")
			}
			connection.ReportError(err)
		}
	}()
	return watcher, nil
}

type discoveryResolver struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func (*discoveryResolver) ResolveNow(resolver.ResolveNowOptions) {}

func (watcher *discoveryResolver) Close() {
	watcher.cancel()
	<-watcher.done
}
