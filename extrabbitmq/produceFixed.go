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

// new action: produce a fixed number of messages via the management API (rabbit-hole Publish)
type produceRabbitFixedAmountAction struct{}

// ensure interfaces
var (
	_ action_kit_sdk.Action[ProduceMessageAttackState]           = (*produceRabbitFixedAmountAction)(nil)
	_ action_kit_sdk.ActionWithStatus[ProduceMessageAttackState] = (*produceRabbitFixedAmountAction)(nil)
	_ action_kit_sdk.ActionWithStop[ProduceMessageAttackState]   = (*produceRabbitFixedAmountAction)(nil)
)

func NewProduceRabbitFixedAmount() action_kit_sdk.Action[ProduceMessageAttackState] {
	return &produceRabbitFixedAmountAction{}
}

func (a *produceRabbitFixedAmountAction) NewEmptyState() ProduceMessageAttackState {
	return ProduceMessageAttackState{}
}

func (a *produceRabbitFixedAmountAction) Describe() action_kit_api.ActionDescription {
	return action_kit_api.ActionDescription{
		Id:          "com.steadybit.extension_rabbitmq.rabbitmq-queue.produce-fixed-amount",
		Label:       "RabbitMQ: Produce (# of Messages)",
		Description: "Publish a fixed number of messages to an exchange using the Management API.",
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
			vhost,
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
func (a *produceRabbitFixedAmountAction) Prepare(ctx context.Context, state *ProduceMessageAttackState, request action_kit_api.PrepareActionRequestBody) (*action_kit_api.PrepareResult, error) {
	if extutil.ToInt64(request.Config["duration"]) == 0 {
		return nil, errors.New("duration must be greater than 0")
	}
	state.DelayBetweenRequestsInMS = getDelayBetweenRequestsInMsFixedAmount(extutil.ToUInt64(request.Config["duration"]), state.NumberOfMessages)
	// reuse existing prepare if present in project
	return prepare(request, state, checkEndedProduceRabbitFixedAmount)
}

func checkEndedProduceRabbitFixedAmount(executionRunData *ExecutionRunData, state *ProduceMessageAttackState) bool {
	return executionRunData.requestCounter.Load() >= state.NumberOfMessages
}

func (a *produceRabbitFixedAmountAction) Start(ctx context.Context, state *ProduceMessageAttackState) (*action_kit_api.StartResult, error) {
	start(state) // reuse existing start helper which should launch worker goroutines
	return nil, nil
}

func (a *produceRabbitFixedAmountAction) Status(ctx context.Context, state *ProduceMessageAttackState) (*action_kit_api.StatusResult, error) {
	executionRunData, err := loadExecutionRunData(state.ExecutionID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to load execution run data")
		return nil, err
	}

	completed := checkEndedProduceRabbitFixedAmount(executionRunData, state)
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

func (a *produceRabbitFixedAmountAction) Stop(ctx context.Context, state *ProduceMessageAttackState) (*action_kit_api.StopResult, error) {
	return stop(state)
}
