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

func TestProduceRabbitFixedAmountAction_Describe(t *testing.T) {
	action := produceRabbitFixedAmountAction{}
	desc := action.Describe()

	assert.Equal(t, "com.steadybit.extension_rabbitmq.queue.produce-fixed-amount", desc.Id)
	assert.Equal(t, "Produce (# of Messages)", desc.Label)
	assert.Equal(t, "Publish a fixed number of messages.", desc.Description)
	assert.NotNil(t, desc.TargetSelection)
	assert.Equal(t, "RabbitMQ", *desc.Technology)
	assert.Equal(t, "RabbitMQ", *desc.Category)
	assert.NotNil(t, desc.Status)
	assert.NotNil(t, desc.Stop)
	assert.GreaterOrEqual(t, len(desc.Parameters), 3)
}

func TestGetDelayBetweenRequestsInMsFixedAmount(t *testing.T) {
	assert.Equal(t, uint64(500), getDelayBetweenRequestsInMsFixedAmount(1000, 3))
	assert.Equal(t, uint64(1000), getDelayBetweenRequestsInMsFixedAmount(1000, 1))
}

func TestPrepareRabbitFixedAmountAction_ValidateDuration(t *testing.T) {
	action := produceRabbitFixedAmountAction{}
	state := ProduceMessageAttackState{}

	req := extutil.JsonMangle(action_kit_api.PrepareActionRequestBody{
		Config:      map[string]any{"duration": 0},
		ExecutionId: uuid.New(),
	})
	_, err := action.Prepare(context.Background(), &state, req)
	require.Error(t, err)
	assert.EqualError(t, err, "duration must be greater than 0")
}

func TestPrepareRabbitFixedAmountAction_SetsDelayAndState(t *testing.T) {
	config.Config.ManagementEndpoints = make([]config.ManagementEndpoint, 0)
	config.Config.ManagementEndpoints = append(config.Config.ManagementEndpoints, config.ManagementEndpoint{URL: "http://test", AMQP: &config.AMQPOptions{URL: "http://test"}})
	action := produceRabbitFixedAmountAction{}
	state := ProduceMessageAttackState{NumberOfMessages: 10}
	req := extutil.JsonMangle(action_kit_api.PrepareActionRequestBody{
		Config:      map[string]any{"duration": 10000, "numberOfMessages": 10, "maxConcurrent": 1},
		ExecutionId: uuid.New(),
		Target: &action_kit_api.Target{
			Attributes: map[string][]string{
				"rabbitmq.queue.name": {"test"},
				"rabbitmq.amqp.url":   {"http://test"},
			},
		},
	})
	result, err := action.Prepare(context.Background(), &state, req)
	assert.Nil(t, result)
	assert.NoError(t, err)
	assert.Greater(t, state.DelayBetweenRequestsInMS, uint64(0))
}

func TestCheckEndedProduceRabbitFixedAmount(t *testing.T) {
	exec := &ExecutionRunData{}
	exec.requestCounter.Store(10)

	state := &ProduceMessageAttackState{NumberOfMessages: 10}
	assert.True(t, checkEndedProduceRabbitFixedAmount(exec, state))

	exec.requestCounter.Store(5)
	assert.False(t, checkEndedProduceRabbitFixedAmount(exec, state))
}
