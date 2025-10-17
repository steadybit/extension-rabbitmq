// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package extrabbitmq

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog/log"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/extension-kit/extutil"
	"sync"
	"sync/atomic"
	"time"
)

type ExecutionRunData struct {
	stopTicker            chan bool                  // stores the stop channels for each execution
	jobs                  chan time.Time             // stores the jobs for each execution
	tickers               *time.Ticker               // stores the tickers for each execution, to be able to stop them
	metrics               chan action_kit_api.Metric // stores the metrics for each execution
	requestCounter        atomic.Uint64              // stores the number of requests for each execution
	requestSuccessCounter atomic.Uint64              // stores the number of successful requests for each execution
}

var (
	ExecutionRunDataMap = sync.Map{} //make(map[uuid.UUID]*ExecutionRunData)
)

func prepare(request action_kit_api.PrepareActionRequestBody, state *ProduceMessageAttackState, checkEnded func(executionRunData *ExecutionRunData, state *ProduceMessageAttackState) bool) (*action_kit_api.PrepareResult, error) {
	if len(request.Target.Attributes["rabbitmq.queue.name"]) == 0 {
		return nil, fmt.Errorf("the target is missing the rabbitmq.queue.name attribute")
	}
	state.Queue = extutil.MustHaveValue(request.Target.Attributes, "rabbitmq.queue.name")[0]

	duration := extutil.ToInt64(request.Config["duration"])
	state.Timeout = time.Now().Add(time.Millisecond * time.Duration(duration))
	state.SuccessRate = extutil.ToInt(request.Config["successRate"])
	state.MaxConcurrent = extutil.ToInt(request.Config["maxConcurrent"])

	if state.MaxConcurrent == 0 {
		return nil, fmt.Errorf("max concurrent can't be zero")
	}
	state.NumberOfMessages = extutil.ToUInt64(request.Config["numberOfMessages"])
	state.RoutingKey = extutil.ToString(request.Config["routingKey"])
	state.Body = extutil.ToString(request.Config["body"])
	state.ExecutionID = request.ExecutionId

	var err error
	if _, ok := request.Config["recordHeaders"]; ok {
		state.RecordHeaders, err = extutil.ToKeyValue(request.Config, "recordHeaders")
		if err != nil {
			log.Error().Err(err).Msg("Failed to parse headers")
			return nil, err
		}
	}

	initExecutionRunData(state)
	executionRunData, err := loadExecutionRunData(state.ExecutionID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to load execution run data")
		return nil, err
	}

	// create worker pool
	for w := 1; w <= state.MaxConcurrent; w++ {
		go requestProducerWorker(executionRunData, state, checkEnded)
	}
	return nil, nil
}

func loadExecutionRunData(executionID uuid.UUID) (*ExecutionRunData, error) {
	erd, ok := ExecutionRunDataMap.Load(executionID)
	if !ok {
		return nil, fmt.Errorf("failed to load execution run data")
	}
	executionRunData := erd.(*ExecutionRunData)
	return executionRunData, nil
}

func initExecutionRunData(state *ProduceMessageAttackState) {
	saveExecutionRunData(state.ExecutionID, &ExecutionRunData{
		stopTicker:            make(chan bool),
		jobs:                  make(chan time.Time, state.MaxConcurrent),
		metrics:               make(chan action_kit_api.Metric, state.MaxConcurrent),
		requestCounter:        atomic.Uint64{},
		requestSuccessCounter: atomic.Uint64{},
	})
}

func saveExecutionRunData(executionID uuid.UUID, executionRunData *ExecutionRunData) {
	ExecutionRunDataMap.Store(executionID, executionRunData)
}

func createPublishRequest(state *ProduceMessageAttackState) (exchange string, routingKey string, pub amqp.Publishing) {
	// Map fields:
	// - state.RecordValue -> message body
	// - state.RecordKey   -> routing key (fallback to queue name)
	// - state.Queue       -> routing key fallback
	// - state.Exchange    -> target exchange (empty string means default exchange)

	ex := state.Exchange // allow empty for default exchange routing to queue
	rk := state.RoutingKey
	if rk == "" {
		rk = state.Queue
	}

	// Convert headers to amqp.Table
	var hdrs amqp.Table
	if len(state.RecordHeaders) > 0 {
		hdrs = amqp.Table{}
		for k, v := range state.RecordHeaders {
			hdrs[k] = v
		}
	}

	return ex, rk, amqp.Publishing{
		Headers:      hdrs,
		ContentType:  "text/plain",
		Body:         []byte(state.Body),
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now(),
	}
}

