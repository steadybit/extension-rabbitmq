// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH
package extrabbitmq

import (
	"context"
	"errors"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/action-kit/go/action_kit_sdk"
	"github.com/steadybit/extension-kit/extbuild"
	"github.com/steadybit/extension-kit/extutil"
)

type publishRabbitFixedAmountAction struct {
	fixedAmountPublishBehavior
}

// ensure interfaces
var (
	_ action_kit_sdk.Action[PublishMessageAttackState]           = (*publishRabbitFixedAmountAction)(nil)
	_ action_kit_sdk.ActionWithStatus[PublishMessageAttackState] = (*publishRabbitFixedAmountAction)(nil)
	_ action_kit_sdk.ActionWithStop[PublishMessageAttackState]   = (*publishRabbitFixedAmountAction)(nil)
)

func NewPublishRabbitFixedAmount() action_kit_sdk.Action[PublishMessageAttackState] {
	return &publishRabbitFixedAmountAction{}
}

func (a *publishRabbitFixedAmountAction) Describe() action_kit_api.ActionDescription {
	return action_kit_api.ActionDescription{
		Id:          "com.steadybit.extension_rabbitmq.queue.publish-fixed-amount",
		Label:       "Publish (# of Messages)",
		Description: "Publish a fixed total number of messages to a queue, distributed evenly across the duration. For rate-based publishing (messages/second), use Publish (Messages / s) instead.",
		Version:     extbuild.GetSemverVersionStringOrUnknown(),
		Icon:        new(rabbitMQIcon),
		TargetSelection: new(action_kit_api.TargetSelection{
			TargetType: queueTargetId,
		}),
		Technology:  new("RabbitMQ"),
		Category:    new("RabbitMQ"),
		Kind:        action_kit_api.Attack,
		TimeControl: action_kit_api.TimeControlInternal,
		Parameters: []action_kit_api.ActionParameter{
			exchange,
			routingKey,
			headers,
			body,
			{
				Name:         "numberOfMessages",
				Label:        "Number of Messages",
				Description:  new("Total number of messages to publish across the entire duration. The publishing rate is this value divided by the duration."),
				Type:         action_kit_api.ActionParameterTypeInteger,
				Required:     new(true),
				DefaultValue: new("1"),
				MinValue:     new(1),
			},
			{
				Name:         "duration",
				Label:        "Duration (seconds)",
				Description:  new("How long the publisher runs, in seconds. The total number of messages is distributed evenly across this duration."),
				Type:         action_kit_api.ActionParameterTypeInteger,
				Required:     new(true),
				DefaultValue: new("30"),
			},
			maxConcurrent,
		},
		Status: new(action_kit_api.MutatingEndpointReferenceWithCallInterval{
			CallInterval: new("1s"),
		}),
		Stop: new(action_kit_api.MutatingEndpointReference{}),
	}
}

func getDelayBetweenRequestsInMsFixedAmount(duration uint64, numberOfRequests uint64) uint64 {
	actualRequests := numberOfRequests - 1
	if actualRequests > 0 {
		return duration / actualRequests
	} else {
		return 1000 / 1
	}
}

// Prepare validates request and sets up state. It defers to shared prepare helpers where available.
func (a *publishRabbitFixedAmountAction) Prepare(ctx context.Context, state *PublishMessageAttackState, request action_kit_api.PrepareActionRequestBody) (*action_kit_api.PrepareResult, error) {
	state.NumberOfMessages = extutil.ToUInt64(request.Config["numberOfMessages"])

	if extutil.ToInt64(request.Config["duration"]) == 0 {
		return nil, errors.New("duration must be greater than 0")
	}
	if state.NumberOfMessages == 0 {
		return nil, errors.New("numberOfMessages must be greater than 0")
	}
	// the duration parameter is an integer number of seconds; the pacing helper works in milliseconds
	state.DelayBetweenRequestsInMS = getDelayBetweenRequestsInMsFixedAmount(extutil.ToUInt64(request.Config["duration"])*1000, state.NumberOfMessages)
	// reuse existing prepare if present in project
	return prepare(request, state, checkEndedPublishRabbitFixedAmount)
}

func checkEndedPublishRabbitFixedAmount(executionRunData *ExecutionRunData, state *PublishMessageAttackState) bool {
	return executionRunData.requestCounter.Load() >= state.NumberOfMessages
}
