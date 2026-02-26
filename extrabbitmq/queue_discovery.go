package extrabbitmq

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
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
	queueTargetId = "com.steadybit.extension_rabbitmq.queue"
)

type rabbitQueueDiscovery struct{}

var _ discovery_kit_sdk.TargetDescriber = (*rabbitQueueDiscovery)(nil)
var _ discovery_kit_sdk.AttributeDescriber = (*rabbitQueueDiscovery)(nil)

func NewRabbitQueueDiscovery(ctx context.Context) discovery_kit_sdk.TargetDiscovery {
	d := &rabbitQueueDiscovery{}
	return discovery_kit_sdk.NewCachedTargetDiscovery(
		d,
		discovery_kit_sdk.WithRefreshTargetsNow(),
		discovery_kit_sdk.WithRefreshTargetsInterval(ctx, time.Duration(config.Config.DiscoveryIntervalQueueSeconds)*time.Second),
	)
}

func (r *rabbitQueueDiscovery) Describe() discovery_kit_api.DiscoveryDescription {
	return discovery_kit_api.DiscoveryDescription{
		Id: queueTargetId,
		Discover: discovery_kit_api.DescribingEndpointReferenceWithCallInterval{
			CallInterval: extutil.Ptr(fmt.Sprintf("%ds", config.Config.DiscoveryIntervalQueueSeconds)),
		},
	}
}

func (r *rabbitQueueDiscovery) DescribeTarget() discovery_kit_api.TargetDescription {
	return discovery_kit_api.TargetDescription{
		Id:       queueTargetId,
		Label:    discovery_kit_api.PluralLabel{One: "RabbitMQ Queue", Other: "RabbitMQ Queues"},
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
		{Attribute: "rabbitmq.queue.max_length", Label: discovery_kit_api.PluralLabel{One: "Max length", Other: "Max lengths"}},
		{Attribute: "rabbitmq.amqp.url", Label: discovery_kit_api.PluralLabel{One: "AMQP URL", Other: "AMQP URLs"}},
		{Attribute: "rabbitmq.cluster.name", Label: discovery_kit_api.PluralLabel{One: "Cluster name", Other: "Cluster names"}},
	}
}

func (r *rabbitQueueDiscovery) DiscoverTargets(ctx context.Context) ([]discovery_kit_api.Target, error) {
	return getAllQueues(ctx)
}

func getAllQueues(ctx context.Context) ([]discovery_kit_api.Target, error) {
	handler := func(client *rabbithole.Client, targetType string) ([]discovery_kit_api.Target, error) {
		out := make([]discovery_kit_api.Target, 0, 32)

		var qs []rabbithole.QueueInfo
		page := 1
		pageSize := 400

		for {
			params := url.Values{}
			params.Set("page", strconv.Itoa(page))
			params.Set("page_size", strconv.Itoa(pageSize))

			paged, err := client.PagedListQueuesWithParameters(params)
			if err != nil {
				return nil, err
			}
			if len(paged.Items) == 0 {
				break
			}

			qs = append(qs, paged.Items...)

			// Stop if we have fetched all items or reached the last page
			if len(qs) >= paged.TotalCount || page >= paged.PageCount {
				break
			}
			page++
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

	targets, err := FetchTargetPerClient(handler, queueTargetId)
	if err != nil {
		return nil, err
	}
	return discovery_kit_commons.ApplyAttributeExcludes(targets, config.Config.DiscoveryAttributesExcludesQueues), nil
}

func toQueueTarget(mgmtURL, amqpURL string, q rabbithole.QueueInfo, cluster string) discovery_kit_api.Target {
	label := q.Vhost + "/" + q.Name
	// Extract max-length from queue arguments (if defined)
	var maxLengthStr string
	if q.Arguments != nil {
		if val, ok := q.Arguments["x-max-length"]; ok {
			switch v := val.(type) {
			case float64:
				maxLengthStr = fmt.Sprintf("%.0f", v)
			case int:
				maxLengthStr = fmt.Sprintf("%d", v)
			case int64:
				maxLengthStr = fmt.Sprintf("%d", v)
			case string:
				maxLengthStr = v
			default:
				maxLengthStr = fmt.Sprintf("%v", v)
			}
		}
	}
	if maxLengthStr == "" {
		maxLengthStr = "unlimited"
	}
	attrs := map[string][]string{
		"rabbitmq.queue.vhost":      {q.Vhost},
		"rabbitmq.queue.name":       {q.Name},
		"rabbitmq.cluster.name":     {cluster},
		"rabbitmq.amqp.url":         {amqpURL},
		"rabbitmq.queue.status":     {q.Status},
		"rabbitmq.mgmt.url":         {mgmtURL},
		"rabbitmq.queue.max_length": {maxLengthStr},
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
