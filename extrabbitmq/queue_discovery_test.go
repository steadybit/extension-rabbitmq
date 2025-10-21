// SPDX-License-Identifier: MIT

package extrabbitmq

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	rabbithole "github.com/michaelklishin/rabbit-hole/v3"
	"github.com/steadybit/discovery-kit/go/discovery_kit_api"
	"github.com/steadybit/extension-rabbitmq/clients"
	"github.com/steadybit/extension-rabbitmq/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueueDescribeTarget(t *testing.T) {
	r := &rabbitQueueDiscovery{}
	td := r.DescribeTarget()

	assert.Equal(t, queueTargetId, td.Id)
	require.Len(t, td.Table.Columns, 8)
	assert.Equal(t, "steadybit.label", td.Table.Columns[0].Attribute)
	assert.Equal(t, "rabbitmq.cluster.name", td.Table.Columns[1].Attribute)
	assert.Equal(t, "rabbitmq.queue.vhost", td.Table.Columns[2].Attribute)
	assert.Equal(t, "rabbitmq.queue.name", td.Table.Columns[3].Attribute)
	assert.Equal(t, "rabbitmq.amqp.url", td.Table.Columns[4].Attribute)
	assert.Equal(t, "rabbitmq.queue.status", td.Table.Columns[5].Attribute)
	assert.Equal(t, "rabbitmq.queue.durable", td.Table.Columns[6].Attribute)
	assert.Equal(t, "rabbitmq.queue.auto_delete", td.Table.Columns[7].Attribute)
}

func TestToQueueTarget_Attributes(t *testing.T) {
	q := rabbithole.QueueInfo{
		Name:   "order",
		Vhost:  "order",
		Status: "running",
	}
	tgt := toQueueTarget("http://mgmt", "amqp://broker:5672/order", q, "cluster-A")

	assert.Equal(t, queueTargetId, tgt.TargetType)
	assert.Equal(t, "order/order", tgt.Label)
	assert.Equal(t, "http://mgmt::order/order", tgt.Id)

	assert.Equal(t, []string{"order"}, tgt.Attributes["rabbitmq.queue.vhost"])
	assert.Equal(t, []string{"order"}, tgt.Attributes["rabbitmq.queue.name"])
	assert.Equal(t, []string{"cluster-A"}, tgt.Attributes["rabbitmq.cluster.name"])
	assert.Equal(t, []string{"amqp://broker:5672/order"}, tgt.Attributes["rabbitmq.amqp.url"])
	assert.Equal(t, []string{"running"}, tgt.Attributes["rabbitmq.queue.status"])
	assert.Equal(t, []string{"http://mgmt"}, tgt.Attributes["rabbitmq.mgmt.url"])
}

func TestResolveAMQPURLForClient(t *testing.T) {
	// Configure two endpoints; only first has AMQP mapping
	config.Config.ManagementEndpoints = []config.ManagementEndpoint{
		{
			URL: "http://one.local:15672",
			AMQP: &config.AMQPOptions{
				URL:   "amqp://one.local:5672/",
				Vhost: "/",
			},
		},
		{
			URL: "http://two.local:15672",
		},
	}
	// Matching by host
	u := resolveAMQPURLForClient("http://one.local:15672")
	assert.Equal(t, "amqp://one.local:5672/", u)

	u2 := resolveAMQPURLForClient("http://two.local:15672")
	assert.Equal(t, "", u2)
}

func TestGetAllQueues_MultipleEndpoints_WithClusterName(t *testing.T) {
	// OK server #1: two queues, cluster name A
	ok1 := httptest.NewServer(mockMgmtQueuesHandler(
		[]rabbithole.QueueInfo{
			{Name: "q1", Vhost: "/", Status: "running"},
			{Name: "q2", Vhost: "order", Status: "idle"},
		},
		"cluster-A",
		http.StatusOK,
	))
	defer ok1.Close()

	// OK server #2: one queue, cluster name B
	ok2 := httptest.NewServer(mockMgmtQueuesHandler(
		[]rabbithole.QueueInfo{
			{Name: "q3", Vhost: "/", Status: "running"},
		},
		"cluster-B",
		http.StatusOK,
	))
	defer ok2.Close()

	// Failing server: 500 on /api/queues
	fail := httptest.NewServer(mockMgmtQueuesHandler(nil, "cluster-C", http.StatusInternalServerError))
	defer fail.Close()

	// Wire config with AMQP mapping for the two OK endpoints
	config.Config.ManagementEndpoints = []config.ManagementEndpoint{
		{
			URL: ok1.URL,
			AMQP: &config.AMQPOptions{
				URL:   "amqp://ok1:5672/",
				Vhost: "/",
			},
		},
		{
			URL: ok2.URL,
			AMQP: &config.AMQPOptions{
				URL:   "amqp://ok2:5672/",
				Vhost: "/",
			},
		},
		{URL: fail.URL},
	}
	require.NoError(t, clients.Init())

	// Discover
	ctx := context.Background()
	targets, err := getAllQueues(ctx)
	require.NoError(t, err)

	// Expect 3 targets (2 + 1, failing endpoint contributes none)
	require.Len(t, targets, 3)

	// Validate essential attributes
	for _, tgt := range targets {
		assert.Equal(t, queueTargetId, tgt.TargetType)
		assert.NotEmpty(t, tgt.Label)
		assert.NotEmpty(t, tgt.Attributes["rabbitmq.queue.vhost"])
		assert.NotEmpty(t, tgt.Attributes["rabbitmq.queue.name"])
		assert.NotEmpty(t, tgt.Attributes["rabbitmq.mgmt.url"])
		assert.NotEmpty(t, tgt.Attributes["rabbitmq.amqp.url"])
		// cluster name resolved via /api/cluster-name
		assert.NotEmpty(t, tgt.Attributes["rabbitmq.cluster.name"])
	}
}

// --- test helpers ---

// mockMgmtQueuesHandler simulates endpoints used by getAllQueues():
//   - GET /api/queues
//   - GET /api/cluster-name (rabbit-hole client.GetClusterName)
func mockMgmtQueuesHandler(queues []rabbithole.QueueInfo, clusterName string, queuesStatus int) http.Handler {
	type clusterNameDoc struct {
		Name string `json:"name"`
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/queues":
			if queuesStatus != http.StatusOK {
				http.Error(w, "error", queuesStatus)
				return
			}
			_ = json.NewEncoder(w).Encode(queues)
			return
		case "/api/cluster-name":
			_ = json.NewEncoder(w).Encode(clusterNameDoc{Name: clusterName})
			return
		default:
			http.NotFound(w, r)
		}
	})
}

// Minimal definitions matching config package used in tests, to help IDEs:
// Remove if your config exports these already. Tests rely on the real package types.
var _ discovery_kit_api.Target // keep import alive
