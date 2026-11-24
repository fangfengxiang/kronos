//go:build etcd_interop && (linux || darwin)

package microv2_test

import (
	"context"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/core/locale"
	"github.com/fangfengxiang/kronos/examples/order"
	"github.com/fangfengxiang/kronos/framework/localization"
)

func TestOrderPropagatesLanguageAcrossBothProtocols(t *testing.T) {
	registry := openTestRegistry(t)
	matcher, err := locale.NewMatcher("en", "en", "fr", "zh-Hans")
	if err != nil {
		t.Fatal(err)
	}
	name := "orders.language." + rand.Text()
	runOrderReplica(t, registry, name, languageOrder{matcher: matcher})
	for _, protocol := range orderProtocols {
		t.Run(string(protocol), func(t *testing.T) {
			waitForInstances(t, registry, name, protocol, 1)
			client := newDiscoveredOrder(t, registry, name, protocol)
			for _, scenario := range []struct {
				name        string
				preferences []string
				want        string
			}{
				{name: "upstream language stays upstream", want: "en"},
				{name: "explicit override and weights", preferences: []string{"fr;q=0.5", "zh-CN;q=0.9"}, want: "zh-Hans"},
				{name: "equal weights preserve field order", preferences: []string{"fr;q=0.8", "zh-CN;q=0.8"}, want: "fr"},
				{name: "invalid and disabled candidates fall back", preferences: []string{"fr;q=invalid", "zh-CN;q=0", "*"}, want: "en"},
			} {
				t.Run(scenario.name, func(t *testing.T) {
					ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
					defer cancel()
					ctx = localization.WithIncomingAcceptLanguage(ctx, "fr")
					if len(scenario.preferences) > 0 {
						ctx = localization.WithAcceptLanguage(ctx, scenario.preferences...)
					}
					reply, err := client.GetOrder(ctx, &order.GetOrderRequest{OrderId: "order-language"})
					if err != nil || reply.GetStatus() != scenario.want {
						t.Fatalf("order language = %q, %v, want %q", reply.GetStatus(), err, scenario.want)
					}
				})
			}
		})
	}
}

type languageOrder struct{ matcher *locale.Matcher }

func (service languageOrder) GetOrder(ctx context.Context, request *order.GetOrderRequest) (*order.GetOrderResponse, error) {
	if len(localization.AcceptLanguage(ctx)) != 0 {
		return nil, errors.New("incoming language appeared in outgoing preferences")
	}
	tag, _ := localization.Negotiate(ctx, service.matcher)
	return &order.GetOrderResponse{OrderId: request.GetOrderId(), Status: tag}, nil
}
