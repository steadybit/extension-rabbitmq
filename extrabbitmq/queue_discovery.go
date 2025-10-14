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
	queueTargetId   = "com.steadybit.extension_rabbitmq.rabbitmq-queue"
	queueIcon       = ""
	queueRefreshSec = 60
)

type rabbitQueueDiscovery struct{}

var _ discovery_kit_sdk.TargetDescriber = (*rabbitQueueDiscovery)(nil)
var _ discovery_kit_sdk.AttributeDescriber = (*rabbitQueueDiscovery)(nil)

func NewRabbitQueueDiscovery(ctx context.Context) discovery_kit_sdk.TargetDiscovery {
	d := &rabbitQueueDiscovery{}
	return discovery_kit_sdk.NewCachedTargetDiscovery(
		d,
		discovery_kit_sdk.WithRefreshTargetsNow(),
		discovery_kit_sdk.WithRefreshTargetsInterval(ctx, queueRefreshSec*time.Second),
	)
}

func (r *rabbitQueueDiscovery) Describe() discovery_kit_api.DiscoveryDescription {
	return discovery_kit_api.DiscoveryDescription{
		Id: queueTargetId,
		Discover: discovery_kit_api.DescribingEndpointReferenceWithCallInterval{
			CallInterval: extutil.Ptr(fmt.Sprintf("%ds", queueRefreshSec)),
		},
	}
}

func (r *rabbitQueueDiscovery) DescribeTarget() discovery_kit_api.TargetDescription {
	return discovery_kit_api.TargetDescription{
		Id:       queueTargetId,
		Label:    discovery_kit_api.PluralLabel{One: "RabbitMQ queue", Other: "RabbitMQ queues"},
		Category: extutil.Ptr("rabbitmq"),
		Version:  extbuild.GetSemverVersionStringOrUnknown(),
		Icon:     extutil.Ptr(queueIcon),
		Table: discovery_kit_api.Table{
			Columns: []discovery_kit_api.Column{
				{Attribute: "steadybit.label"},
				{Attribute: "rabbitmq.mgmt.url"},
				{Attribute: "rabbitmq.queue.vhost"},
				{Attribute: "rabbitmq.queue.name"},
				{Attribute: "rabbitmq.queue.status"},
				{Attribute: "rabbitmq.queue.durable"},
				{Attribute: "rabbitmq.queue.auto_delete"},
			},
			OrderBy: []discovery_kit_api.OrderBy{{Attribute: "steadybit.label", Direction: "ASC"}},
		},
	}
}

func (r *rabbitQueueDiscovery) DescribeAttributes() []discovery_kit_api.AttributeDescription {
	return []discovery_kit_api.AttributeDescription{
		{Attribute: "rabbitmq.queue.vhost", Label: discovery_kit_api.PluralLabel{One: "Vhost", Other: "Vhosts"}},
		{Attribute: "rabbitmq.queue.name", Label: discovery_kit_api.PluralLabel{One: "Queue name", Other: "Queue names"}},
		{Attribute: "rabbitmq.queue.state", Label: discovery_kit_api.PluralLabel{One: "Status", Other: "Status"}},
		{Attribute: "rabbitmq.queue.durable", Label: discovery_kit_api.PluralLabel{One: "Durable", Other: "Durable"}},
		{Attribute: "rabbitmq.queue.auto_delete", Label: discovery_kit_api.PluralLabel{One: "Auto-delete", Other: "Auto-delete"}},
		{Attribute: "rabbitmq.mgmt.url", Label: discovery_kit_api.PluralLabel{One: "Management URL", Other: "Management URLs"}},
	}
}

func (r *rabbitQueueDiscovery) DiscoverTargets(ctx context.Context) ([]discovery_kit_api.Target, error) {
	return getAllQueues(ctx)
}

// --- core listing ---

func getAllQueues(ctx context.Context) ([]discovery_kit_api.Target, error) {
	result := make([]discovery_kit_api.Target, 0, 64)

	// endpoints from config: prefer comma-separated list (ManagementURLs), then single (ManagementURL), else localhost
	raw := strings.TrimSpace(config.Config.ManagementURL)
	if raw == "" {
		raw = strings.TrimSpace(config.Config.ManagementURL)
	}
	endpoints := []string{"http://localhost:15672"}
	if raw != "" {
		parts := strings.Split(raw, ",")
		endpoints = make([]string, 0, len(parts))
		for _, p := range parts {
			if s := strings.TrimSpace(p); s != "" {
				endpoints = append(endpoints, s)
			}
		}
	}

	seen := make(map[string]struct{})
	for _, host := range endpoints {
		client, err := createNewClient(host)
		if err != nil {
			log.Warn().Err(err).Str("host", host).Msg("failed to initialize rabbitmq management client")
			continue
		}

		qs, err := client.ListQueues()
		if err != nil {
			log.Warn().Err(err).Str("host", host).Msg("failed to list queues")
			continue
		}

		for _, q := range qs {
			id := host + "::" + q.Vhost + "/" + q.Name
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			result = append(result, toQueueTarget(host, q))
		}
	}

	// optional exclude list: DiscoveryAttributesExcludesQueues
	return discovery_kit_commons.ApplyAttributeExcludes(result, config.Config.DiscoveryAttributesExcludesQueues), nil
}

func toQueueTarget(mgmtURL string, q rabbithole.QueueInfo) discovery_kit_api.Target {
	label := q.Vhost + "/" + q.Name
	attrs := map[string][]string{
		"rabbitmq.queue.vhost":                   {q.Vhost},
		"rabbitmq.queue.name":                    {q.Name},
		"rabbitmq.queue.status":                  {q.Status},
		"rabbitmq.queue.consumers":               {fmt.Sprintf("%d", q.Consumers)},
		"rabbitmq.queue.messages":                {fmt.Sprintf("%d", q.Messages)},
		"rabbitmq.queue.messages_ready":          {fmt.Sprintf("%d", q.MessagesReady)},
		"rabbitmq.queue.messages_unacknowledged": {fmt.Sprintf("%d", q.MessagesUnacknowledged)},
		"rabbitmq.queue.durable":                 {fmt.Sprintf("%t", q.Durable)},
		"rabbitmq.queue.auto_delete":             {fmt.Sprintf("%t", q.AutoDelete)},
		"rabbitmq.mgmt.url":                      {mgmtURL},
	}

	return discovery_kit_api.Target{
		Id:         mgmtURL + "::" + label,
		Label:      label,
		TargetType: queueTargetId,
		Attributes: attrs,
	}
}
