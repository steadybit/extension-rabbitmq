// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2026 Steadybit GmbH
package extrabbitmq

import (
	"context"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/action-kit/go/action_kit_sdk"
	"github.com/steadybit/extension-kit/extbuild"
	"github.com/steadybit/extension-kit/extutil"
)

type publishExchangePeriodicallyAction struct {
	periodicPublishBehavior
}

// ensure interfaces
var (
	_ action_kit_sdk.Action[PublishMessageAttackState]           = (*publishExchangePeriodicallyAction)(nil)
	_ action_kit_sdk.ActionWithStatus[PublishMessageAttackState] = (*publishExchangePeriodicallyAction)(nil)
	_ action_kit_sdk.ActionWithStop[PublishMessageAttackState]   = (*publishExchangePeriodicallyAction)(nil)
)

func NewPublishExchangePeriodically() action_kit_sdk.Action[PublishMessageAttackState] {
	return &publishExchangePeriodicallyAction{}
}

func (a *publishExchangePeriodicallyAction) Describe() action_kit_api.ActionDescription {
	return action_kit_api.ActionDescription{
		Id:          "com.steadybit.extension_rabbitmq.exchange.publish-periodically",
		Label:       "Publish to Exchange (Messages / s)",
		Description: "Publish messages to an exchange at a constant rate (messages per second) for the attack duration. Delivery is determined by the exchange type and its bindings; unroutable messages count as failures. For publishing a fixed total number of messages, use Publish to Exchange (# of Messages) instead.",
		Version:     extbuild.GetSemverVersionStringOrUnknown(),
		Icon:        extutil.Ptr(rabbitMQIcon),
		TargetSelection: extutil.Ptr(action_kit_api.TargetSelection{
			TargetType: exchangeTargetId,
			SelectionTemplates: extutil.Ptr([]action_kit_api.TargetSelectionTemplate{
				{
					Label:       "exchange name",
					Description: extutil.Ptr("Find exchange by name"),
					Query:       "rabbitmq.exchange.name=\"\"",
				},
			}),
		}),
		Technology:  extutil.Ptr("RabbitMQ"),
		Category:    extutil.Ptr("RabbitMQ"),
		Kind:        action_kit_api.Attack,
		TimeControl: action_kit_api.TimeControlExternal,
		Parameters: []action_kit_api.ActionParameter{
			routingKeyExchange,
			headers,
			body,
			{
				Name:         "messagesPerSecond",
				Label:        "Messages per second",
				Description:  extutil.Ptr("The number of messages per second. Should be between 1 and 10."),
				Type:         action_kit_api.ActionParameterTypeInteger,
				DefaultValue: extutil.Ptr("1"),
				MinValue:     extutil.Ptr(1),
				MaxValue:     extutil.Ptr(10),
				Required:     extutil.Ptr(true),
			},
			durationPublishPeriodically,
			successRate,
			maxConcurrent,
		},
		Status: extutil.Ptr(action_kit_api.MutatingEndpointReferenceWithCallInterval{
			CallInterval: extutil.Ptr("1s"),
		}),
		Stop: extutil.Ptr(action_kit_api.MutatingEndpointReference{}),
	}
}

func (a *publishExchangePeriodicallyAction) Prepare(ctx context.Context, state *PublishMessageAttackState, request action_kit_api.PrepareActionRequestBody) (*action_kit_api.PrepareResult, error) {
	state.DelayBetweenRequestsInMS = getDelayBetweenRequestsInMsPeriodically(extutil.ToInt64(request.Config["messagesPerSecond"]))
	return prepareExchange(request, state, func(executionRunData *ExecutionRunData, state *PublishMessageAttackState) bool { return false })
}
