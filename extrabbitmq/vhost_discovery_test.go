// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package extrabbitmq

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/steadybit/discovery-kit/go/discovery_kit_api"
	"github.com/steadybit/extension-rabbitmq/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	rabbithole "github.com/michaelklishin/rabbit-hole/v3"
)

/*************************
 * Helpers
 *************************/

func mockVhostMgmtServer(t *testing.T, vhosts []rabbithole.VhostInfo, cluster string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/api/vhosts", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(vhosts)
	})
	mux.HandleFunc("/api/cluster-name", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(struct {
			Name string `json:"name"`
		}{Name: cluster})
	})
	// fallback to 200 OK for any other path (rabbithole may probe)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	return httptest.NewServer(mux)
}

/*************************
 * Describe / Attributes
 *************************/

func TestVhostDiscovery_Describe(t *testing.T) {
	desc := (&rabbitVhostDiscovery{}).Describe()
	require.Equal(t, vhostTargetId, desc.Id)
	require.NotNil(t, desc.Discover.CallInterval)
}

func TestVhostDiscovery_DescribeTarget(t *testing.T) {
	td := (&rabbitVhostDiscovery{}).DescribeTarget()

	require.Equal(t, vhostTargetId, td.Id)
	require.Equal(t, "RabbitMQ Vhost", td.Label.One)
	require.Equal(t, "RabbitMQ Vhosts", td.Label.Other)
	require.NotNil(t, td.Category)
	require.Equal(t, "rabbitmq", *td.Category)
	require.NotNil(t, td.Icon)

	// table columns & order
	require.Len(t, td.Table.Columns, 4)
	wantCols := []string{
		"steadybit.label",
		"rabbitmq.vhost.name",
		"rabbitmq.vhost.tracing",
		"rabbitmq.cluster.name",
	}
	for i, c := range td.Table.Columns {
		assert.Equalf(t, wantCols[i], c.Attribute, "column %d mismatch", i)
	}
	require.Len(t, td.Table.OrderBy, 1)
	require.Equal(t, "steadybit.label", td.Table.OrderBy[0].Attribute)
	require.Equal(t, discovery_kit_api.OrderByDirection("ASC"), td.Table.OrderBy[0].Direction)
}

func TestVhostDiscovery_DescribeAttributes(t *testing.T) {
	attrs := (&rabbitVhostDiscovery{}).DescribeAttributes()
	// presence and labels for all
	want := map[string]string{
		"rabbitmq.vhost.name":    "Vhost name",
		"rabbitmq.vhost.tracing": "Vhost tracing",
		"rabbitmq.cluster.name":  "Cluster name",
	}
	require.Len(t, attrs, len(want))
	for _, a := range attrs {
		lbl, ok := want[a.Attribute]
		assert.True(t, ok, "unexpected attribute %q", a.Attribute)
		assert.Equal(t, lbl, a.Label.One)
	}
}

/*************************
 * toVhostTarget mapping
 *************************/

func Test_toVhostTarget_BuildsAttributes(t *testing.T) {
	vh := rabbithole.VhostInfo{Name: "orders", Tracing: true}
	cluster := "cluster-A"
	mgmt := "http://mgmt.local:15672"

	tgt := toVhostTarget(mgmt, vh, cluster)

	assert.Equal(t, mgmt+"::orders", tgt.Id)
	assert.Equal(t, "orders", tgt.Label)
	assert.Equal(t, vhostTargetId, tgt.TargetType)

	assert.Equal(t, []string{"orders"}, tgt.Attributes["rabbitmq.vhost.name"])
	assert.Equal(t, []string{"true"}, tgt.Attributes["rabbitmq.vhost.tracing"])
	assert.Equal(t, []string{cluster}, tgt.Attributes["rabbitmq.cluster.name"])
}

/*************************
 * getAllVhosts end-to-end against mocked mgmt endpoints
 *************************/

