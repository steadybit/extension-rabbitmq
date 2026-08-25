// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH
package extrabbitmq

import (
	"context"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/action-kit/go/action_kit_sdk"
	"github.com/steadybit/extension-kit/extbuild"
	"github.com/steadybit/extension-kit/extutil"
)

// new action: publish a fixed number of messages via the management API (rabbit-hole Publish)
type publishRabbitPeriodicallyAction struct {
	periodicPublishBehavior
}

// ensure interfaces
var (
	_ action_kit_sdk.Action[PublishMessageAttackState]           = (*publishRabbitPeriodicallyAction)(nil)
	_ action_kit_sdk.ActionWithStatus[PublishMessageAttackState] = (*publishRabbitPeriodicallyAction)(nil)
	_ action_kit_sdk.ActionWithStop[PublishMessageAttackState]   = (*publishRabbitPeriodicallyAction)(nil)
)

func NewPublishRabbitPeriodically() action_kit_sdk.Action[PublishMessageAttackState] {
	return &publishRabbitPeriodicallyAction{}
}

func (a *publishRabbitPeriodicallyAction) Describe() action_kit_api.ActionDescription {
	return action_kit_api.ActionDescription{
		Id:          "com.steadybit.extension_rabbitmq.queue.publish-periodically",
		Label:       "Publish (Messages / s)",
		Description: "Publish messages to a queue at a constant rate (messages per second) for the attack duration. For publishing a fixed total number of messages, use Publish (# of Messages) instead.",
		Version:     extbuild.GetSemverVersionStringOrUnknown(),
		Icon:        new(rabbitMQIcon),
		TargetSelection: new(action_kit_api.TargetSelection{
			TargetType: queueTargetId,
		}),
		Technology:  new("RabbitMQ"),
		Category:    new("RabbitMQ"),
		Kind:        action_kit_api.Attack,
		TimeControl: action_kit_api.TimeControlExternal,
		Parameters: []action_kit_api.ActionParameter{
			exchange,
			routingKey,
			headers,
			body,
			{
				Name:         "messagesPerSecond",
				Label:        "Messages per second",
				Description:  new("The number of messages per second. Should be between 1 and 10."),
				Type:         action_kit_api.ActionParameterTypeInteger,
				DefaultValue: new("1"),
				MinValue:     new(1),
				MaxValue:     new(10),
				Required:     new(true),
			},
			durationPublishPeriodically,
			successRate,
			maxConcurrent,
		},
		Status: new(action_kit_api.MutatingEndpointReferenceWithCallInterval{
			CallInterval: new("1s"),
		}),
		Stop: new(action_kit_api.MutatingEndpointReference{}),
	}
}

func getDelayBetweenRequestsInMsPeriodically(recordsPerSecond int64) uint64 {
	if recordsPerSecond > 0 {
		return uint64(1000 / recordsPerSecond)
	} else {
		return 1000 / 1
	}
}

// Prepare validates request and sets up state. It defers to shared prepare helpers where available.
func (a *publishRabbitPeriodicallyAction) Prepare(ctx context.Context, state *PublishMessageAttackState, request action_kit_api.PrepareActionRequestBody) (*action_kit_api.PrepareResult, error) {
	state.DelayBetweenRequestsInMS = getDelayBetweenRequestsInMsPeriodically(extutil.ToInt64(request.Config["messagesPerSecond"]))
	return prepare(request, state, func(executionRunData *ExecutionRunData, state *PublishMessageAttackState) bool { return false })
}
