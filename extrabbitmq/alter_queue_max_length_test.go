// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package extrabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	rabbithole "github.com/michaelklishin/rabbit-hole/v3"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/extension-rabbitmq/config"
	"github.com/stretchr/testify/require"
)

func TestAlterQueueMaxLength_Describe_Full(t *testing.T) {
	a := NewAlterQueueMaxLengthAttack()
	desc := a.Describe()

	require.NotEmpty(t, desc.Id)
	require.True(t, strings.HasSuffix(desc.Id, ".alter-max-length"))
	require.Equal(t, "Alter Queue Max Length", desc.Label)
	require.Equal(t, action_kit_api.Attack, desc.Kind)

	require.NotNil(t, desc.TargetSelection)
	require.Equal(t, queueTargetId, desc.TargetSelection.TargetType)
	if desc.TargetSelection.QuantityRestriction == nil {
		t.Fatalf("QuantityRestriction must be set to ExactlyOne")
	}
	require.Equal(t, action_kit_api.QuantityRestrictionExactlyOne, *desc.TargetSelection.QuantityRestriction)

	hasMaxLen := false
	for _, p := range desc.Parameters {
		if p.Name == "maxLength" {
			hasMaxLen = true
			require.Equal(t, action_kit_api.ActionParameterTypeInteger, p.Type)
		}
	}
	require.True(t, hasMaxLen, "expected parameter 'maxLength'")
}

func TestAlterQueueMaxLength_NewEmptyState_ZeroValues(t *testing.T) {
	a := NewAlterQueueMaxLengthAttack()
	state := a.NewEmptyState()
	require.Empty(t, state.PolicyName)
	require.Empty(t, state.Queue)
	require.Empty(t, state.Vhost)
	require.Empty(t, state.ManagementURL)
	require.Zero(t, state.TargetMaxLength)
	require.True(t, state.Duration.IsZero())
}

func TestAlterQueueMaxLength_Prepare_SetsState(t *testing.T) {
	a := NewAlterQueueMaxLengthAttack()
	state := a.NewEmptyState()

	req := action_kit_api.PrepareActionRequestBody{
		Config: map[string]interface{}{
			"duration":  float64(30_000),
			"maxLength": float64(123),
		},
		Target: &action_kit_api.Target{
			Attributes: map[string][]string{
				"rabbitmq.queue.name":  {"order"},
				"rabbitmq.queue.vhost": {"order-vhost"},
				"rabbitmq.mgmt.url":    {"http://example:15672"},
			},
		},
	}

	start := time.Now()
	_, err := a.Prepare(context.Background(), &state, req)
	require.NoError(t, err)

	require.Equal(t, 123, state.TargetMaxLength)
	require.Equal(t, "order", state.Queue)
	require.Equal(t, "order-vhost", state.Vhost)
	require.Equal(t, "http://example:15672", state.ManagementURL)
	require.True(t, state.Duration.After(start))
	require.True(t, state.Duration.Sub(start) >= 29*time.Second)
}

func TestAlterQueueMaxLength_Prepare_MissingAttrPanics(t *testing.T) {
	a := NewAlterQueueMaxLengthAttack()
	state := a.NewEmptyState()
	req := action_kit_api.PrepareActionRequestBody{
		Config: map[string]interface{}{
			"duration":  float64(10_000),
			"maxLength": float64(1),
		},
		Target: &action_kit_api.Target{Attributes: map[string][]string{
			// missing queue.vhost and mgmt.url on purpose
			"rabbitmq.queue.name": {"q"},
		}},
	}
	require.Panics(t, func() {
		_, _ = a.Prepare(context.Background(), &state, req)
	})
}

