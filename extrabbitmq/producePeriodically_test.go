// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package extrabbitmq

import (
	"context"
	"github.com/google/uuid"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/extension-kit/extutil"
	"github.com/steadybit/extension-rabbitmq/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestProduceRabbitPeriodicallyAction_Describe(t *testing.T) {
	action := produceRabbitPeriodicallyAction{}
	desc := action.Describe()

	assert.Equal(t, "com.steadybit.extension_rabbitmq.queue.produce-periodically", desc.Id)
	assert.Equal(t, "Produce (Messages / s)", desc.Label)
	assert.Equal(t, "Publish X messages per second for a given duration.", desc.Description)
	assert.NotNil(t, desc.TargetSelection)
	assert.Equal(t, "RabbitMQ", *desc.Technology)
	assert.Equal(t, "RabbitMQ", *desc.Category)
	assert.NotNil(t, desc.Status)
	assert.NotNil(t, desc.Stop)
	assert.GreaterOrEqual(t, len(desc.Parameters), 3)
}

func TestGetDelayBetweenRequestsInMsPeriodically(t *testing.T) {
	assert.Equal(t, uint64(1000), getDelayBetweenRequestsInMsPeriodically(1))
	assert.Equal(t, uint64(500), getDelayBetweenRequestsInMsPeriodically(2))
	assert.Equal(t, uint64(100), getDelayBetweenRequestsInMsPeriodically(10))
	assert.Equal(t, uint64(1000), getDelayBetweenRequestsInMsPeriodically(0)) // default fallback
}

func TestPrepareRabbitPeriodicallyAction_SetsDelayAndState(t *testing.T) {
	config.Config.ManagementEndpoints = make([]config.ManagementEndpoint, 0)
	config.Config.ManagementEndpoints = append(config.Config.ManagementEndpoints, config.ManagementEndpoint{URL: "http://localhost", AMQP: &config.AMQPOptions{URL: "amqp://localhost"}})

	action := produceRabbitPeriodicallyAction{}
	state := ProduceMessageAttackState{}
	req := extutil.JsonMangle(action_kit_api.PrepareActionRequestBody{
		Config: map[string]any{
			"messagesPerSecond": 5,
			"duration":          10000,
			"maxConcurrent":     1,
		},
		ExecutionId: uuid.New(),
		Target: &action_kit_api.Target{
			Attributes: map[string][]string{
				"rabbitmq.queue.name": {"test-queue"},
				"rabbitmq.amqp.url":   {"amqp://localhost"},
			},
		},
	})

	result, err := action.Prepare(context.Background(), &state, req)
	require.NoError(t, err)
	assert.Nil(t, result)
	assert.Equal(t, uint64(200), state.DelayBetweenRequestsInMS)
}

func TestStatusRabbitPeriodicallyAction_LoadsMetrics(t *testing.T) {
	action := produceRabbitPeriodicallyAction{}
	state := ProduceMessageAttackState{ExecutionID: uuid.New()}

	erd := &ExecutionRunData{metrics: make(chan action_kit_api.Metric, 2)}
	erd.metrics <- action_kit_api.Metric{Metric: map[string]string{"metric1": "true"}}
	saveExecutionRunData(state.ExecutionID, erd)

	res, err := action.Status(context.Background(), &state)
	require.NoError(t, err)
	assert.False(t, res.Completed)
	assert.NotNil(t, res.Metrics)
	assert.Len(t, *res.Metrics, 1)
}

func TestStatusRabbitPeriodicallyAction_ErrorOnMissingRunData(t *testing.T) {
	action := produceRabbitPeriodicallyAction{}
	state := ProduceMessageAttackState{ExecutionID: uuid.New()}

	res, err := action.Status(context.Background(), &state)
	assert.Nil(t, res)
	assert.Error(t, err)
}

func TestGetExecutionRunData_ReturnsSavedData(t *testing.T) {
	action := produceRabbitPeriodicallyAction{}
	id := uuid.New()
	expected := &ExecutionRunData{}
	saveExecutionRunData(id, expected)

	got, err := action.getExecutionRunData(id)
	require.NoError(t, err)
	assert.Equal(t, expected, got)
}