func Test_getAllVhosts_MultipleEndpoints_WithClusters(t *testing.T) {
	// Save & restore global config
	origMgmt := config.Config.ManagementEndpoints
	origExcl := config.Config.DiscoveryAttributesExcludesVhosts
	defer func() {
		config.Config.ManagementEndpoints = origMgmt
		config.Config.DiscoveryAttributesExcludesVhosts = origExcl
	}()

	s1 := mockVhostMgmtServer(t,
		[]rabbithole.VhostInfo{
			{Name: "/", Tracing: false},
			{Name: "orders", Tracing: true},
		}, "cluster-ONE")
	defer s1.Close()

	s2 := mockVhostMgmtServer(t,
		[]rabbithole.VhostInfo{
			{Name: "billing", Tracing: false},
		}, "cluster-TWO")
	defer s2.Close()

	// Provide two management endpoints. Username/Password not used by mock but required by config shape.
	config.Config.ManagementEndpoints = []config.ManagementEndpoint{
		{URL: s1.URL, Username: "u", Password: "p"},
		{URL: s2.URL, Username: "u", Password: "p"},
	}
	config.Config.DiscoveryAttributesExcludesVhosts = nil

	targets, err := getAllVhosts(context.Background())
	require.NoError(t, err)

	// Expect 3 vhosts total
	require.Len(t, targets, 3)

	// Helper to fetch one by vhost name
	get := func(name string) discovery_kit_api.Target {
		for _, t0 := range targets {
			if vals, ok := t0.Attributes["rabbitmq.vhost.name"]; ok && len(vals) == 1 && vals[0] == name {
				return t0
			}
		}
		return discovery_kit_api.Target{}
	}

	root := get("/")
	require.NotEmpty(t, root.Id)
	assert.Equal(t, []string{"/"}, root.Attributes["rabbitmq.vhost.name"])
	assert.Equal(t, []string{"false"}, root.Attributes["rabbitmq.vhost.tracing"])

	orders := get("orders")
	require.NotEmpty(t, orders.Id)
	assert.Equal(t, []string{"orders"}, orders.Attributes["rabbitmq.vhost.name"])
	assert.Equal(t, []string{"true"}, orders.Attributes["rabbitmq.vhost.tracing"])

	billing := get("billing")
	require.NotEmpty(t, billing.Id)
	assert.Equal(t, []string{"billing"}, billing.Attributes["rabbitmq.vhost.name"])
	assert.Equal(t, []string{"false"}, billing.Attributes["rabbitmq.vhost.tracing"])
}

func Test_getAllVhosts_AppliesAttributeExcludes(t *testing.T) {
	// Save & restore
	origMgmt := config.Config.ManagementEndpoints
	origExcl := config.Config.DiscoveryAttributesExcludesVhosts
	defer func() {
		config.Config.ManagementEndpoints = origMgmt
		config.Config.DiscoveryAttributesExcludesVhosts = origExcl
	}()

	s := mockVhostMgmtServer(t,
		[]rabbithole.VhostInfo{
			{Name: "a", Tracing: true},
			{Name: "b", Tracing: false},
		}, "cX")
	defer s.Close()

	config.Config.ManagementEndpoints = []config.ManagementEndpoint{
		{URL: s.URL, Username: "u", Password: "p"},
	}

	// Exclude the tracing attribute (pattern semantics depend on ApplyAttributeExcludes;
	// use exact key to ensure removal)
	config.Config.DiscoveryAttributesExcludesVhosts = []string{"rabbitmq.vhost.tracing"}

	targets, err := getAllVhosts(context.Background())
	require.NoError(t, err)
	require.Len(t, targets, 2)

	for _, tgt := range targets {
		// Attribute should be removed
		_, has := tgt.Attributes["rabbitmq.vhost.tracing"]
		assert.False(t, has, "tracing attribute should be excluded")
		// Other attributes remain
		_, has = tgt.Attributes["rabbitmq.vhost.name"]
		assert.True(t, has)
		_, has = tgt.Attributes["rabbitmq.cluster.name"]
		assert.True(t, has)
	}
}
