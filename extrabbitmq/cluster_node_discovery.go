// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package extrabbitmq

import (
	"context"
	"fmt"
	"strings"
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

const (
	nodeTargetId   = "com.steadybit.extension_rabbitmq.rabbitmq-node"
	nodeIcon       = ""
	nodeRefreshSec = 60
)

type rabbitNodeDiscovery struct{}

var _ discovery_kit_sdk.TargetDescriber = (*rabbitNodeDiscovery)(nil)
var _ discovery_kit_sdk.AttributeDescriber = (*rabbitNodeDiscovery)(nil)

func NewRabbitNodeDiscovery(ctx context.Context) discovery_kit_sdk.TargetDiscovery {
	d := &rabbitNodeDiscovery{}
	return discovery_kit_sdk.NewCachedTargetDiscovery(
		d,
		discovery_kit_sdk.WithRefreshTargetsNow(),
		discovery_kit_sdk.WithRefreshTargetsInterval(ctx, nodeRefreshSec*time.Second),
	)
}

func (r *rabbitNodeDiscovery) Describe() discovery_kit_api.DiscoveryDescription {
	return discovery_kit_api.DiscoveryDescription{
		Id: nodeTargetId,
		Discover: discovery_kit_api.DescribingEndpointReferenceWithCallInterval{
			CallInterval: extutil.Ptr(fmt.Sprintf("%ds", nodeRefreshSec)),
		},
	}
}

func (r *rabbitNodeDiscovery) DescribeTarget() discovery_kit_api.TargetDescription {
	return discovery_kit_api.TargetDescription{
		Id:       nodeTargetId,
		Label:    discovery_kit_api.PluralLabel{One: "RabbitMQ node", Other: "RabbitMQ nodes"},
		Category: extutil.Ptr("rabbitmq"),
		Version:  extbuild.GetSemverVersionStringOrUnknown(),
		Icon:     extutil.Ptr(nodeIcon),
		Table: discovery_kit_api.Table{
			Columns: []discovery_kit_api.Column{
				{Attribute: "steadybit.label"},
				{Attribute: "rabbitmq.mgmt.url"},
				{Attribute: "rabbitmq.node.name"},
				{Attribute: "rabbitmq.node.type"},
				{Attribute: "rabbitmq.node.running"},
			},
			OrderBy: []discovery_kit_api.OrderBy{{Attribute: "steadybit.label", Direction: "ASC"}},
		},
	}
}

func (r *rabbitNodeDiscovery) DescribeAttributes() []discovery_kit_api.AttributeDescription {
	return []discovery_kit_api.AttributeDescription{
		{Attribute: "rabbitmq.node.name", Label: discovery_kit_api.PluralLabel{One: "Node name", Other: "Node names"}},
		{Attribute: "rabbitmq.node.type", Label: discovery_kit_api.PluralLabel{One: "Node type", Other: "Node types"}},
		{Attribute: "rabbitmq.node.running", Label: discovery_kit_api.PluralLabel{One: "Running state", Other: "Running states"}},
		{Attribute: "rabbitmq.mgmt.url", Label: discovery_kit_api.PluralLabel{One: "Management URL", Other: "Management URLs"}},
	}
}

func (r *rabbitNodeDiscovery) DiscoverTargets(ctx context.Context) ([]discovery_kit_api.Target, error) {
	return getAllNodes(ctx)
}

// --- core listing ---

func getAllNodes(ctx context.Context) ([]discovery_kit_api.Target, error) {
	result := make([]discovery_kit_api.Target, 0, 16)

	endpoints := strings.Split(strings.TrimSpace(config.Config.ManagementURL), ",")
	if len(endpoints) == 0 || (len(endpoints) == 1 && endpoints[0] == "") {
		if config.Config.ManagementURL != "" {
			endpoints = []string{config.Config.ManagementURL}
		} else {
			endpoints = []string{"http://localhost:15672"}
		}
	}

	seen := make(map[string]struct{})
	for _, raw := range endpoints {
		host := strings.TrimSpace(raw)
		if host == "" {
			continue
		}

		client, err := createNewClient(host)
		if err != nil {
			log.Warn().Err(err).Str("host", host).Msg("failed to initialize rabbitmq management client")
			continue
		}

		nodes, err := client.ListNodes()
		if err != nil {
			log.Warn().Err(err).Str("host", host).Msg("failed to list nodes")
			continue
		}

		for _, n := range nodes {
			id := host + "::" + n.Name
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			result = append(result, toNodeTarget(host, n))
		}
	}

	return discovery_kit_commons.ApplyAttributeExcludes(result, nil), nil
}

func toNodeTarget(mgmtURL string, n rabbithole.NodeInfo) discovery_kit_api.Target {
	attrs := map[string][]string{
		"rabbitmq.node.name":    {n.Name},
		"rabbitmq.node.type":    {n.NodeType},
		"rabbitmq.node.running": {fmt.Sprintf("%t", n.IsRunning)},
		"rabbitmq.mgmt.url":     {mgmtURL},
	}

	return discovery_kit_api.Target{
		Id:         mgmtURL + "::" + n.Name,
		Label:      n.Name,
		TargetType: nodeTargetId,
		Attributes: attrs,
	}
}
