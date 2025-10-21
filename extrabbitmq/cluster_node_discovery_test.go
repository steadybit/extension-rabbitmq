// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

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

func TestGetAllNodes_MultipleEndpoints_WithError(t *testing.T) {
	// Do not use t.Parallel() here because clients.Init() only runs once.

	ok := httptest.NewServer(mockMgmtNodesHandler(
		[]rabbithole.NodeInfo{{Name: "rabbit@ok-0", NodeType: "disc", IsRunning: true}},
		"cluster-OK",
		http.StatusOK,
	))
	defer ok.Close()

	fail := httptest.NewServer(mockMgmtNodesHandler(
		nil,
		"cluster-FAIL",
		http.StatusInternalServerError,
	))
	defer fail.Close()

	// Initialize pool once with both endpoints
	config.Config.ManagementEndpoints = []config.ManagementEndpoint{
		{URL: ok.URL},
		{URL: fail.URL},
	}
	setEndpointsJSON(config.Config.ManagementEndpoints)
	require.NoError(t, clients.Init())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	targets, err := getAllNodes(ctx)
	require.NoError(t, err)
	require.Len(t, targets, 1)

	tgt := targets[0]
	assert.Equal(t, "rabbit@ok-0", tgt.Label)
	assert.NotEmpty(t, tgt.Attributes["rabbitmq.cluster.name"])
	assert.Equal(t, []string{"true"}, tgt.Attributes["rabbitmq.node.running"])
}

func TestToNodeTarget_Attributes(t *testing.T) {
	n := rabbithole.NodeInfo{Name: "rabbit@x", NodeType: "disc", IsRunning: true}
	tgt := toNodeTarget("http://mgmt", n, "cname")
	assert.Equal(t, nodeTargetId, tgt.TargetType)
	assert.Equal(t, "rabbit@x", tgt.Label)
	assert.Equal(t, "http://mgmt::rabbit@x", tgt.Id)
	assert.Equal(t, []string{"rabbit@x"}, tgt.Attributes["rabbitmq.node.name"])
	assert.Equal(t, []string{"disc"}, tgt.Attributes["rabbitmq.node.type"])
	assert.Equal(t, []string{"true"}, tgt.Attributes["rabbitmq.node.running"])
}

// --- test helpers ---

// mockMgmtNodesHandler simulates RabbitMQ Management API endpoints used by getAllNodes():
//   - GET /api/nodes
//   - GET /api/cluster-name (some libs may read overview instead; we serve a compatible body for both)
func mockMgmtNodesHandler(nodes []rabbithole.NodeInfo, clusterName string, nodesStatus int) http.Handler {
	type clusterNameDoc struct {
		Name string `json:"name"`
	}
	type overviewDoc struct {
		ClusterName string `json:"cluster_name"`
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/nodes":
			if nodesStatus != http.StatusOK {
				http.Error(w, "error", nodesStatus)
				return
			}
			_ = json.NewEncoder(w).Encode(nodes)
			return
		case "/api/cluster-name":
			_ = json.NewEncoder(w).Encode(clusterNameDoc{Name: clusterName})
			return
		case "/api/overview":
			_ = json.NewEncoder(w).Encode(overviewDoc{ClusterName: clusterName})
			return
		default:
			http.NotFound(w, r)
		}
	})
}
