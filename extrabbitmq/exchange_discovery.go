// SPDX-License-Identifier: MIT
// Discovery for RabbitMQ exchanges using rabbit-hole and FetchTargetPerClient.

package extrabbitmq

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/steadybit/discovery-kit/go/discovery_kit_api"
	"github.com/steadybit/discovery-kit/go/discovery_kit_commons"
	"github.com/steadybit/discovery-kit/go/discovery_kit_sdk"
	"github.com/steadybit/extension-kit/extbuild"
	"github.com/steadybit/extension-kit/extutil"

	rabbithole "github.com/michaelklishin/rabbit-hole/v3"
)

const (
	exchangeTargetId   = "com.steadybit.extension_rabbitmq.exchange"
	exchangeRefreshSec = 60
)

type rabbitExchangeDiscovery struct{}

var _ discovery_kit_sdk.TargetDescriber = (*rabbitExchangeDiscovery)(nil)
var _ discovery_kit_sdk.AttributeDescriber = (*rabbitExchangeDiscovery)(nil)

func NewRabbitExchangeDiscovery(ctx context.Context) discovery_kit_sdk.TargetDiscovery {
	d := &rabbitExchangeDiscovery{}
	return discovery_kit_sdk.NewCachedTargetDiscovery(
		d,
		discovery_kit_sdk.WithRefreshTargetsNow(),
		discovery_kit_sdk.WithRefreshTargetsInterval(ctx, exchangeRefreshSec*time.Second),
	)
}

func (r *rabbitExchangeDiscovery) Describe() discovery_kit_api.DiscoveryDescription {
	return discovery_kit_api.DiscoveryDescription{
		Id: exchangeTargetId,
		Discover: discovery_kit_api.DescribingEndpointReferenceWithCallInterval{
			CallInterval: extutil.Ptr(fmt.Sprintf("%ds", exchangeRefreshSec)),
		},
	}
}

func (r *rabbitExchangeDiscovery) DescribeTarget() discovery_kit_api.TargetDescription {
	return discovery_kit_api.TargetDescription{
		Id:       exchangeTargetId,
		Label:    discovery_kit_api.PluralLabel{One: "RabbitMQ exchange", Other: "RabbitMQ exchanges"},
		Category: extutil.Ptr("rabbitmq"),
		Version:  extbuild.GetSemverVersionStringOrUnknown(),
		Icon:     extutil.Ptr(rabbitMQIcon),
		Table: discovery_kit_api.Table{
			Columns: []discovery_kit_api.Column{
				{Attribute: "steadybit.label"},
				{Attribute: "rabbitmq.exchange.vhost"},
				{Attribute: "rabbitmq.exchange.name"},
				{Attribute: "rabbitmq.exchange.type"},
				{Attribute: "rabbitmq.exchange.durable"},
				{Attribute: "rabbitmq.exchange.auto_delete"},
				{Attribute: "rabbitmq.exchange.internal"},
			},
			OrderBy: []discovery_kit_api.OrderBy{{Attribute: "steadybit.label", Direction: "ASC"}},
		},
	}
}

func (r *rabbitExchangeDiscovery) DescribeAttributes() []discovery_kit_api.AttributeDescription {
	return []discovery_kit_api.AttributeDescription{
		{Attribute: "rabbitmq.exchange.vhost", Label: discovery_kit_api.PluralLabel{One: "Vhost", Other: "Vhosts"}},
		{Attribute: "rabbitmq.exchange.name", Label: discovery_kit_api.PluralLabel{One: "Exchange name", Other: "Exchange names"}},
		{Attribute: "rabbitmq.exchange.type", Label: discovery_kit_api.PluralLabel{One: "Type", Other: "Types"}},
		{Attribute: "rabbitmq.exchange.durable", Label: discovery_kit_api.PluralLabel{One: "Durable", Other: "Durable"}},
		{Attribute: "rabbitmq.exchange.auto_delete", Label: discovery_kit_api.PluralLabel{One: "Auto-delete", Other: "Auto-delete"}},
		{Attribute: "rabbitmq.exchange.internal", Label: discovery_kit_api.PluralLabel{One: "Internal", Other: "Internal"}},
	}
}

func (r *rabbitExchangeDiscovery) DiscoverTargets(ctx context.Context) ([]discovery_kit_api.Target, error) {
	return getAllExchanges(ctx)
}

// --- core listing ---

func getAllExchanges(ctx context.Context) ([]discovery_kit_api.Target, error) {
	handler := func(client *rabbithole.Client) ([]discovery_kit_api.Target, error) {
		out := make([]discovery_kit_api.Target, 0, 32)

		exs, err := client.ListExchanges()
		if err != nil {
			return nil, err
		}
		for _, ex := range exs {
			out = append(out, toExchangeTarget(client.Endpoint, ex))
		}
		return out, nil
	}

	targets, err := FetchTargetPerClient(handler)
	if err != nil {
		log.Warn().Err(err).Msg("exchange discovery encountered errors")
	}
	// No exchange-specific exclude list configured -> pass nil.
	return discovery_kit_commons.ApplyAttributeExcludes(targets, nil), nil
}

func toExchangeTarget(mgmtURL string, ex rabbithole.ExchangeInfo) discovery_kit_api.Target {
	label := ex.Vhost + "/" + ex.Name
	attrs := map[string][]string{
		"rabbitmq.exchange.vhost":       {ex.Vhost},
		"rabbitmq.exchange.name":        {ex.Name},
		"rabbitmq.exchange.type":        {ex.Type},
		"rabbitmq.exchange.durable":     {fmt.Sprintf("%t", ex.Durable)},
		"rabbitmq.exchange.auto_delete": {fmt.Sprintf("%t", ex.AutoDelete)},
		"rabbitmq.exchange.internal":    {fmt.Sprintf("%t", ex.Internal)},
	}

	return discovery_kit_api.Target{
		Id:         mgmtURL + "::" + label,
		Label:      label,
		TargetType: exchangeTargetId,
		Attributes: attrs,
	}
}
