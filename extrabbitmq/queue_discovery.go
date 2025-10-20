package extrabbitmq

import (
	"context"
	"fmt"
	"net/url"
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
	queueTargetId   = "com.steadybit.extension_rabbitmq.queue"
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
		Icon:     extutil.Ptr(rabbitMQIcon),
		Table: discovery_kit_api.Table{
			Columns: []discovery_kit_api.Column{
				{Attribute: "steadybit.label"},
				{Attribute: "rabbitmq.cluster.name"},
				{Attribute: "rabbitmq.queue.vhost"},
				{Attribute: "rabbitmq.queue.name"},
				{Attribute: "rabbitmq.amqp.url"},
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
		{Attribute: "rabbitmq.queue.status", Label: discovery_kit_api.PluralLabel{One: "Status", Other: "Status"}},
		{Attribute: "rabbitmq.queue.durable", Label: discovery_kit_api.PluralLabel{One: "Durable", Other: "Durable"}},
		{Attribute: "rabbitmq.queue.auto_delete", Label: discovery_kit_api.PluralLabel{One: "Auto-delete", Other: "Auto-delete"}},
		{Attribute: "rabbitmq.amqp.url", Label: discovery_kit_api.PluralLabel{One: "AMQP URL", Other: "AMQP URLs"}},
		{Attribute: "rabbitmq.cluster.name", Label: discovery_kit_api.PluralLabel{One: "Cluster name", Other: "Cluster names"}},
	}
}

func (r *rabbitQueueDiscovery) DiscoverTargets(ctx context.Context) ([]discovery_kit_api.Target, error) {
	return getAllQueues(ctx)
}

// --- core listing ---

func getAllQueues(ctx context.Context) ([]discovery_kit_api.Target, error) {
	handler := func(client *rabbithole.Client) ([]discovery_kit_api.Target, error) {
		out := make([]discovery_kit_api.Target, 0, 32)

		qs, err := client.ListQueues()
		if err != nil {
			return nil, err
		}
		for _, q := range qs {
			amqpURL := resolveAMQPURLForClient(client.Endpoint)
			cn, _ := client.GetClusterName()
			clusterName := ""
			if cn != nil {
				clusterName = cn.Name
			}

			out = append(out, toQueueTarget(client.Endpoint, amqpURL, q, clusterName))
		}
		return out, nil
	}

	targets, err := FetchTargetPerClient(handler)
	if err != nil {
		// FetchTargetPerClient already logs per-endpoint errors; only return fatal errors
		log.Warn().Err(err).Msg("queue discovery encountered errors")
	}
	return discovery_kit_commons.ApplyAttributeExcludes(targets, config.Config.DiscoveryAttributesExcludesQueues), nil
}

func toQueueTarget(mgmtURL, amqpURL string, q rabbithole.QueueInfo, cluster string) discovery_kit_api.Target {
	label := q.Vhost + "/" + q.Name
	attrs := map[string][]string{
		"rabbitmq.queue.vhost":  {q.Vhost},
		"rabbitmq.queue.name":   {q.Name},
		"rabbitmq.cluster.name": {cluster},
		"rabbitmq.amqp.url":     {amqpURL},
		"rabbitmq.queue.status": {q.Status},
		"rabbitmq.mgmt.url":     {mgmtURL},
	}

	return discovery_kit_api.Target{
		Id:         mgmtURL + "::" + label,
		Label:      label,
		TargetType: queueTargetId,
		Attributes: attrs,
	}
}

func resolveAMQPURLForClient(mgmtEndpoint string) string {
	u, err := url.Parse(mgmtEndpoint)
	if err != nil {
		return ""
	}
	host := u.Host
	for i := range config.Config.ManagementEndpoints {
		ep := &config.Config.ManagementEndpoints[i]
		epu, err := url.Parse(ep.URL)
		if err != nil {
			continue
		}
		if epu.Host == host && ep.AMQP != nil {
			return ep.AMQP.URL
		}
	}
	return ""
}

//func getQueueBoundExchanges(client *rabbithole.Client, vhost, queue string) []string {
//	bindings, err := client.ListQueueBindings(vhost, queue)
//	if err != nil {
//		log.Warn().Err(err).Str("vhost", vhost).Str("queue", queue).Msg("failed to list queue bindings")
//		return nil
//	}
//	// Collect unique sources (exchanges) for this queue
//	set := make(map[string]struct{})
//	for _, b := range bindings {
//		if b.Source != "" { // source is the exchange name
//			set[b.Source] = struct{}{}
//		}
//	}
//	if len(set) == 0 {
//		return nil
//	}
//	out := make([]string, 0, len(set))
//	for s := range set {
//		out = append(out, s)
//	}
//	return out
//}