// Example usage inside your worker loop (replace Kafka produce with Publish):
func requestProducerWorker(executionRunData *ExecutionRunData, state *ProduceMessageAttackState, checkEnded func(*ExecutionRunData, *ProduceMessageAttackState) bool) {
	// connect to RabbitMQ using AMQP client instead of rabbit-hole
	endpoint := "amqp://guest:guest@localhost:5672/" // default connection string; could be adapted from your config
	conn, err := amqp.Dial(endpoint)
	if err != nil {
		log.Error().Err(err).Msg("Failed to connect to RabbitMQ via AMQP")
		return
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.Error().Err(err).Msg("Failed to open a channel")
		return
	}
	defer ch.Close()

	for range executionRunData.jobs {
		if !checkEnded(executionRunData, state) {
			exchange, routingKey, pub := createPublishRequest(state)

			// publish to exchange (empty exchange routes to queue)
			err = ch.PublishWithContext(context.Background(), exchange, routingKey, false, false, pub)
			executionRunData.requestCounter.Add(1)

			if err != nil {
				log.Error().Err(err).Msg("Failed to publish message")
			} else {
				executionRunData.requestSuccessCounter.Add(1)
			}
		}
	}
}

func start(state *ProduceMessageAttackState) {
	executionRunData, err := loadExecutionRunData(state.ExecutionID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to load execution run data")
	}
	executionRunData.tickers = time.NewTicker(time.Duration(state.DelayBetweenRequestsInMS) * time.Millisecond)
	executionRunData.stopTicker = make(chan bool)

	now := time.Now()
	log.Debug().Msgf("Schedule first record at %v", now)
	executionRunData.jobs <- now
	go func() {
		for {
			select {
			case <-executionRunData.stopTicker:
				log.Debug().Msg("Stop Record Scheduler")
				return
			case t := <-executionRunData.tickers.C:
				log.Debug().Msgf("Schedule Record at %v", t)
				executionRunData.jobs <- t
			}
		}
	}()
	ExecutionRunDataMap.Store(state.ExecutionID, executionRunData)
}

func retrieveLatestMetrics(metrics chan action_kit_api.Metric) []action_kit_api.Metric {

	statusMetrics := make([]action_kit_api.Metric, 0, len(metrics))
	for {
		select {
		case metric, ok := <-metrics:
			if ok {
				log.Debug().Msgf("Status Metric: %v", metric)
				statusMetrics = append(statusMetrics, metric)
			} else {
				log.Debug().Msg("Channel closed")
				return statusMetrics
			}
		default:
			log.Debug().Msg("No metrics available")
			return statusMetrics
		}
	}
}

func stop(state *ProduceMessageAttackState) (*action_kit_api.StopResult, error) {
	executionRunData, err := loadExecutionRunData(state.ExecutionID)
	if err != nil {
		log.Debug().Err(err).Msg("Execution run data not found, stop was already called")
		return nil, nil
	}
	stopTickers(executionRunData)

	latestMetrics := retrieveLatestMetrics(executionRunData.metrics)
	// calculate the success rate
	successRate := float64(executionRunData.requestSuccessCounter.Load()) / float64(executionRunData.requestCounter.Load()) * 100
	log.Debug().Msgf("Success Rate: %v%%", successRate)
	ExecutionRunDataMap.Delete(state.ExecutionID)
	if successRate < float64(state.SuccessRate) {
		log.Info().Msgf("Success Rate (%.2f%%) was below %v%%", successRate, state.SuccessRate)
		return extutil.Ptr(action_kit_api.StopResult{
			Metrics: extutil.Ptr(latestMetrics),
			Error: &action_kit_api.ActionKitError{
				Title:  fmt.Sprintf("Success Rate (%.2f%%) was below %v%%", successRate, state.SuccessRate),
				Status: extutil.Ptr(action_kit_api.Failed),
			},
		}), nil
	}
	log.Info().Msgf("Success Rate (%.2f%%) was above/equal %v%%", successRate, state.SuccessRate)
	return extutil.Ptr(action_kit_api.StopResult{
		Metrics: extutil.Ptr(latestMetrics),
	}), nil
}

func stopTickers(executionRunData *ExecutionRunData) {
	ticker := executionRunData.tickers
	if ticker != nil {
		ticker.Stop()
	}
	// non-blocking send
	select {
	case executionRunData.stopTicker <- true: // stop the ticker
		log.Trace().Msg("Stopped ticker")
	default:
		log.Debug().Msg("Ticker already stopped")
	}
}
