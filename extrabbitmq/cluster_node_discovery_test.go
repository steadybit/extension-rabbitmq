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

/*********************
 * Test helpers
 *********************/

func mockNodeMgmtServer(t *testing.T, nodes []rabbithole.NodeInfo, cluster string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/api/nodes", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(nodes)
	})
	mux.HandleFunc("/api/cluster-name", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(struct {
			Name string `json:"name"`
		}{Name: cluster})
	})
	// default handler for anything else rabbithole might ping
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	return httptest.NewServer(mux)
}

/*********************
 * Describe / Target / Attributes
 *********************/

func Test_Node_DiscoveryDescribe(t *testing.T) {
	desc := (&rabbitNodeDiscovery{}).Describe()
	require.Equal(t, nodeTargetId, desc.Id)
	require.NotNil(t, desc.Discover.CallInterval)
}

func Test_Node_DescribeTarget_Table(t *testing.T) {
	td := (&rabbitNodeDiscovery{}).DescribeTarget()

	require.Equal(t, nodeTargetId, td.Id)
	require.Equal(t, "RabbitMQ node", td.Label.One)
	require.Equal(t, "RabbitMQ nodes", td.Label.Other)
	require.NotNil(t, td.Category)
	require.Equal(t, "rabbitmq", *td.Category)

	// Columns
	wantCols := []string{
		"steadybit.label",
		"rabbitmq.node.name",
		"rabbitmq.node.type",
		"rabbitmq.node.running",
		"rabbitmq.cluster.name",
		"rabbitmq.mgmt.url",
	}
	require.Len(t, td.Table.Columns, len(wantCols))
	for i, c := range td.Table.Columns {
		assert.Equalf(t, wantCols[i], c.Attribute, "column %d mismatch", i)
	}

	// OrderBy
	require.Len(t, td.Table.OrderBy, 1)
	require.Equal(t, "steadybit.label", td.Table.OrderBy[0].Attribute)
	require.Equal(t, discovery_kit_api.OrderByDirection("ASC"), td.Table.OrderBy[0].Direction)
}

func Test_Node_DescribeAttributes_AllPresent(t *testing.T) {
	attrs := (&rabbitNodeDiscovery{}).DescribeAttributes()
	want := map[string]struct{}{
		"rabbitmq.node.name":    {},
		"rabbitmq.node.type":    {},
		"rabbitmq.node.running": {},
		"rabbitmq.cluster.name": {},
	}
	require.Len(t, attrs, len(want))
	for _, a := range attrs {
		_, ok := want[a.Attribute]
		assert.True(t, ok, "unexpected attribute %q", a.Attribute)
		delete(want, a.Attribute)
	}
	require.Empty(t, want, "missing attributes %v", want)
}

/*********************
 * toNodeTarget mapping
 *********************/

func Test_toNodeTarget_AttributesAndId(t *testing.T) {
	n := rabbithole.NodeInfo{
		Name:      "rabbit@node-1",
		NodeType:  "disc",
		IsRunning: true,
	}
	cluster := "cluster-1"
	mgmtURL := "http://rabbit.example:15672"

	tgt := toNodeTarget(mgmtURL, n, cluster)

	assert.Equal(t, mgmtURL+"::"+n.Name, tgt.Id)
	assert.Equal(t, "rabbit@node-1", tgt.Label)
	assert.Equal(t, nodeTargetId, tgt.TargetType)

	assert.Equal(t, []string{"rabbit@node-1"}, tgt.Attributes["rabbitmq.node.name"])
	assert.Equal(t, []string{"disc"}, tgt.Attributes["rabbitmq.node.type"])
	assert.Equal(t, []string{"true"}, tgt.Attributes["rabbitmq.node.running"])
	assert.Equal(t, []string{mgmtURL}, tgt.Attributes["rabbitmq.mgmt.url"])
}

/*********************
 * getAllNodes via mocked management endpoints
 *********************/

func Test_getAllNodes_MultipleEndpoints_WithClusters(t *testing.T) {
	// Save & restore global config
	origMgmt := config.Config.ManagementEndpoints
	defer func() { config.Config.ManagementEndpoints = origMgmt }()

	s1 := mockNodeMgmtServer(t,
		[]rabbithole.NodeInfo{
			{Name: "rabbit@a-1", NodeType: "disc", IsRunning: true},
			{Name: "rabbit@a-2", NodeType: "ram", IsRunning: false},
		},
		"cluster-A",
	)
	defer s1.Close()

	s2 := mockNodeMgmtServer(t,
		[]rabbithole.NodeInfo{
			{Name: "rabbit@b-1", NodeType: "disc", IsRunning: true},
		},
		"cluster-B",
	)
	defer s2.Close()

	config.Config.ManagementEndpoints = []config.ManagementEndpoint{
		{URL: s1.URL, Username: "u", Password: "p"},
		{URL: s2.URL, Username: "u", Password: "p"},
	}

	targets, err := getAllNodes(context.Background())
	require.NoError(t, err)
	require.Len(t, targets, 3)

	// helper to find by node name
	find := func(name string) discovery_kit_api.Target {
		for _, t0 := range targets {
			if v, ok := t0.Attributes["rabbitmq.node.name"]; ok && len(v) == 1 && v[0] == name {
				return t0
			}
		}
		return discovery_kit_api.Target{}
	}

	a1 := find("rabbit@a-1")
	require.NotEmpty(t, a1.Id)
	assert.Equal(t, []string{"disc"}, a1.Attributes["rabbitmq.node.type"])
	assert.Equal(t, []string{"true"}, a1.Attributes["rabbitmq.node.running"])
	assert.Len(t, a1.Attributes["rabbitmq.mgmt.url"], 1)

	a2 := find("rabbit@a-2")
	require.NotEmpty(t, a2.Id)
	assert.Equal(t, []string{"ram"}, a2.Attributes["rabbitmq.node.type"])
	assert.Equal(t, []string{"false"}, a2.Attributes["rabbitmq.node.running"])

	b1 := find("rabbit@b-1")
	require.NotEmpty(t, b1.Id)
	assert.Equal(t, []string{"disc"}, b1.Attributes["rabbitmq.node.type"])
	assert.Equal(t, []string{"true"}, b1.Attributes["rabbitmq.node.running"])
}

func Test_getAllNodes_PartialFailure_StillReturnsFromHealthy(t *testing.T) {
	origMgmt := config.Config.ManagementEndpoints
	defer func() { config.Config.ManagementEndpoints = origMgmt }()

	// failing endpoint
	fail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "oops", http.StatusInternalServerError)
	}))
	defer fail.Close()

	// healthy endpoint
	okSrv := mockNodeMgmtServer(t,
		[]rabbithole.NodeInfo{
			{Name: "rabbit@ok-1", NodeType: "disc", IsRunning: true},
		},
		"cluster-OK",
	)
	defer okSrv.Close()

	config.Config.ManagementEndpoints = []config.ManagementEndpoint{
		{URL: fail.URL, Username: "u", Password: "p"},
		{URL: okSrv.URL, Username: "u", Password: "p"},
	}

	targets, err := getAllNodes(context.Background())
	// Function logs warnings but should not fail entirely.
	require.NoError(t, err)
	require.Len(t, targets, 1)

	tgt := targets[0]
	assert.Equal(t, []string{"rabbit@ok-1"}, tgt.Attributes["rabbitmq.node.name"])
}
