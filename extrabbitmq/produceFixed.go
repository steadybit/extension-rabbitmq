// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH
package extrabbitmq

/*
import (
	"context"
	"errors"
	"fmt"
	"github.com/steadybit/extension-rabbitmq/config"
	"time"

	"github.com/google/uuid"
	rabbithole "github.com/michaelklishin/rabbit-hole/v3"
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
	_ action_kit_sdk.Action[RabbitMQAttackState]           = (*produceRabbitFixedAmountAction)(nil)
	_ action_kit_sdk.ActionWithStatus[RabbitMQAttackState] = (*produceRabbitFixedAmountAction)(nil)
	_ action_kit_sdk.ActionWithStop[RabbitMQAttackState]   = (*produceRabbitFixedAmountAction)(nil)
)

func NewProduceRabbitFixedAmount() action_kit_sdk.Action[RabbitMQAttackState] {
	return &produceRabbitFixedAmountAction{}
}

func (a *produceRabbitFixedAmountAction) NewEmptyState() RabbitMQAttackState {
	return RabbitMQAttackState{}
}

func (a *produceRabbitFixedAmountAction) Describe() action_kit_api.ActionDescription {
	return action_kit_api.ActionDescription{
		Id:          "com.steadybit.extension_rabbitmq.rabbitmq-queue.produce-fixed-amount",
		Label:       "RabbitMQ: Produce (# of Messages)",
		Description: "Publish a fixed number of messages to an exchange using the Management API.",
		Version:     extbuild.GetSemverVersionStringOrUnknown(),
		Icon:        extutil.Ptr(""),
		TargetSelection: extutil.Ptr(action_kit_api.TargetSelection{
			TargetType: queueTargetId,
		}),
		Technology:  extutil.Ptr("RabbitMQ"),
		Category:    extutil.Ptr("RabbitMQ"),
		Kind:        action_kit_api.Attack,
		TimeControl: action_kit_api.TimeControlInternal,
		Parameters: []action_kit_api.ActionParameter{
			{
				Name:         "vhost",
				Label:        "Vhost",
				Type:         action_kit_api.ActionParameterTypeString,
				Required:     extutil.Ptr(true),
				DefaultValue: extutil.Ptr("/"),
			},
			{
				Name:         "exchange",
				Label:        "Exchange",
				Type:         action_kit_api.ActionParameterTypeString,
				Required:     extutil.Ptr(true),
				DefaultValue: extutil.Ptr(""),
			},
			{
				Name:         "routingKey",
				Label:        "Routing Key",
				Type:         action_kit_api.ActionParameterTypeString,
				Required:     extutil.Ptr(false),
				DefaultValue: extutil.Ptr(""),
			},
			{
				Name:         "body",
				Label:        "Message body",
				Type:         action_kit_api.ActionParameterTypeString,
				Required:     extutil.Ptr(false),
				DefaultValue: extutil.Ptr("test-message"),
			},
			{
				Name:         "numberOfMessages",
				Label:        "Number of Messages",
				Type:         action_kit_api.ActionParameterTypeInteger,
				Required:     extutil.Ptr(true),
				DefaultValue: extutil.Ptr("1"),
			},
			{
				Name:         "duration",
				Label:        "Duration (ms)",
				Type:         action_kit_api.ActionParameterTypeInteger,
				Required:     extutil.Ptr(true),
				DefaultValue: extutil.Ptr("1000"),
			},
			maxConcurrent,
		},
		Status: extutil.Ptr(action_kit_api.MutatingEndpointReferenceWithCallInterval{
			CallInterval: extutil.Ptr("1s"),
		}),
		Stop: extutil.Ptr(action_kit_api.MutatingEndpointReference{}),
	}
}

// RabbitMQAttackState mirrors the project's state pattern.
// Keep fields that prepare/start/stop helpers expect.
type RabbitMQAttackState struct {
	ExecutionID              uuid.UUID `json:"executionId"`
	NumberOfMessages         int64     `json:"numberOfMessages"`
	DelayBetweenRequestsInMS int64     `json:"delayBetweenRequestsInMS"`
	Vhost                    string    `json:"vhost"`
	Exchange                 string    `json:"exchange"`
	RoutingKey               string    `json:"routingKey"`
	Body                     string    `json:"body"`
	MaxConcurrent            int       `json:"maxConcurrent"`
	// plus any fields your project uses (e.g., StartedAt, etc.)
}

func getDelayBetweenRequestsInMsForMessages(durationMs int64, count int64) int64 {
	if durationMs > 0 && count > 0 {
		return durationMs / count
	}
	return 1000
}

// Prepare validates request and sets up state. It defers to shared prepare helpers where available.
func (a *produceRabbitFixedAmountAction) Prepare(ctx context.Context, state *RabbitMQAttackState, request action_kit_api.PrepareActionRequestBody) (*action_kit_api.PrepareResult, error) {
	if extutil.ToInt64(request.Config["duration"]) == 0 {
		return nil, errors.New("duration must be greater than 0")
	}
	state.NumberOfMessages = extutil.ToInt64(request.Config["numberOfMessages"])
	state.DelayBetweenRequestsInMS = getDelayBetweenRequestsInMsForMessages(extutil.ToInt64(request.Config["duration"]), state.NumberOfMessages)
	state.Vhost = extutil.ToString(request.Config["vhost"])
	state.Exchange = extutil.ToString(request.Config["exchange"])
	state.RoutingKey = extutil.ToString(request.Config["routingKey"])
	state.Body = extutil.ToString(request.Config["body"])
	state.MaxConcurrent = int(extutil.ToInt64(request.Config["maxConcurrent"]))
	// reuse existing prepare if present in project
	return prepare(request, state, checkEndedProduceRabbitFixedAmount)
}

func checkEndedProduceRabbitFixedAmount(executionRunData *ExecutionRunData, state *RabbitMQAttackState) bool {
	return executionRunData.requestCounter.Load() >= state.NumberOfMessages
}

func (a *produceRabbitFixedAmountAction) Start(ctx context.Context, state *RabbitMQAttackState) (*action_kit_api.StartResult, error) {
	start(state) // reuse existing start helper which should launch worker goroutines
	return nil, nil
}

func (a *produceRabbitFixedAmountAction) Status(ctx context.Context, state *RabbitMQAttackState) (*action_kit_api.StatusResult, error) {
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

func (a *produceRabbitFixedAmountAction) Stop(ctx context.Context, state *RabbitMQAttackState) (*action_kit_api.StopResult, error) {
	return stop(state)
}

// The worker function used by your shared start/runner helpers.
// It publishes one message via rabbit-hole Publish API and increments counters.
func rabbitPublishOnce(client *rabbithole.Client, state *RabbitMQAttackState) error {
	if client == nil {
		return errors.New("management client is nil")
	}

	req := rabbithole.PublishRequest{
		Properties:      map[string]interface{}{},
		RoutingKey:      state.RoutingKey,
		Payload:         state.Body,
		PayloadEncoding: "string",
	}

	_, err := client.Publish(state.Vhost, state.Exchange, req)
	return err
}

// This function will be called by the generic worker launcher in your project.
// It demonstrates the publish loop and interaction with ExecutionRunData.
// It assumes ExecutionRunData exposes: ctx, stopCh, requestCounter (atomic), metrics collector, and a semaphore for concurrency control.
func runRabbitPublishLoop(executionID uuid.UUID, state *RabbitMQAttackState) {
	executionRunData, err := loadExecutionRunData(executionID)
	if err != nil {
		log.Error().Err(err).Msg("failed to load execution run data for publish loop")
		return
	}

	// create management client using existing factory (createNewClient) and management URL from config or target attributes
	// assume executionRunData holds the management URL in executionRunData.targetManagementURL or similar
	// fallback to config.Config.ManagementURL if not present
	mgmtURL := executionRunData.ManagementURL
	if mgmtURL == "" {
		mgmtURL = config.Config.ManagementURL
	}
	client, err := createNewClient(mgmtURL)
	if err != nil {
		log.Error().Err(err).Msg("failed to create management client for publish loop")
		return
	}

	ticker := time.NewTicker(time.Duration(state.DelayBetweenRequestsInMS) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-executionRunData.Ctx.Done():
			return
		case <-executionRunData.StopCh:
			return
		case <-ticker.C:
			// concurrency control if present
			if executionRunData.Sem != nil {
				executionRunData.Sem.Acquire()
			}
			err := rabbitPublishOnce(client, state)
			if executionRunData.Sem != nil {
				executionRunData.Sem.Release()
			}
			if err != nil {
				log.Error().Err(err).Msg("publish failed")
				// record metric/error via executionRunData if available
			} else {
				executionRunData.requestCounter.Add(1)
			}
			// when reached goal finish
			if executionRunData.requestCounter.Load() >= state.NumberOfMessages {
				return
			}
		}
	}
}
*/
