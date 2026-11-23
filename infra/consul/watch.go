package consul

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/api/watch"
	"github.com/hashicorp/go-hclog"
)

// Watch delivers the first healthy address snapshot and subsequent changes,
// including an empty snapshot, until ctx is canceled. Calls to observe run
// serially and must return promptly. The Consul SDK retries failed queries.
func (registry *Registry) Watch(ctx context.Context, name string, observe func([]string)) error {
	if !validIdentifier(name) || observe == nil {
		return errors.New("Consul watch requires a service name valid in a URL path segment and an observer")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	plan, err := watch.Parse(map[string]any{"type": "service", "service": name, "tag": grpcTag, "passingonly": true})
	if err != nil {
		return fmt.Errorf("watch Consul service %q: %w", name, err)
	}
	var previous []string
	var delivered bool
	plan.Handler = func(_ uint64, result any) {
		entries, ok := result.([]*api.ServiceEntry)
		if !ok || ctx.Err() != nil {
			return
		}
		current := addresses(entries)
		if !delivered || !slices.Equal(previous, current) {
			previous = slices.Clone(current)
			delivered = true
			observe(current)
		}
	}
	stop := context.AfterFunc(ctx, plan.Stop)
	defer stop()
	if err := plan.RunWithClientAndHclog(registry.client, hclog.NewNullLogger()); err != nil {
		return fmt.Errorf("watch Consul service %q: %w", name, err)
	}
	return ctx.Err()
}
