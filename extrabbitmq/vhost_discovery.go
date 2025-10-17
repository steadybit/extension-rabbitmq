// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2024 Steadybit GmbH

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
	"github.com/steadybit/extension-rabbitmq/config"
)

// Target type and icon
const (
	vhostTargetId = "com.steadybit.extension_rabbitmq.vhost"
)

// Discovery implementation
const defaultVhostDiscoveryIntervalSeconds = 60

type rabbitVhostDiscovery struct{}

var _ discovery_kit_sdk.TargetDescriber = (*rabbitVhostDiscovery)(nil)
var _ discovery_kit_sdk.AttributeDescriber = (*rabbitVhostDiscovery)(nil)

func NewRabbitVhostDiscovery(ctx context.Context) discovery_kit_sdk.TargetDiscovery {
	d := &rabbitVhostDiscovery{}
	return discovery_kit_sdk.NewCachedTargetDiscovery(
		d,
		discovery_kit_sdk.WithRefreshTargetsNow(),
		discovery_kit_sdk.WithRefreshTargetsInterval(ctx, defaultVhostDiscoveryIntervalSeconds*time.Second),
	)
}

func (r *rabbitVhostDiscovery) Describe() discovery_kit_api.DiscoveryDescription {
	return discovery_kit_api.DiscoveryDescription{
		Id: vhostTargetId,
		Discover: discovery_kit_api.DescribingEndpointReferenceWithCallInterval{
			CallInterval: extutil.Ptr(fmt.Sprintf("%ds", defaultVhostDiscoveryIntervalSeconds)),
		},
	}
}

func (r *rabbitVhostDiscovery) DescribeTarget() discovery_kit_api.TargetDescription {
	return discovery_kit_api.TargetDescription{
		Id:       vhostTargetId,
		Label:    discovery_kit_api.PluralLabel{One: "RabbitMQ vhost", Other: "RabbitMQ vhosts"},
		Category: extutil.Ptr("rabbitmq"),
		Version:  extbuild.GetSemverVersionStringOrUnknown(),
		Icon:     extutil.Ptr(rabbitMQIcon),
		Table: discovery_kit_api.Table{
			Columns: []discovery_kit_api.Column{
				{Attribute: "steadybit.label"},
				{Attribute: "rabbitmq.vhost.name"},
				{Attribute: "rabbitmq.vhost.tracing"},
			},
			OrderBy: []discovery_kit_api.OrderBy{{Attribute: "steadybit.label", Direction: "ASC"}},
		},
	}
}

func (r *rabbitVhostDiscovery) DescribeAttributes() []discovery_kit_api.AttributeDescription {
	return []discovery_kit_api.AttributeDescription{
		{Attribute: "rabbitmq.vhost.name", Label: discovery_kit_api.PluralLabel{One: "Vhost name", Other: "Vhost names"}},
		{Attribute: "rabbitmq.vhost.tracing", Label: discovery_kit_api.PluralLabel{One: "Vhost tracing", Other: "Vhost tracing"}},
	}
}

func (r *rabbitVhostDiscovery) DiscoverTargets(ctx context.Context) ([]discovery_kit_api.Target, error) {
	return getAllVhosts(ctx)
}

// --- core listing ---

func getAllVhosts(ctx context.Context) ([]discovery_kit_api.Target, error) {
	handler := func(client *rabbithole.Client) ([]discovery_kit_api.Target, error) {
		out := make([]discovery_kit_api.Target, 0, 16)

		vhosts, err := client.ListVhosts()
		if err != nil {
			return nil, err
		}
		for _, vh := range vhosts {
			out = append(out, toVhostTarget(client.Endpoint, vh))
		}
		return out, nil
	}

	targets, err := FetchTargetPerClient(handler)
	if err != nil {
		log.Warn().Err(err).Msg("vhost discovery encountered errors")
	}
	return discovery_kit_commons.ApplyAttributeExcludes(targets, config.Config.DiscoveryAttributesExcludesVhosts), nil
}

func toVhostTarget(mgmtURL string, vh rabbithole.VhostInfo) discovery_kit_api.Target {
	attrs := map[string][]string{
		"rabbitmq.vhost.name":    {vh.Name},
		"rabbitmq.vhost.tracing": {fmt.Sprintf("%t", vh.Tracing)},
	}

	return discovery_kit_api.Target{
		Id:         mgmtURL + "::" + vh.Name,
		Label:      vh.Name,
		TargetType: vhostTargetId,
		Attributes: attrs,
	}
}