func TestStart_CreatesPolicy_Success(t *testing.T) {
	// HTTP fake policy API
	var putCalled int32
	var capturedPutPath string
	var capturedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/policies/order":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/policies/"):
			atomic.AddInt32(&putCalled, 1)
			capturedPutPath = r.URL.Path
			defer r.Body.Close()
			_ = json.NewDecoder(r.Body).Decode(&capturedBody)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// Wire config so Start() can obtain a management client
	config.Config.ManagementEndpoints = []config.ManagementEndpoint{
		{URL: srv.URL, Username: "u", Password: "p"},
	}

	// Prepare state
	a := NewAlterQueueMaxLengthAttack()
	state := a.NewEmptyState()
	state.Queue = "order.queue+v1"
	state.Vhost = "order"
	state.ManagementURL = srv.URL
	state.TargetMaxLength = 777

	// Act
	res, err := a.Start(context.Background(), &state)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotEmpty(t, state.PolicyName) // should be generated

	// Verify PUT request
	require.Equal(t, int32(1), atomic.LoadInt32(&putCalled))
	require.Regexp(t, regexp.MustCompile(`/api/policies/order/steadybit-alter-maxlen-order\.queue\+v1-`), capturedPutPath)

	// Body assertions
	require.Equal(t, "queues", capturedBody["apply-to"])
	require.EqualValues(t, 1, capturedBody["priority"])

	def, ok := capturedBody["definition"].(map[string]any)
	require.True(t, ok, "definition must be an object")
	require.EqualValues(t, 777, def["max-length"])
}

func TestStart_Error_NoEndpointConfigured(t *testing.T) {
	config.Config.ManagementEndpoints = nil // ensure not found
	a := NewAlterQueueMaxLengthAttack()
	state := a.NewEmptyState()
	state.Queue = "q"
	state.Vhost = "v"
	state.ManagementURL = "http://not-in-config"
	state.TargetMaxLength = 1

	_, err := a.Start(context.Background(), &state)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no endpoint configuration found for amqp url: http://not-in-config")
}

func TestStart_Error_PutPolicyFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/policies/v" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
			return
		}
		if r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/policies/") {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	config.Config.ManagementEndpoints = []config.ManagementEndpoint{
		{URL: srv.URL, Username: "u", Password: "p"},
	}

	a := NewAlterQueueMaxLengthAttack()
	state := a.NewEmptyState()
	state.Queue = "q"
	state.Vhost = "v"
	state.ManagementURL = srv.URL
	state.TargetMaxLength = 5

	_, err := a.Start(context.Background(), &state)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to create policy for queue")
}

func TestStart_PriorityBumpedWhenExistingPolicies(t *testing.T) {
	// arrange server to return existing policies with a matching one at priority 7
	var putBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/policies/order":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			// one matching queues policy with priority 7 and a non-queues policy we should ignore
			_, _ = w.Write([]byte(`[
                {"name":"p1","pattern":"^order\\.queue$","apply-to":"queues","priority":7,"definition":{}},
                {"name":"ex","pattern":".*","apply-to":"exchanges","priority":99,"definition":{}}
              ]`))
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/policies/"):
			defer r.Body.Close()
			_ = json.NewDecoder(r.Body).Decode(&putBody)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// configure endpoint
	config.Config.ManagementEndpoints = []config.ManagementEndpoint{{URL: srv.URL, Username: "u", Password: "p"}}

	a := NewAlterQueueMaxLengthAttack()
	state := a.NewEmptyState()
	state.Queue = "order.queue"
	state.Vhost = "order"
	state.ManagementURL = srv.URL
	state.TargetMaxLength = 10

	// act
	_, err := a.Start(context.Background(), &state)
	require.NoError(t, err)

	// assert priority bumped to 8
	require.EqualValues(t, 8, putBody["priority"])
}

func TestSafeDeletePolicy_Success(t *testing.T) {
	vhost := "order"
	name := "steadybit-test-policy"
	wantPath := "/api/policies/" + vhost + "/" + name

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != wantPath {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c, err := rabbithole.NewClient(srv.URL, "user", "pass")
	require.NoError(t, err)

	err = safeDeletePolicy(c, vhost, name)
	require.NoError(t, err)
}

func TestSafeDeletePolicy_NotFound(t *testing.T) {
	vhost := "order"
	name := "missing-policy"
	wantPath := "/api/policies/" + vhost + "/" + name

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != wantPath {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	c, err := rabbithole.NewClient(srv.URL, "user", "pass")
	require.NoError(t, err)

	err = safeDeletePolicy(c, vhost, name)
	require.Error(t, err)
	require.Contains(t, err.Error(), fmt.Sprintf("policy %s not found in vhost %s", name, vhost))
}
