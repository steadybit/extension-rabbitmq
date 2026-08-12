// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2026 Steadybit GmbH
package extrabbitmq

import (
	"context"

	"github.com/rs/zerolog/log"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/extension-kit/extutil"
)

// fixedAmountPublishBehavior carries the Start/Status/Stop lifecycle shared by the
// fixed-amount publish actions (queue and exchange targeted). The action completes once
// the configured number of messages has been published.
type fixedAmountPublishBehavior struct{}

func (fixedAmountPublishBehavior) NewEmptyState() PublishMessageAttackState {
	return PublishMessageAttackState{}
}

func (fixedAmountPublishBehavior) Start(_ context.Context, state *PublishMessageAttackState) (*action_kit_api.StartResult, error) {
	start(state)
	return nil, nil
}

func (fixedAmountPublishBehavior) Status(_ context.Context, state *PublishMessageAttackState) (*action_kit_api.StatusResult, error) {
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

func (fixedAmountPublishBehavior) Stop(_ context.Context, state *PublishMessageAttackState) (*action_kit_api.StopResult, error) {
	return stop(state)
}

// periodicPublishBehavior carries the Start/Status/Stop lifecycle shared by the rate-based
// publish actions (queue and exchange targeted). The action runs until the platform stops it.
type periodicPublishBehavior struct{}

func (periodicPublishBehavior) NewEmptyState() PublishMessageAttackState {
	return PublishMessageAttackState{}
}

func (periodicPublishBehavior) Start(_ context.Context, state *PublishMessageAttackState) (*action_kit_api.StartResult, error) {
	start(state)
	return nil, nil
}

func (periodicPublishBehavior) Status(_ context.Context, state *PublishMessageAttackState) (*action_kit_api.StatusResult, error) {
	executionRunData, err := loadExecutionRunData(state.ExecutionID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to load execution run data")
		return nil, err
	}
	latestMetrics := retrieveLatestMetrics(executionRunData.metrics)
	return &action_kit_api.StatusResult{
		Completed: false,
		Metrics:   extutil.Ptr(latestMetrics),
	}, nil
}

func (periodicPublishBehavior) Stop(_ context.Context, state *PublishMessageAttackState) (*action_kit_api.StopResult, error) {
	return stop(state)
}
