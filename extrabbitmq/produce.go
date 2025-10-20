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
	"github.com/steadybit/extension-rabbitmq/config"
	"net/url"
	"strings"
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
	var err error
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

	// AMQP Config
	amqpAttr := extutil.MustHaveValue(request.Target.Attributes, "rabbitmq.amqp.url")[0]
	configAmqp, err := config.GetEndpointByAMQPURL(amqpAttr)
	if err != nil {
		return nil, err
	}

	// determine vhost from target attributes
	vhostAttr := "/"
	if len(request.Target.Attributes["rabbitmq.queue.vhost"]) > 0 {
		vhostAttr = request.Target.Attributes["rabbitmq.queue.vhost"][0]
	}
	state.Vhost = vhostAttr

	finalAMQP, err := buildAMQPURL(configAmqp.AMQP.URL, vhostAttr, configAmqp.AMQP.Username, configAmqp.AMQP.Password)
	if err != nil {
		log.Error().Err(err).Msg("failed to build AMQP URL")
		return nil, err
	}
	state.AmqpURL = finalAMQP
	state.AmqpUser = configAmqp.AMQP.Username
	state.AmqpPassword = configAmqp.AMQP.Password
	state.AmqpCA = configAmqp.AMQP.CAFile
	state.AmqpInsecureSkipVerify = configAmqp.AMQP.InsecureSkipVerify

	if _, ok := request.Config["headers"]; ok {
		state.Headers, err = extutil.ToKeyValue(request.Config, "headers")
		if err != nil {
			log.Error().Err(err).Msg("Failed to parse headers")
			return nil, err
		}
	}

	// Ensure a positive tick interval. If not given, derive from duration/numberOfMessages or default.
	if state.DelayBetweenRequestsInMS <= 0 {
		per := int64(0)
		if duration > 0 && state.NumberOfMessages > 0 {
			per = duration / int64(state.NumberOfMessages)
		}
		if per <= 0 {
			per = 100 // 100ms sensible default
		}
		state.DelayBetweenRequestsInMS = uint64(int(per))
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

func buildAMQPURL(base, vhost, user, pass string) (string, error) {
	if strings.TrimSpace(base) == "" {
		return "", fmt.Errorf("amqp base URL empty")
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	// set vhost path
	if vhost == "" || vhost == "/" {
		u.Path = "/"
	} else {
		u.Path = "/" + url.PathEscape(strings.TrimPrefix(vhost, "/"))
	}
	// inject credentials if not present
	if u.User == nil && (user != "" || pass != "") {
		u.User = url.UserPassword(user, pass)
	}
	return u.String(), nil
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
	if len(state.Headers) > 0 {
		hdrs = amqp.Table{}
		for k, v := range state.Headers {
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
	amqpConn, amqpChan, err := createNewAMQPConnection(state.AmqpURL, state.AmqpUser, state.AmqpPassword, state.AmqpInsecureSkipVerify, state.AmqpCA)
	if err != nil {
		log.Error().Err(err).Msg("Failed to connect to RabbitMQ via AMQP")
		return
	}
	defer amqpConn.Close()
	defer amqpChan.Close()

	// Ensure target queue exists in the selected vhost without creating it (fail fast if missing)
	if state.Queue != "" {
		if _, qErr := amqpChan.QueueDeclarePassive(state.Queue, true, false, false, false, nil); qErr != nil {
			log.Error().Err(qErr).Str("queue", state.Queue).Str("vhost", state.Vhost).Msg("Queue not found or inaccessible; aborting publish worker")
			return
		}
	}

	// Enable publisher confirms and set up listeners
	if err := amqpChan.Confirm(false); err != nil {
		log.Warn().Err(err).Msg("Publisher confirms not available; proceeding without confirms")
	}
	confirms := amqpChan.NotifyPublish(make(chan amqp.Confirmation, state.MaxConcurrent*4))
	returns := amqpChan.NotifyReturn(make(chan amqp.Return, state.MaxConcurrent*4))

	// Log returned (unroutable) messages
	go func() {
		for r := range returns {
			log.Warn().Str("exchange", r.Exchange).Str("routingKey", r.RoutingKey).Uint16("code", r.ReplyCode).Str("text", r.ReplyText).Msg("Message returned by broker (unroutable)")
		}
	}()

	for range executionRunData.jobs {
		if !checkEnded(executionRunData, state) {
			exchangeCreated, routingKey, pub := createPublishRequest(state)

			// Drain stale notifications
			for {
				select {
				case <-confirms:
					continue
				case <-returns:
					continue
				default:
					goto drained
				}
			}
		drained:

			// Publish with mandatory=true, immediate=false so unroutable messages are returned
			mandatory := true
			immediate := false
			err = amqpChan.PublishWithContext(context.Background(), exchangeCreated, routingKey, mandatory, immediate, pub)
			executionRunData.requestCounter.Add(1)
			if err != nil {
				log.Error().Err(err).Str("exchange", exchangeCreated).Str("routingKey", routingKey).Msg("Failed to publish message")
				continue
			}

			select {
			case r := <-returns:
				log.Error().Str("exchange", r.Exchange).Str("routingKey", r.RoutingKey).Uint16("code", r.ReplyCode).Str("text", r.ReplyText).Msg("Message returned (unroutable)")
				continue
			case c := <-confirms:
				if c.Ack {
					executionRunData.requestSuccessCounter.Add(1)
				} else {
					log.Error().Str("exchange", exchangeCreated).Str("routingKey", routingKey).Msg("Publishing negatively acknowledged (nack)")
				}
			case <-time.After(5 * time.Second):
				log.Error().Str("exchange", exchangeCreated).Str("routingKey", routingKey).Msg("No publish confirm within 5s")
			}
		}
	}
}

func start(state *ProduceMessageAttackState) {
	executionRunData, err := loadExecutionRunData(state.ExecutionID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to load execution run data")
	}
	interval := time.Duration(state.DelayBetweenRequestsInMS) * time.Millisecond
	if interval <= 0 {
		log.Warn().Uint64("DelayBetweenRequestsInMS", state.DelayBetweenRequestsInMS).Msg("non-positive interval; defaulting to 100ms")
		interval = 100 * time.Millisecond
	}
	executionRunData.tickers = time.NewTicker(interval)
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
