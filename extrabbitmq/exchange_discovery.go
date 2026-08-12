// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2026 Steadybit GmbH

package extrabbitmq

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/steadybit/discovery-kit/go/discovery_kit_api"
	"github.com/steadybit/discovery-kit/go/discovery_kit_commons"
	"github.com/steadybit/discovery-kit/go/discovery_kit_sdk"
	"github.com/steadybit/extension-kit/extbuild"
	"github.com/steadybit/extension-kit/extutil"

	rabbithole "github.com/michaelklishin/rabbit-hole/v3"
	"github.com/steadybit/extension-rabbitmq/config"
)

const (
	exchangeTargetId = "com.steadybit.extension_rabbitmq.exchange"
)

type rabbitExchangeDiscovery struct{}

var _ discovery_kit_sdk.TargetDescriber = (*rabbitExchangeDiscovery)(nil)
var _ discovery_kit_sdk.AttributeDescriber = (*rabbitExchangeDiscovery)(nil)

func NewRabbitExchangeDiscovery(ctx context.Context) discovery_kit_sdk.TargetDiscovery {
	d := &rabbitExchangeDiscovery{}
	return discovery_kit_sdk.NewCachedTargetDiscovery(
		d,
		discovery_kit_sdk.WithRefreshTargetsNow(),
		discovery_kit_sdk.WithRefreshTargetsInterval(ctx, time.Duration(config.Config.DiscoveryIntervalExchangeSeconds)*time.Second),
	)
}

func (r *rabbitExchangeDiscovery) Describe() discovery_kit_api.DiscoveryDescription {
	return discovery_kit_api.DiscoveryDescription{
		Id: exchangeTargetId,
		Discover: discovery_kit_api.DescribingEndpointReferenceWithCallInterval{
			CallInterval: extutil.Ptr(fmt.Sprintf("%ds", config.Config.DiscoveryIntervalExchangeSeconds)),
		},
	}
}

func (r *rabbitExchangeDiscovery) DescribeTarget() discovery_kit_api.TargetDescription {
	return discovery_kit_api.TargetDescription{
		Id:       exchangeTargetId,
		Label:    discovery_kit_api.PluralLabel{One: "RabbitMQ Exchange", Other: "RabbitMQ Exchanges"},
		Category: extutil.Ptr("rabbitmq"),
		Version:  extbuild.GetSemverVersionStringOrUnknown(),
		Icon:     extutil.Ptr(rabbitMQIcon),
		Table: discovery_kit_api.Table{
			Columns: []discovery_kit_api.Column{
				{Attribute: "steadybit.label"},
				{Attribute: "rabbitmq.cluster.name"},
				{Attribute: "rabbitmq.exchange.vhost"},
				{Attribute: "rabbitmq.exchange.name"},
				{Attribute: "rabbitmq.exchange.type"},
				{Attribute: "rabbitmq.exchange.durable"},
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
	}
}

func (r *rabbitExchangeDiscovery) DiscoverTargets(ctx context.Context) ([]discovery_kit_api.Target, error) {
	return getAllExchanges(ctx)
}

func getAllExchanges(_ context.Context) ([]discovery_kit_api.Target, error) {
	handler := func(client *rabbithole.Client, targetType string) ([]discovery_kit_api.Target, error) {
		amqpURL := resolveAMQPURLForClient(client.Endpoint)
		clusterName := ""
		if cn, _ := client.GetClusterName(); cn != nil {
			clusterName = cn.Name
		}

		exchanges, err := client.ListExchanges()
		if err != nil {
			return nil, err
		}

		out := make([]discovery_kit_api.Target, 0, len(exchanges))
		for _, ex := range exchanges {
			// The unnamed default exchange, the amq.* built-ins and internal exchanges are
			// not meaningful publish targets, so they are not reported.
			if ex.Name == "" || strings.HasPrefix(ex.Name, "amq.") || ex.Internal {
				continue
			}
			out = append(out, toExchangeTarget(client.Endpoint, amqpURL, ex, clusterName))
		}
		return out, nil
	}

	targets, err := FetchTargetPerClient(handler, exchangeTargetId)
	if err != nil {
		return nil, err
	}
	return discovery_kit_commons.ApplyAttributeExcludes(targets, config.Config.DiscoveryAttributesExcludesExchanges), nil
}

func toExchangeTarget(mgmtURL, amqpURL string, ex rabbithole.ExchangeInfo, cluster string) discovery_kit_api.Target {
	label := ex.Vhost + "/" + ex.Name
	attrs := map[string][]string{
		"rabbitmq.exchange.vhost":       {ex.Vhost},
		"rabbitmq.exchange.name":        {ex.Name},
		"rabbitmq.exchange.type":        {ex.Type},
		"rabbitmq.exchange.durable":     {fmt.Sprintf("%t", ex.Durable)},
		"rabbitmq.exchange.auto_delete": {fmt.Sprintf("%t", ex.AutoDelete)},
		"rabbitmq.cluster.name":         {cluster},
		"rabbitmq.amqp.url":             {amqpURL},
		"rabbitmq.mgmt.url":             {mgmtURL},
	}

	return discovery_kit_api.Target{
		Id:         mgmtURL + "::exchange::" + label,
		Label:      label,
		TargetType: exchangeTargetId,
		Attributes: attrs,
	}
}
