// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package extrabbitmq

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/extension-kit/extutil"
	"github.com/steadybit/extension-rabbitmq/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueueBacklogCheck_Describe(t *testing.T) {
	// Given
	action := QueueBacklogCheckAction{}

	// When
	desc := action.Describe()

	// Then
	assert.Equal(t, "Check Queue Backlog", desc.Label)
	assert.Equal(t, "Check the backlog of a RabbitMQ queue (total messages in queue). Fails if backlog exceeds the threshold during the check window.", desc.Description)
	assert.Equal(t, rabbitQueueTargetId+".check-backlog", desc.Id)
	require.NotNil(t, desc.Status)
	require.NotNil(t, desc.TargetSelection)
	require.NotNil(t, desc.Parameters)
	require.GreaterOrEqual(t, len(desc.Parameters), 2)
}

func TestQueueBacklogCheck_Prepare_MissingAttributes(t *testing.T) {
	tests := []struct {
		name        string
		targetAttrs map[string][]string
		wantErrMsg  string
	}{
		{
			name:        "missing queue name",
			targetAttrs: map[string][]string{"rabbitmq.queue.vhost": {"/"}, "rabbitmq.mgmt.url": {"http://rabbit:15672"}},
			wantErrMsg:  "target missing attribute rabbitmq.queue.name",
		},
		{
			name:        "missing vhost",
			targetAttrs: map[string][]string{"rabbitmq.queue.name": {"q1"}, "rabbitmq.mgmt.url": {"http://rabbit:15672"}},
			wantErrMsg:  "target missing attribute rabbitmq.queue.vhost",
		},
		{
			name:        "missing mgmt url",
			targetAttrs: map[string][]string{"rabbitmq.queue.name": {"q1"}, "rabbitmq.queue.vhost": {"/"}},
			wantErrMsg:  "target missing attribute rabbitmq.mgmt.url",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			action := QueueBacklogCheckAction{}
			state := QueueBacklogCheckState{}
			req := extutil.JsonMangle(action_kit_api.PrepareActionRequestBody{
				Target: &action_kit_api.Target{Attributes: tc.targetAttrs},
				Config: map[string]any{
					"acceptableBacklog": 10,
					"duration":          float64(1000),
				},
				ExecutionId: uuid.New(),
			})

			_, err := action.Prepare(context.Background(), &state, req)
			require.Error(t, err)
			assert.Equal(t, tc.wantErrMsg, err.Error())
		})
	}
}

func TestQueueBacklogCheck_Prepare_SetsState(t *testing.T) {
	// Given
	action := QueueBacklogCheckAction{}
	state := QueueBacklogCheckState{}
	req := extutil.JsonMangle(action_kit_api.PrepareActionRequestBody{
		Target: &action_kit_api.Target{
			Attributes: map[string][]string{
				"rabbitmq.queue.name":  {"orders"},
				"rabbitmq.queue.vhost": {"/"},
				"rabbitmq.mgmt.url":    {"http://rabbit.local:15672"},
			},
		},
		Config: map[string]any{
			"acceptableBacklog": 5,
			"duration":          float64(1500), // milliseconds
		},
		ExecutionId: uuid.New(),
	})

	// When
	_, err := action.Prepare(context.Background(), &state, req)

	// Then
	require.NoError(t, err)
	assert.Equal(t, "orders", state.Queue)
	assert.Equal(t, "/", state.Vhost)
	assert.Equal(t, "http://rabbit.local:15672", state.MgmtURL)
	assert.Equal(t, int64(5), state.AcceptableBacklog)
	assert.False(t, state.StateCheckFailed)
	assert.WithinDuration(t, time.Now().Add(1500*time.Millisecond), state.End, 2*time.Second)
}

func TestQueueBacklogCheck_Status_NoEndpointConfigured(t *testing.T) {
	// Given
	ConfigReset := config.Config // keep old
	defer func() { config.Config = ConfigReset }()
	config.Config.ManagementEndpoints = nil

	state := &QueueBacklogCheckState{
		Vhost:             "/",
		Queue:             "orders",
		MgmtURL:           "http://unknown:15672",
		AcceptableBacklog: 10,
		End:               time.Now().Add(100 * time.Millisecond),
	}

	// When
	res, err := QueueBacklogCheckStatus(context.Background(), state)

	// Then
	require.Error(t, err)
	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "no endpoint configuration found")
}

func TestToQueueMetric_BelowThreshold_SetsFulfilledTrue(t *testing.T) {
	// Given
	now := time.Now()
	state := &QueueBacklogCheckState{
		Vhost:             "/",
		Queue:             "q1",
		AcceptableBacklog: 10,
	}

	// When
	m := toQueueMetric(7, state, now)

	// Then
	require.NotNil(t, m)
	assert.Equal(t, extutil.Ptr("rabbitmq_queue_backlog"), m.Name)
	assert.Equal(t, "true", m.Metric["backlog_constraints_fulfilled"])
	assert.Equal(t, "q1", m.Metric["queue"])
	assert.Equal(t, "/", m.Metric["vhost"])
	assert.Equal(t, "//"+state.Queue, m.Metric["id"])
	assert.Equal(t, float64(7), m.Value)
	assert.WithinDuration(t, now, m.Timestamp, time.Second)
}

func TestToQueueMetric_AboveThreshold_SetsFulfilledFalse(t *testing.T) {
	// Given
	now := time.Now()
	state := &QueueBacklogCheckState{
		Vhost:             "v1",
		Queue:             "tasks",
		AcceptableBacklog: 3,
	}

	// When
	m := toQueueMetric(9, state, now)

	// Then
	require.NotNil(t, m)
	assert.Equal(t, "false", m.Metric["backlog_constraints_fulfilled"])
	assert.Equal(t, "v1/tasks", m.Metric["id"])
	assert.Equal(t, float64(9), m.Value)
}
