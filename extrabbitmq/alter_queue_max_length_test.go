// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package extrabbitmq

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	rabbithole "github.com/michaelklishin/rabbit-hole/v3"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/extension-kit/extutil"
	"github.com/stretchr/testify/require"
)

func TestAlterQueueMaxLength_Describe(t *testing.T) {
	a := NewAlterQueueMaxLengthAttack()
	desc := a.Describe()

	require.NotEmpty(t, desc.Id)
	require.True(t, strings.HasSuffix(desc.Id, ".alter-max-length"))
	require.Equal(t, "Alter Queue Max Length (Policy)", desc.Label)
	require.Equal(t, action_kit_api.Attack, desc.Kind)
	require.NotNil(t, desc.TargetSelection)
	require.Equal(t, queueTargetId, desc.TargetSelection.TargetType)
	require.NotEmpty(t, desc.Parameters)
	// ensure parameter "maxLength" exists
	found := false
	for _, p := range desc.Parameters {
		if p.Name == "maxLength" {
			found = true
			require.Equal(t, action_kit_api.ActionParameterTypeInteger, p.Type)
			break
		}
	}
	require.True(t, found, "expected parameter 'maxLength' to be present")
}

func TestAlterQueueMaxLength_NewEmptyState(t *testing.T) {
	a := NewAlterQueueMaxLengthAttack()
	state := a.NewEmptyState()
	// zero-value checks
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
			"duration":  float64(30_000), // 30s in ms
			"maxLength": float64(123),
		},
		Target: &action_kit_api.Target{
			// these attributes are required by Prepare via MustHaveValue
			Attributes: map[string][]string{
				"rabbitmq.queue.name":  {"order"},
				"rabbitmq.queue.vhost": {"/"},
				"rabbitmq.mgmt.url":    {"https://mq-0.ns.svc:15671"},
			},
		},
	}

	before := time.Now()
	_, err := a.Prepare(context.Background(), &state, req)
	require.NoError(t, err)

	require.Equal(t, 123, state.TargetMaxLength)
	require.Equal(t, "order", state.Queue)
	require.Equal(t, "/", state.Vhost)
	require.Equal(t, "https://mq-0.ns.svc:15671", state.ManagementURL)
	require.True(t, state.Duration.After(before))
	require.True(t, state.Duration.Sub(before) >= 29*time.Second) // loose bound
}

func TestRabbitsafeRegex_EscapesRegexMeta(t *testing.T) {
	in := `order.queue+name?*(v1)[test]{a}^$|/pipe`
	out := rabbitsafeRegex(in)
	// Ensure all meta characters are escaped literally
	require.Equal(t, `order\.queue\+name\?\*\(v1\)\[test\]\{a\}\^\$\|\/pipe`, out)
	// And when we anchor outside this function, it should match literally.
	pattern := "^" + out + "$"
	require.Regexp(t, pattern, in)
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
	require.Error(t, err, "expected error when policy is not found")
}

// Helper to satisfy extutil.MustHaveValue usage in Prepare tests where needed.
func extStrPtr(s string) *string { return extutil.Ptr(s) }
