// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH
package extrabbitmq

import (
	"context"
	"errors"
	"github.com/rs/zerolog/log"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/action-kit/go/action_kit_sdk"
	"github.com/steadybit/extension-kit/extbuild"
	"github.com/steadybit/extension-kit/extutil"
)

type publishRabbitFixedAmountAction struct{}

// ensure interfaces
var (
	_ action_kit_sdk.Action[PublishMessageAttackState]           = (*publishRabbitFixedAmountAction)(nil)
	_ action_kit_sdk.ActionWithStatus[PublishMessageAttackState] = (*publishRabbitFixedAmountAction)(nil)
	_ action_kit_sdk.ActionWithStop[PublishMessageAttackState]   = (*publishRabbitFixedAmountAction)(nil)
)

func NewPublishRabbitFixedAmount() action_kit_sdk.Action[PublishMessageAttackState] {
	return &publishRabbitFixedAmountAction{}
}

func (a *publishRabbitFixedAmountAction) NewEmptyState() PublishMessageAttackState {
	return PublishMessageAttackState{}
}

func (a *publishRabbitFixedAmountAction) Describe() action_kit_api.ActionDescription {
	return action_kit_api.ActionDescription{
		Id:          "com.steadybit.extension_rabbitmq.queue.publish-fixed-amount",
		Label:       "Publish (# of Messages)",
		Description: "Publish a fixed number of messages.",
		Version:     extbuild.GetSemverVersionStringOrUnknown(),
		Icon:        extutil.Ptr(rabbitMQIcon),
		TargetSelection: extutil.Ptr(action_kit_api.TargetSelection{
			TargetType: queueTargetId,
		}),
		Technology:  extutil.Ptr("RabbitMQ"),
		Category:    extutil.Ptr("RabbitMQ"),
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
				Type:         action_kit_api.ActionParameterTypeInteger,
				Required:     extutil.Ptr(true),
				DefaultValue: extutil.Ptr("1"),
			},
			{
				Name:         "duration",
				Label:        "Duration (seconds)",
				Type:         action_kit_api.ActionParameterTypeInteger,
				Required:     extutil.Ptr(true),
				DefaultValue: extutil.Ptr("30"),
			},
			maxConcurrent,
		},
		Status: extutil.Ptr(action_kit_api.MutatingEndpointReferenceWithCallInterval{
			CallInterval: extutil.Ptr("1s"),
		}),
		Stop: extutil.Ptr(action_kit_api.MutatingEndpointReference{}),
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
	state.DelayBetweenRequestsInMS = getDelayBetweenRequestsInMsFixedAmount(extutil.ToUInt64(request.Config["duration"]), state.NumberOfMessages)
	// reuse existing prepare if present in project
	return prepare(request, state, checkEndedPublishRabbitFixedAmount)
}

func checkEndedPublishRabbitFixedAmount(executionRunData *ExecutionRunData, state *PublishMessageAttackState) bool {
	return executionRunData.requestCounter.Load() >= state.NumberOfMessages
}

func (a *publishRabbitFixedAmountAction) Start(ctx context.Context, state *PublishMessageAttackState) (*action_kit_api.StartResult, error) {
	start(state) // reuse existing start helper which should launch worker goroutines
	return nil, nil
}

func (a *publishRabbitFixedAmountAction) Status(ctx context.Context, state *PublishMessageAttackState) (*action_kit_api.StatusResult, error) {
	executionRunData, err := loadExecutionRunData(state.ExecutionID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to load execution run data")
		return nil, err
	}

	completed := checkEndedPublishRabbitFixedAmount(executionRunData, state)
	if completed {
		stopTickers(executionRunData)
		log.Info().Msg("Action completed")
	}

	latestMetrics := retrieveLatestMetrics(executionRunData.metrics)
	return &action_kit_api.StatusResult{
		Completed: completed,
		Metrics:   extutil.Ptr(latestMetrics),
	}, nil
}

func (a *publishRabbitFixedAmountAction) Stop(ctx context.Context, state *PublishMessageAttackState) (*action_kit_api.StopResult, error) {
	return stop(state)
}
