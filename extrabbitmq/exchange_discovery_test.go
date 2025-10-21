// SPDX-License-Identifier: MIT

package extrabbitmq

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	rabbithole "github.com/michaelklishin/rabbit-hole/v3"
	"github.com/steadybit/extension-rabbitmq/clients"
	"github.com/steadybit/extension-rabbitmq/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExchangeDescribeTarget(t *testing.T) {
	r := &rabbitExchangeDiscovery{}
	td := r.DescribeTarget()

	assert.Equal(t, exchangeTargetId, td.Id)
	require.Len(t, td.Table.Columns, 7)
	assert.Equal(t, "steadybit.label", td.Table.Columns[0].Attribute)
	assert.Equal(t, "rabbitmq.exchange.vhost", td.Table.Columns[1].Attribute)
	assert.Equal(t, "rabbitmq.exchange.name", td.Table.Columns[2].Attribute)
	assert.Equal(t, "rabbitmq.exchange.type", td.Table.Columns[3].Attribute)
	assert.Equal(t, "rabbitmq.exchange.durable", td.Table.Columns[4].Attribute)
	assert.Equal(t, "rabbitmq.exchange.auto_delete", td.Table.Columns[5].Attribute)
	assert.Equal(t, "rabbitmq.exchange.internal", td.Table.Columns[6].Attribute)
}

func TestToExchangeTarget_Attributes(t *testing.T) {
	ex := rabbithole.ExchangeInfo{
		Name:       "amq.direct",
		Vhost:      "/",
		Type:       "direct",
		Durable:    true,
		AutoDelete: false,
		Internal:   false,
	}
	tgt := toExchangeTarget("http://mgmt", ex)

	assert.Equal(t, exchangeTargetId, tgt.TargetType)
	assert.Equal(t, "//amq.direct", tgt.Label)
	assert.Equal(t, "http://mgmt:://amq.direct", tgt.Id)

	assert.Equal(t, []string{"/"}, tgt.Attributes["rabbitmq.exchange.vhost"])
	assert.Equal(t, []string{"amq.direct"}, tgt.Attributes["rabbitmq.exchange.name"])
	assert.Equal(t, []string{"direct"}, tgt.Attributes["rabbitmq.exchange.type"])
	assert.Equal(t, []string{"true"}, tgt.Attributes["rabbitmq.exchange.durable"])
	assert.Equal(t, []string{"false"}, tgt.Attributes["rabbitmq.exchange.auto_delete"])
	assert.Equal(t, []string{"false"}, tgt.Attributes["rabbitmq.exchange.internal"])
}

func TestGetAllExchanges_MultipleEndpoints_WithOneFailing(t *testing.T) {
	// OK server with two exchanges
	ok1 := httptest.NewServer(mockMgmtExchangesHandler([]rabbithole.ExchangeInfo{
		{Name: "ex-a", Vhost: "/", Type: "direct", Durable: true},
		{Name: "ex-b", Vhost: "order", Type: "topic", Durable: false, AutoDelete: true, Internal: false},
	}, http.StatusOK))
	defer ok1.Close()

	// OK server with one exchange
	ok2 := httptest.NewServer(mockMgmtExchangesHandler([]rabbithole.ExchangeInfo{
		{Name: "ex-c", Vhost: "/", Type: "fanout", Durable: true},
	}, http.StatusOK))
	defer ok2.Close()

	// Failing server
	fail := httptest.NewServer(mockMgmtExchangesHandler(nil, http.StatusInternalServerError))
	defer fail.Close()

	// Configure endpoints
	config.Config.ManagementEndpoints = []config.ManagementEndpoint{
		{URL: ok1.URL},
		{URL: ok2.URL},
		{URL: fail.URL},
	}
	// Keep JSON in sync if other logic inspects it
	setEndpointsJSON(config.Config.ManagementEndpoints)

	// Initialize pooled management clients
	require.NoError(t, clients.Init())

	// Discover
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	targets, err := getAllExchanges(ctx)
	require.NoError(t, err)
	require.Len(t, targets, 3)

	// Basic attribute sanity on all targets
	for _, tgt := range targets {
		assert.Equal(t, exchangeTargetId, tgt.TargetType)
		assert.NotEmpty(t, tgt.Label)
		assert.NotEmpty(t, tgt.Attributes["rabbitmq.exchange.vhost"])
		assert.NotEmpty(t, tgt.Attributes["rabbitmq.exchange.name"])
		assert.NotEmpty(t, tgt.Attributes["rabbitmq.exchange.type"])
		assert.NotEmpty(t, tgt.Attributes["rabbitmq.exchange.durable"])
	}
}

// --- test helpers ---

// mockMgmtExchangesHandler simulates the RabbitMQ Management API endpoint used by ListExchanges():
//   - GET /api/exchanges
func mockMgmtExchangesHandler(exchanges []rabbithole.ExchangeInfo, status int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/exchanges" {
			http.NotFound(w, r)
			return
		}
		if status != http.StatusOK {
			http.Error(w, "error", status)
			return
		}
		_ = json.NewEncoder(w).Encode(exchanges)
	})
}
