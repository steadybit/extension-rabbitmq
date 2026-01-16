// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package extrabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/steadybit/discovery-kit/go/discovery_kit_api"
	"github.com/steadybit/extension-rabbitmq/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	rabbithole "github.com/michaelklishin/rabbit-hole/v3"
)

/***********************
 * Test helpers
 ***********************/

func mockQueueMgmtServer(t *testing.T, queues []rabbithole.QueueInfo, cluster string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/api/queues", func(w http.ResponseWriter, r *http.Request) {
		type pagedQueuesResponse struct {
			Items      []rabbithole.QueueInfo `json:"items"`
			Page       int                    `json:"page"`
			PageCount  int                    `json:"page_count"`
			PageSize   int                    `json:"page_size"`
			TotalCount int                    `json:"total_count"`
		}

		resp := pagedQueuesResponse{
			Items:      queues,
			Page:       1,
			PageCount:  1,
			PageSize:   len(queues),
			TotalCount: len(queues),
		}

		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/cluster-name", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(struct {
			Name string `json:"name"`
		}{Name: cluster})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// rabbithole sometimes probes other endpoints; keep them 200
		w.WriteHeader(http.StatusOK)
	})
	return httptest.NewServer(mux)
}

/***********************
 * Describe / Target / Attributes
 ***********************/

func Test_Queue_DiscoveryDescribe(t *testing.T) {
	desc := (&rabbitQueueDiscovery{}).Describe()
	require.Equal(t, queueTargetId, desc.Id)
	require.NotNil(t, desc.Discover.CallInterval)
}

func Test_Queue_DescribeTarget_Table(t *testing.T) {
	td := (&rabbitQueueDiscovery{}).DescribeTarget()

	require.Equal(t, queueTargetId, td.Id)
	require.Equal(t, "RabbitMQ Queue", td.Label.One)
	require.Equal(t, "RabbitMQ Queues", td.Label.Other)
	require.NotNil(t, td.Category)
	require.Equal(t, "rabbitmq", *td.Category)

	wantCols := []string{
		"steadybit.label",
		"rabbitmq.cluster.name",
		"rabbitmq.queue.vhost",
		"rabbitmq.queue.name",
		"rabbitmq.amqp.url",
		"rabbitmq.queue.status",
		"rabbitmq.queue.durable",
		"rabbitmq.queue.auto_delete",
	}
	require.Len(t, td.Table.Columns, len(wantCols))
	for i, c := range td.Table.Columns {
		assert.Equalf(t, wantCols[i], c.Attribute, "column %d mismatch", i)
	}

	require.Len(t, td.Table.OrderBy, 1)
	require.Equal(t, "steadybit.label", td.Table.OrderBy[0].Attribute)
	require.Equal(t, discovery_kit_api.OrderByDirection("ASC"), td.Table.OrderBy[0].Direction)
}

func Test_Queue_DescribeAttributes_AllPresent(t *testing.T) {
	attrs := (&rabbitQueueDiscovery{}).DescribeAttributes()
	want := map[string]struct{}{
		"rabbitmq.queue.vhost":       {},
		"rabbitmq.queue.name":        {},
		"rabbitmq.queue.status":      {},
		"rabbitmq.queue.durable":     {},
		"rabbitmq.queue.auto_delete": {},
		"rabbitmq.queue.max_length":  {},
		"rabbitmq.amqp.url":          {},
		"rabbitmq.cluster.name":      {},
	}
	require.Len(t, attrs, len(want))
	for _, a := range attrs {
		_, ok := want[a.Attribute]
		assert.True(t, ok, "unexpected attribute %q", a.Attribute)
		delete(want, a.Attribute)
	}
	require.Empty(t, want, "missing attributes %v", want)
}

/***********************
 * toQueueTarget mapping
 ***********************/

