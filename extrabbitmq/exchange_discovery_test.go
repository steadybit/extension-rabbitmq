// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2026 Steadybit GmbH

package extrabbitmq

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/steadybit/extension-rabbitmq/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	rabbithole "github.com/michaelklishin/rabbit-hole/v3"
)

func mockExchangeMgmtServer(t *testing.T, exchanges []rabbithole.ExchangeInfo, cluster string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/api/exchanges", func(w http.ResponseWriter, r *http.Request) {
		// the discovery fetches with pagination and a columns filter
		type pagedExchangesResponse struct {
			Items      []rabbithole.ExchangeInfo `json:"items"`
			Page       int                       `json:"page"`
			PageCount  int                       `json:"page_count"`
			TotalCount int                       `json:"total_count"`
		}
		_ = json.NewEncoder(w).Encode(pagedExchangesResponse{
			Items:      exchanges,
			Page:       1,
			PageCount:  1,
			TotalCount: len(exchanges),
		})
	})
	// rabbit-hole requests "cluster-name/" with a trailing slash — the pattern must match both
	mux.HandleFunc("/api/cluster-name/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(struct {
			Name string `json:"name"`
		}{Name: cluster})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return httptest.NewServer(mux)
}

func Test_Exchange_DiscoveryDescribe(t *testing.T) {
	desc := (&rabbitExchangeDiscovery{}).Describe()
	require.Equal(t, exchangeTargetId, desc.Id)
	require.NotNil(t, desc.Discover.CallInterval)
}

func Test_Exchange_DescribeTarget(t *testing.T) {
	td := (&rabbitExchangeDiscovery{}).DescribeTarget()
	require.Equal(t, exchangeTargetId, td.Id)
	require.Equal(t, "RabbitMQ Exchange", td.Label.One)
	require.Equal(t, "RabbitMQ Exchanges", td.Label.Other)
	require.Equal(t, "rabbitmq", *td.Category)
	require.NotEmpty(t, td.Table.Columns)
}

func Test_Exchange_DescribeAttributes_AllPresent(t *testing.T) {
	attrs := (&rabbitExchangeDiscovery{}).DescribeAttributes()
	want := map[string]struct{}{
		"rabbitmq.exchange.vhost":       {},
		"rabbitmq.exchange.name":        {},
		"rabbitmq.exchange.type":        {},
		"rabbitmq.exchange.durable":     {},
		"rabbitmq.exchange.auto_delete": {},
	}
	require.Len(t, attrs, len(want))
	for _, a := range attrs {
		_, ok := want[a.Attribute]
		assert.True(t, ok, "unexpected attribute %q", a.Attribute)
		delete(want, a.Attribute)
	}
	require.Empty(t, want, "missing attributes %v", want)
}

func Test_Exchange_Discovery_FiltersBuiltinsAndInternal(t *testing.T) {
	exchanges := []rabbithole.ExchangeInfo{
		{Vhost: "/", Name: "", Type: "direct", Durable: true},           // default exchange
		{Vhost: "/", Name: "amq.topic", Type: "topic", Durable: true},   // built-in
		{Vhost: "/", Name: "amq.fanout", Type: "fanout", Durable: true}, // built-in
		{Vhost: "/", Name: "internal.ex", Type: "topic", Durable: true, Internal: true},
		{Vhost: "/", Name: "demo.topic", Type: "topic", Durable: true},
		{Vhost: "orders", Name: "order.events", Type: "fanout", Durable: true, AutoDelete: true},
	}
	srv := mockExchangeMgmtServer(t, exchanges, "test-cluster")
	defer srv.Close()

	setEndpointsJSON([]config.ManagementEndpoint{
		{URL: srv.URL, AMQP: &config.AMQPOptions{URL: "amqp://broker:5672", Vhost: "/"}},
	})

	targets, err := getAllExchanges(context.Background())
	require.NoError(t, err)
	require.Len(t, targets, 2)

	demo := findTargetByLabel(targets, "//demo.topic")
	require.NotNil(t, demo, "expected //demo.topic target, got %v", targets)
	assertAttr(t, *demo, "rabbitmq.exchange.name", "demo.topic")
	assertAttr(t, *demo, "rabbitmq.exchange.type", "topic")
	assertAttr(t, *demo, "rabbitmq.exchange.durable", "true")
	assertAttr(t, *demo, "rabbitmq.exchange.auto_delete", "false")
	assertAttr(t, *demo, "rabbitmq.cluster.name", "test-cluster")
	assertAttr(t, *demo, "rabbitmq.amqp.url", "amqp://broker:5672")
	assert.Equal(t, exchangeTargetId, demo.TargetType)

	orders := findTargetByLabel(targets, "orders/order.events")
	require.NotNil(t, orders)
	assertAttr(t, *orders, "rabbitmq.exchange.vhost", "orders")
	assertAttr(t, *orders, "rabbitmq.exchange.type", "fanout")
	assertAttr(t, *orders, "rabbitmq.exchange.auto_delete", "true")
}
