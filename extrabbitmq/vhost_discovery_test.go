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

func TestGetAllVhosts(t *testing.T) {
	t.Parallel()

	// Server 1: returns two vhosts and a cluster name
	s1 := httptest.NewServer(mockRabbitMgmtHandler(
		[]rabbithole.VhostInfo{{Name: "vhost1", Tracing: false}, {Name: "vhost2", Tracing: true}},
		"cluster-1",
		http.StatusOK,
	))
	defer s1.Close()

	// Server 2: returns one vhost and a different cluster name
	s2 := httptest.NewServer(mockRabbitMgmtHandler(
		[]rabbithole.VhostInfo{{Name: "orders", Tracing: false}},
		"cluster-2",
		http.StatusOK,
	))
	defer s2.Close()

	// Server 3: returns 500 on /api/vhosts (to exercise error path)
	s3 := httptest.NewServer(mockRabbitMgmtHandler(
		nil,
		"cluster-err",
		http.StatusInternalServerError,
	))
	defer s3.Close()

	// Configure endpoints and initialize pooled management clients once
	config.Config.ManagementEndpoints = []config.ManagementEndpoint{
		{URL: s1.URL},
		{URL: s2.URL},
		{URL: s3.URL},
	}
	// also keep JSON in sync if other code paths read it
	setEndpointsJSON(config.Config.ManagementEndpoints)

	// Initialize pool (builds one rabbithole client per endpoint)
	err := clients.Init()
	require.NoError(t, err)

	// Call discovery
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	targets, derr := getAllVhosts(ctx)
	require.NoError(t, derr)

	// Expect: vhosts from s1 (2) + s2 (1). s3 contributes none due to 500.
	assert.Len(t, targets, 3)

	// Validate attributes on all targets
	for _, tgt := range targets {
		assert.Equal(t, vhostTargetId, tgt.TargetType)
		assert.NotEmpty(t, tgt.Label)
		assert.NotEmpty(t, tgt.Attributes["rabbitmq.vhost.name"])
		assert.NotEmpty(t, tgt.Attributes["rabbitmq.vhost.tracing"])
		// cluster name is filled via /api/overview or /api/cluster-name
		assert.NotEmpty(t, tgt.Attributes["rabbitmq.cluster.name"])
	}
}

// --- test helpers ---

// mockRabbitMgmtHandler simulates enough of the RabbitMQ Management API
// for ListVhosts() and GetClusterName().
func mockRabbitMgmtHandler(vhosts []rabbithole.VhostInfo, clusterName string, vhostsStatus int) http.Handler {
	type clusterNameDoc struct {
		Name string `json:"name"`
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/vhosts":
			if vhostsStatus != http.StatusOK {
				http.Error(w, "error", vhostsStatus)
				return
			}
			_ = json.NewEncoder(w).Encode(vhosts)
			return
		case "/api/cluster-name", "/api/overview":
			// rabbits-hole may call /api/cluster-name; some versions use Overview for derived info.
			_ = json.NewEncoder(w).Encode(clusterNameDoc{Name: clusterName})
			return
		default:
			http.NotFound(w, r)
			return
		}
	})
}