func Test_toQueueTarget_MaxLength_Extraction(t *testing.T) {
	mgmt := "http://mgmt:15672"
	amqp := "amqp://broker:5672"
	cluster := "c1"

	t.Run("float64", func(t *testing.T) {
		q := rabbithole.QueueInfo{Vhost: "v", Name: "q", Status: "running", Arguments: map[string]interface{}{"x-max-length": float64(123)}}
		tgt := toQueueTarget(mgmt, amqp, q, cluster)
		assert.Equal(t, []string{"123"}, tgt.Attributes["rabbitmq.queue.max_length"])
	})
	t.Run("int", func(t *testing.T) {
		q := rabbithole.QueueInfo{Vhost: "v", Name: "q", Status: "running", Arguments: map[string]interface{}{"x-max-length": int(456)}}
		tgt := toQueueTarget(mgmt, amqp, q, cluster)
		assert.Equal(t, []string{"456"}, tgt.Attributes["rabbitmq.queue.max_length"])
	})
	t.Run("int64", func(t *testing.T) {
		q := rabbithole.QueueInfo{Vhost: "v", Name: "q", Status: "running", Arguments: map[string]interface{}{"x-max-length": int64(789)}}
		tgt := toQueueTarget(mgmt, amqp, q, cluster)
		assert.Equal(t, []string{"789"}, tgt.Attributes["rabbitmq.queue.max_length"])
	})
	t.Run("string", func(t *testing.T) {
		q := rabbithole.QueueInfo{Vhost: "v", Name: "q", Status: "running", Arguments: map[string]interface{}{"x-max-length": "42"}}
		tgt := toQueueTarget(mgmt, amqp, q, cluster)
		assert.Equal(t, []string{"42"}, tgt.Attributes["rabbitmq.queue.max_length"])
	})
	t.Run("missing -> unlimited", func(t *testing.T) {
		q := rabbithole.QueueInfo{Vhost: "v", Name: "q", Status: "running"}
		tgt := toQueueTarget(mgmt, amqp, q, cluster)
		assert.Equal(t, []string{"unlimited"}, tgt.Attributes["rabbitmq.queue.max_length"])
	})
	t.Run("other type -> fmt", func(t *testing.T) {
		q := rabbithole.QueueInfo{Vhost: "v", Name: "q", Status: "running", Arguments: map[string]interface{}{"x-max-length": []int{1, 2}}}
		tgt := toQueueTarget(mgmt, amqp, q, cluster)
		assert.Equal(t, []string{"[1 2]"}, tgt.Attributes["rabbitmq.queue.max_length"])
	})
}

func Test_toQueueTarget_BaseAttributes(t *testing.T) {
	q := rabbithole.QueueInfo{
		Vhost:  "order",
		Name:   "q1",
		Status: "running",
	}
	tgt := toQueueTarget("http://rabbit:15672", "amqp://rabbit:5672", q, "cluster-Z")
	assert.Equal(t, "order/q1", tgt.Label)
	assert.Equal(t, queueTargetId, tgt.TargetType)
	assert.Equal(t, "http://rabbit:15672::order/q1", tgt.Id)

	assert.Equal(t, []string{"order"}, tgt.Attributes["rabbitmq.queue.vhost"])
	assert.Equal(t, []string{"q1"}, tgt.Attributes["rabbitmq.queue.name"])
	assert.Equal(t, []string{"cluster-Z"}, tgt.Attributes["rabbitmq.cluster.name"])
	assert.Equal(t, []string{"amqp://rabbit:5672"}, tgt.Attributes["rabbitmq.amqp.url"])
	assert.Equal(t, []string{"running"}, tgt.Attributes["rabbitmq.queue.status"])
	assert.Equal(t, []string{"http://rabbit:15672"}, tgt.Attributes["rabbitmq.mgmt.url"])
}

/***********************
 * resolveAMQPURLForClient
 ***********************/

func Test_resolveAMQPURLForClient_MatchesHost(t *testing.T) {
	orig := config.Config.ManagementEndpoints
	defer func() { config.Config.ManagementEndpoints = orig }()

	srv := httptest.NewServer(http.NewServeMux())
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	host := u.Host

	config.Config.ManagementEndpoints = []config.ManagementEndpoint{
		{
			URL: srv.URL,
			AMQP: &config.AMQPOptions{
				URL: "amqp://broker1:5672",
			},
		},
		{
			URL: "http://other:15672",
			AMQP: &config.AMQPOptions{
				URL: "amqp://broker2:5672",
			},
		},
	}

	got := resolveAMQPURLForClient(fmt.Sprintf("http://%s", host))
	assert.Equal(t, "amqp://broker1:5672", got)
}

