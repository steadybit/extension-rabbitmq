// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package extrabbitmq

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/extension-kit/extutil"
	"github.com/steadybit/extension-rabbitmq/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublishRabbitFixedAmountAction_Describe(t *testing.T) {
	action := publishRabbitFixedAmountAction{}
	desc := action.Describe()

	assert.Equal(t, "com.steadybit.extension_rabbitmq.queue.publish-fixed-amount", desc.Id)
	assert.Equal(t, "Publish (# of Messages)", desc.Label)
	assert.Equal(t, "Publish a fixed total number of messages to a queue, distributed evenly across the duration. For rate-based publishing (messages/second), use Publish (Messages / s) instead.", desc.Description)
	assert.NotNil(t, desc.TargetSelection)
	assert.Equal(t, "RabbitMQ", *desc.Technology)
	assert.Equal(t, "RabbitMQ", *desc.Category)
	assert.NotNil(t, desc.Status)
	assert.NotNil(t, desc.Stop)
	assert.GreaterOrEqual(t, len(desc.Parameters), 3)
}

func TestQueuePublishActions_ExchangeParameterDeprecated(t *testing.T) {
	for _, desc := range []action_kit_api.ActionDescription{
		(&publishRabbitFixedAmountAction{}).Describe(),
		(&publishRabbitPeriodicallyAction{}).Describe(),
	} {
		p := requireParam(t, desc, "exchange")
		require.NotNil(t, p.Deprecated, "%s: exchange must be marked deprecated", desc.Id)
		assert.True(t, *p.Deprecated, "%s: exchange must be marked deprecated", desc.Id)
		require.NotNil(t, p.DeprecationMessage, "%s: exchange must carry a deprecation message", desc.Id)
		assert.Contains(t, *p.DeprecationMessage, "Publish to Exchange")
	}
}

// requireParam returns the named parameter of an action description, failing the test if the
// action does not declare it.
func requireParam(t *testing.T, desc action_kit_api.ActionDescription, name string) action_kit_api.ActionParameter {
	t.Helper()
	for _, p := range desc.Parameters {
		if p.Name == name {
			return p
		}
	}
	require.FailNowf(t, "missing parameter", "%s: no %s parameter", desc.Id, name)
	return action_kit_api.ActionParameter{}
}

// The publish actions used to declare duration as an integer number of seconds, which made the
// pacing disagree with the step duration the platform derives from the same parameter. They now
// share one declaration and follow the convention used by every other action.
func TestPublishActions_DeclareDurationAsDuration(t *testing.T) {
	for _, desc := range []action_kit_api.ActionDescription{
		(&publishRabbitFixedAmountAction{}).Describe(),
		(&publishRabbitPeriodicallyAction{}).Describe(),
		(&publishExchangeFixedAmountAction{}).Describe(),
		(&publishExchangePeriodicallyAction{}).Describe(),
	} {
		p := requireParam(t, desc, "duration")
		assert.Equal(t, action_kit_api.ActionParameterTypeDuration, p.Type, "%s: duration must use the duration type", desc.Id)
	}
}

func TestGetDelayBetweenRequestsInMsFixedAmount(t *testing.T) {
	assert.Equal(t, uint64(500), getDelayBetweenRequestsInMsFixedAmount(1000, 3))
	assert.Equal(t, uint64(1000), getDelayBetweenRequestsInMsFixedAmount(1000, 1))
}

func TestPrepareRabbitFixedAmountAction_ValidateDuration(t *testing.T) {
	action := publishRabbitFixedAmountAction{}
	state := PublishMessageAttackState{}

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
	action := publishRabbitFixedAmountAction{}
	state := PublishMessageAttackState{NumberOfMessages: 10}
	req := extutil.JsonMangle(action_kit_api.PrepareActionRequestBody{
		Config:      map[string]any{"duration": 30000, "numberOfMessages": 11, "maxConcurrent": 1, "exchange": "my-exchange", "routingKey": "my-key"},
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
	// duration is in milliseconds: 11 messages over 30000ms = one message every 3000ms
	assert.Equal(t, uint64(3000), state.DelayBetweenRequestsInMS)
	assert.Equal(t, "my-exchange", state.Exchange)
	assert.Equal(t, "my-key", state.RoutingKey)
}

func TestPrepareRabbitFixedAmountAction_RejectsZeroMessages(t *testing.T) {
	action := publishRabbitFixedAmountAction{}
	state := PublishMessageAttackState{}
	req := extutil.JsonMangle(action_kit_api.PrepareActionRequestBody{
		Config:      map[string]any{"duration": 30000, "numberOfMessages": 0, "maxConcurrent": 1},
		ExecutionId: uuid.New(),
	})
	_, err := action.Prepare(context.Background(), &state, req)
	require.Error(t, err)
	assert.EqualError(t, err, "numberOfMessages must be greater than 0")
}

func TestCheckEndedPublishRabbitFixedAmount(t *testing.T) {
	exec := &ExecutionRunData{}
	exec.requestCounter.Store(10)

	state := &PublishMessageAttackState{NumberOfMessages: 10}
	assert.True(t, checkEndedPublishRabbitFixedAmount(exec, state))

	exec.requestCounter.Store(5)
	assert.False(t, checkEndedPublishRabbitFixedAmount(exec, state))
}
