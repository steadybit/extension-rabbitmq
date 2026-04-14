// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH
package extrabbitmq

import (
	"context"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/action-kit/go/action_kit_sdk"
	"github.com/steadybit/extension-kit/extbuild"
	"github.com/steadybit/extension-kit/extutil"
)

// new action: publish a fixed number of messages via the management API (rabbit-hole Publish)
type publishRabbitPeriodicallyAction struct{}

// ensure interfaces
var (
	_ action_kit_sdk.Action[PublishMessageAttackState]           = (*publishRabbitPeriodicallyAction)(nil)
	_ action_kit_sdk.ActionWithStatus[PublishMessageAttackState] = (*publishRabbitPeriodicallyAction)(nil)
	_ action_kit_sdk.ActionWithStop[PublishMessageAttackState]   = (*publishRabbitPeriodicallyAction)(nil)
)

func NewPublishRabbitPeriodically() action_kit_sdk.Action[PublishMessageAttackState] {
	return &publishRabbitPeriodicallyAction{}
}

func (a *publishRabbitPeriodicallyAction) NewEmptyState() PublishMessageAttackState {
	return PublishMessageAttackState{}
}

func (a *publishRabbitPeriodicallyAction) Describe() action_kit_api.ActionDescription {
	return action_kit_api.ActionDescription{
		Id:          "com.steadybit.extension_rabbitmq.queue.publish-periodically",
		Label:       "Publish (Messages / s)",
		Description: "Publish X messages per second for a given duration.",
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
			{
				Name:         "duration",
				Label:        "Duration (seconds)",
				Type:         action_kit_api.ActionParameterTypeInteger,
				Required:     new(true),
				DefaultValue: new("30"),
			},
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

func (a *publishRabbitPeriodicallyAction) Start(ctx context.Context, state *PublishMessageAttackState) (*action_kit_api.StartResult, error) {
	start(state) // reuse existing start helper which should launch worker goroutines
	return nil, nil
}

func (a *publishRabbitPeriodicallyAction) Status(ctx context.Context, state *PublishMessageAttackState) (*action_kit_api.StatusResult, error) {
	executionRunData, err := loadExecutionRunData(state.ExecutionID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to load execution run data")
		return nil, err
	}
	latestMetrics := retrieveLatestMetrics(executionRunData.metrics)
	return &action_kit_api.StatusResult{
		Completed: false,
		Metrics:   new(latestMetrics),
	}, nil
}

func (a *publishRabbitPeriodicallyAction) Stop(ctx context.Context, state *PublishMessageAttackState) (*action_kit_api.StopResult, error) {
	return stop(state)
}

func (a *publishRabbitPeriodicallyAction) getExecutionRunData(executionID uuid.UUID) (*ExecutionRunData, error) {
	return loadExecutionRunData(executionID)
}