func Test_resolveAMQPURLForClient_NoMatchOrErrors(t *testing.T) {
	orig := config.Config.ManagementEndpoints
	defer func() { config.Config.ManagementEndpoints = orig }()

	config.Config.ManagementEndpoints = []config.ManagementEndpoint{
		{URL: "http://bad host###", AMQP: &config.AMQPOptions{URL: "amqp://ignored"}},
		{URL: "http://foo:15672"}, // no AMQP
	}

	assert.Equal(t, "", resolveAMQPURLForClient(":// not a url")) // parse error
	assert.Equal(t, "", resolveAMQPURLForClient("http://bar:15672"))
}

/***********************
 * getAllQueues end-to-end with mocked mgmt API
 ***********************/

func Test_getAllQueues_SingleEndpoint_OK(t *testing.T) {
	orig := config.Config.ManagementEndpoints
	defer func() { config.Config.ManagementEndpoints = orig }()

	queues := []rabbithole.QueueInfo{
		{Vhost: "order", Name: "q1", Status: "running", Arguments: map[string]interface{}{"x-max-length": 100.0}},
		{Vhost: "order", Name: "q2", Status: "idle"},
	}
	s := mockQueueMgmtServer(t, queues, "cluster-A")
	defer s.Close()

	// Make sure the management URL host matches what resolveAMQPURLForClient will compare
	u, _ := url.Parse(s.URL)

	config.Config.ManagementEndpoints = []config.ManagementEndpoint{
		{
			URL: s.URL,
			AMQP: &config.AMQPOptions{
				URL: "amqp://" + u.Host, // arbitrary but detectable
			},
			Username: "u",
			Password: "p",
		},
	}

	// Call discovery
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	targets, err := getAllQueues(ctx)
	require.NoError(t, err)
	require.Len(t, targets, 2)

	// Find helpers
	find := func(vhost, name string) discovery_kit_api.Target {
		want := vhost + "/" + name
		for _, t0 := range targets {
			if t0.Label == want {
				return t0
			}
		}
		return discovery_kit_api.Target{}
	}

	q1 := find("order", "q1")
	require.NotEmpty(t, q1.Id)
	assert.Equal(t, []string{"amqp://" + u.Host}, q1.Attributes["rabbitmq.amqp.url"])
	assert.Equal(t, []string{"100"}, q1.Attributes["rabbitmq.queue.max_length"])
	assert.Equal(t, []string{"running"}, q1.Attributes["rabbitmq.queue.status"])

	q2 := find("order", "q2")
	require.NotEmpty(t, q2.Id)
	assert.Equal(t, []string{"unlimited"}, q2.Attributes["rabbitmq.queue.max_length"])
	assert.Equal(t, []string{"idle"}, q2.Attributes["rabbitmq.queue.status"])
}

func Test_getAllQueues_PartialFailure_HealthyStillReturned(t *testing.T) {
	orig := config.Config.ManagementEndpoints
	defer func() { config.Config.ManagementEndpoints = orig }()

	fail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer fail.Close()

	ok := mockQueueMgmtServer(t, []rabbithole.QueueInfo{
		{Vhost: "v", Name: "ok", Status: "running"},
	}, "cluster-OK")
	defer ok.Close()

	config.Config.ManagementEndpoints = []config.ManagementEndpoint{
		{URL: fail.URL, Username: "u", Password: "p"},
		{URL: ok.URL, Username: "u", Password: "p", AMQP: &config.AMQPOptions{URL: "amqp://ok"}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	targets, err := getAllQueues(ctx)
	require.NoError(t, err)
	require.Len(t, targets, 1)
	assert.Equal(t, []string{"ok"}, targets[0].Attributes["rabbitmq.queue.name"])
}
