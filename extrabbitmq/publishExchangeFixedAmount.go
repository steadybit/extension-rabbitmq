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

type publishExchangeFixedAmountAction struct {
	fixedAmountPublishBehavior
}

// ensure interfaces
var (
	_ action_kit_sdk.Action[PublishMessageAttackState]           = (*publishExchangeFixedAmountAction)(nil)
	_ action_kit_sdk.ActionWithStatus[PublishMessageAttackState] = (*publishExchangeFixedAmountAction)(nil)
	_ action_kit_sdk.ActionWithStop[PublishMessageAttackState]   = (*publishExchangeFixedAmountAction)(nil)
)

func NewPublishExchangeFixedAmount() action_kit_sdk.Action[PublishMessageAttackState] {
	return &publishExchangeFixedAmountAction{}
}

func (a *publishExchangeFixedAmountAction) Describe() action_kit_api.ActionDescription {
	return action_kit_api.ActionDescription{
		Id:          "com.steadybit.extension_rabbitmq.exchange.publish-fixed-amount",
		Label:       "Publish to Exchange (# of Messages)",
		Description: "Publish a fixed total number of messages to an exchange, distributed evenly across the duration. Delivery is determined by the exchange type and its bindings; unroutable messages count as failures. For rate-based publishing, use Publish to Exchange (Messages / s) instead.",
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
		TimeControl: action_kit_api.TimeControlInternal,
		Parameters: []action_kit_api.ActionParameter{
			routingKeyExchange,
			headers,
			body,
			{
				Name:         "numberOfMessages",
				Label:        "Number of Messages",
				Description:  extutil.Ptr("Total number of messages to publish across the entire duration. The publishing rate is this value divided by the duration."),
				Type:         action_kit_api.ActionParameterTypeInteger,
				Required:     extutil.Ptr(true),
				DefaultValue: extutil.Ptr("1"),
				MinValue:     extutil.Ptr(1),
			},
			durationPublishFixedAmount,
			successRate,
			maxConcurrent,
		},
		Status: extutil.Ptr(action_kit_api.MutatingEndpointReferenceWithCallInterval{
			CallInterval: extutil.Ptr("1s"),
		}),
		Stop: extutil.Ptr(action_kit_api.MutatingEndpointReference{}),
	}
}

func (a *publishExchangeFixedAmountAction) Prepare(ctx context.Context, state *PublishMessageAttackState, request action_kit_api.PrepareActionRequestBody) (*action_kit_api.PrepareResult, error) {
	return prepareFixedAmount(request, state, prepareExchange)
}
