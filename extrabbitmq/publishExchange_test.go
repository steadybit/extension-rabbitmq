// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2026 Steadybit GmbH

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

func TestPublishExchangeFixedAmountAction_Describe(t *testing.T) {
	desc := (&publishExchangeFixedAmountAction{}).Describe()
	assert.Equal(t, "com.steadybit.extension_rabbitmq.exchange.publish-fixed-amount", desc.Id)
	assert.Equal(t, "Publish to Exchange (# of Messages)", desc.Label)
	require.NotNil(t, desc.TargetSelection)
	assert.Equal(t, exchangeTargetId, desc.TargetSelection.TargetType)
	for _, p := range desc.Parameters {
		assert.NotEqual(t, "exchange", p.Name, "the exchange comes from the target, not a parameter")
	}
}

func TestPublishExchangePeriodicallyAction_Describe(t *testing.T) {
	desc := (&publishExchangePeriodicallyAction{}).Describe()
	assert.Equal(t, "com.steadybit.extension_rabbitmq.exchange.publish-periodically", desc.Id)
	assert.Equal(t, "Publish to Exchange (Messages / s)", desc.Label)
	require.NotNil(t, desc.TargetSelection)
	assert.Equal(t, exchangeTargetId, desc.TargetSelection.TargetType)
	for _, p := range desc.Parameters {
		assert.NotEqual(t, "exchange", p.Name, "the exchange comes from the target, not a parameter")
	}
}

func TestPrepareExchangeFixedAmount_SetsState(t *testing.T) {
	config.Config.ManagementEndpoints = []config.ManagementEndpoint{
		{URL: "http://test", AMQP: &config.AMQPOptions{URL: "http://test"}},
	}
	action := publishExchangeFixedAmountAction{}
	state := PublishMessageAttackState{}
	req := extutil.JsonMangle(action_kit_api.PrepareActionRequestBody{
		Config:      map[string]any{"duration": 10000, "numberOfMessages": 10, "maxConcurrent": 1, "routingKey": "orders.created"},
		ExecutionId: uuid.New(),
		Target: &action_kit_api.Target{
			Attributes: map[string][]string{
				"rabbitmq.exchange.name":  {"demo.topic"},
				"rabbitmq.exchange.vhost": {"orders"},
				"rabbitmq.amqp.url":       {"http://test"},
			},
		},
	})
	result, err := action.Prepare(context.Background(), &state, req)
	assert.Nil(t, result)
	require.NoError(t, err)
	assert.Equal(t, "demo.topic", state.Exchange)
	assert.Equal(t, "orders", state.Vhost)
	assert.Equal(t, "orders.created", state.RoutingKey)
	assert.Empty(t, state.Queue)
}

func TestPrepareExchangeFixedAmount_MissingExchangeAttribute(t *testing.T) {
	action := publishExchangeFixedAmountAction{}
	state := PublishMessageAttackState{}
	req := extutil.JsonMangle(action_kit_api.PrepareActionRequestBody{
		Config:      map[string]any{"duration": 10000, "numberOfMessages": 10, "maxConcurrent": 1},
		ExecutionId: uuid.New(),
		Target:      &action_kit_api.Target{Attributes: map[string][]string{}},
	})
	_, err := action.Prepare(context.Background(), &state, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rabbitmq.exchange.name")
}

func Test_createPublishRequest_noRoutingKeyFallbackForExchangeTargets(t *testing.T) {
	// Exchange targets have no queue: an empty routing key must be published as-is,
	// not replaced by a fallback.
	state := &PublishMessageAttackState{Exchange: "demo.topic", RoutingKey: "", Queue: "", Body: "x"}
	ex, rk, _ := createPublishRequest(state)
	assert.Equal(t, "demo.topic", ex)
	assert.Equal(t, "", rk)
}

func Test_guardExchangeTargetCount_limitsTargetsPerExecution(t *testing.T) {
	expKey := "TEST-GUARD-1"
	execId := 90001
	newReq := func() action_kit_api.PrepareActionRequestBody {
		return action_kit_api.PrepareActionRequestBody{
			ExecutionContext: &action_kit_api.ExecutionContext{ExperimentKey: &expKey, ExecutionId: &execId},
		}
	}
	for i := 0; i < maxQueueTargetsWithExchange; i++ {
		require.NoErrorf(t, guardExchangeTargetCount(newReq()), "target %d must be allowed", i+1)
	}
	err := guardExchangeTargetCount(newReq())
	require.Error(t, err, "target %d must be rejected", maxQueueTargetsWithExchange+1)
	assert.Contains(t, err.Error(), "restrict the target selection")

	// a different execution has its own counter
	otherExec := 90002
	otherReq := action_kit_api.PrepareActionRequestBody{
		ExecutionContext: &action_kit_api.ExecutionContext{ExperimentKey: &expKey, ExecutionId: &otherExec},
	}
	require.NoError(t, guardExchangeTargetCount(otherReq))
}

func Test_guardExchangeTargetCount_skipsWithoutExecutionContext(t *testing.T) {
	for i := 0; i < maxQueueTargetsWithExchange+5; i++ {
		require.NoError(t, guardExchangeTargetCount(action_kit_api.PrepareActionRequestBody{}))
	}
}

func TestPrepareRabbitFixedAmount_FailsOn11thTargetWithExchange(t *testing.T) {
	config.Config.ManagementEndpoints = []config.ManagementEndpoint{
		{URL: "http://test", AMQP: &config.AMQPOptions{URL: "http://test"}},
	}
	action := publishRabbitFixedAmountAction{}
	expKey := "TEST-GUARD-2"
	execId := 90100

	newReq := func() action_kit_api.PrepareActionRequestBody {
		return extutil.JsonMangle(action_kit_api.PrepareActionRequestBody{
			Config:      map[string]any{"duration": 10000, "numberOfMessages": 10, "maxConcurrent": 1, "exchange": "demo.topic"},
			ExecutionId: uuid.New(),
			ExecutionContext: &action_kit_api.ExecutionContext{
				ExperimentKey: &expKey,
				ExecutionId:   &execId,
			},
			Target: &action_kit_api.Target{
				Attributes: map[string][]string{
					"rabbitmq.queue.name": {"q"},
					"rabbitmq.amqp.url":   {"http://test"},
				},
			},
		})
	}

	for i := 0; i < maxQueueTargetsWithExchange; i++ {
		state := PublishMessageAttackState{}
		_, err := action.Prepare(context.Background(), &state, newReq())
		require.NoErrorf(t, err, "target %d must prepare", i+1)
	}

	state := PublishMessageAttackState{}
	_, err := action.Prepare(context.Background(), &state, newReq())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "restrict the target selection")
}
