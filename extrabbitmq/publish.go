// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package extrabbitmq

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog/log"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/extension-kit/extutil"
	"github.com/steadybit/extension-rabbitmq/clients"
	"github.com/steadybit/extension-rabbitmq/config"
)

type ExecutionRunData struct {
	stopTicker            chan bool                  // stores the stop channels for each execution
	jobs                  chan time.Time             // stores the jobs for each execution
	tickers               *time.Ticker               // stores the tickers for each execution, to be able to stop them
	metrics               chan action_kit_api.Metric // stores the metrics for each execution
	requestCounter        atomic.Uint64              // stores the number of requests for each execution
	requestSuccessCounter atomic.Uint64              // stores the number of successful requests for each execution
	stopOnce              sync.Once                  // ensures ticker stop/close happens once
	jobsCloseOnce         sync.Once                  // ensures the jobs channel is closed only once
	tickerDone            chan struct{}              // closed when the ticker goroutine exits
	workers               sync.WaitGroup             // tracks publisher workers so stop can wait for in-flight confirms
}

func logReturn(ret amqp.Return, msg string) {
	log.Error().
		Str("exchange", ret.Exchange).
		Str("routingKey", ret.RoutingKey).
		Uint16("code", ret.ReplyCode).
		Str("text", ret.ReplyText).
		Msg(msg)
}

func startReturnsLogger(ch *amqp.Channel, buf int) <-chan amqp.Return {
	r := ch.NotifyReturn(make(chan amqp.Return, buf))
	go func() {
		for ret := range r {
			logReturn(ret, "message returned (unroutable)")
		}
	}()
	return r
}

// armChannel enables publisher confirms on ch (best-effort) and registers the listeners used to
// judge delivery. When confirms are unavailable there is no sync point to correlate returns with,
// so returned messages are only logged.
func armChannel(ch *amqp.Channel, buf int) (confirms <-chan amqp.Confirmation, returns <-chan amqp.Return) {
	if err := ch.Confirm(false); err != nil {
		log.Debug().Msg("publisher confirms not available")
		_ = startReturnsLogger(ch, buf)
		return nil, nil
	}
	confirms = ch.NotifyPublish(make(chan amqp.Confirmation, buf))
	// consumed by the worker itself to exclude unroutable messages from the success count
	returns = ch.NotifyReturn(make(chan amqp.Return, buf))
	return confirms, returns
}

var (
	ExecutionRunDataMap = sync.Map{} //make(map[uuid.UUID]*ExecutionRunData)
)

// maxQueueTargetsWithExchange is the maximum number of queue targets a single publish step
// may prepare when the exchange parameter is set.
const maxQueueTargetsWithExchange = 10

var exchangeGuard = struct {
	sync.Mutex
	entries map[string]*exchangeGuardEntry
}{entries: map[string]*exchangeGuardEntry{}}

type exchangeGuardEntry struct {
	targets  map[uuid.UUID]struct{}
	lastSeen time.Time
}

// guardExchangeTargetCount fails the preparation once more than maxQueueTargetsWithExchange
// targets prepare a queue publish with the same exchange parameter within one experiment
// execution. With an exchange set, every targeted queue starts an identical publisher against
// that same exchange, multiplying the load by the number of targets while the targeted queue
// itself contributes nothing but its vhost.
//
// The platform does not tell the extension how many targets an execution has, so distinct
// target preparations are counted per (experiment execution, exchange, routing key):
//   - steps publishing to different exchanges or routing keys count separately, so an
//     execution with several distinct publish steps is not falsely aborted;
//   - targets are identified by their action execution ID, so a platform-side retry of the
//     same target's prepare does not consume additional budget;
//   - the guard is per extension process — with multiple replicas the preparations of one
//     execution may split across processes and the limit may not be reached.
func guardExchangeTargetCount(request action_kit_api.PrepareActionRequestBody, exchange, routingKey string) error {
	ec := request.ExecutionContext
	if ec == nil || ec.ExperimentKey == nil || ec.ExecutionId == nil {
		log.Warn().Msg("cannot enforce the exchange target-count guard: the prepare request has no execution context")
		return nil
	}
	key := fmt.Sprintf("%s/%d/%s/%s", *ec.ExperimentKey, *ec.ExecutionId, exchange, routingKey)
	now := time.Now()

	exchangeGuard.Lock()
	defer exchangeGuard.Unlock()
	for k, e := range exchangeGuard.entries {
		if now.Sub(e.lastSeen) > time.Hour {
			delete(exchangeGuard.entries, k)
		}
	}
	entry := exchangeGuard.entries[key]
	if entry == nil {
		entry = &exchangeGuardEntry{targets: map[uuid.UUID]struct{}{}}
		exchangeGuard.entries[key] = entry
	}
	entry.targets[request.ExecutionId] = struct{}{}
	entry.lastSeen = now
	if len(entry.targets) > maxQueueTargetsWithExchange {
		return fmt.Errorf("the exchange parameter is set and more than %d queue targets were prepared: restrict the target selection to at most %d queues, or use the Publish to Exchange action instead", maxQueueTargetsWithExchange, maxQueueTargetsWithExchange)
	}
	return nil
}

// prepareFixedAmount is shared by the queue and exchange fixed-amount publish actions, which
// differ only in how the destination is resolved. prepareTarget is prepare or prepareExchange.
func prepareFixedAmount(request action_kit_api.PrepareActionRequestBody, state *PublishMessageAttackState, prepareTarget func(action_kit_api.PrepareActionRequestBody, *PublishMessageAttackState, func(*ExecutionRunData, *PublishMessageAttackState) bool) (*action_kit_api.PrepareResult, error)) (*action_kit_api.PrepareResult, error) {
	state.NumberOfMessages = extutil.ToUInt64(request.Config["numberOfMessages"])

	if extutil.ToInt64(request.Config["duration"]) == 0 {
		return nil, errors.New("duration must be greater than 0")
	}
	if state.NumberOfMessages == 0 {
		return nil, errors.New("numberOfMessages must be greater than 0")
	}
	state.DelayBetweenRequestsInMS = getDelayBetweenRequestsInMsFixedAmount(extutil.ToUInt64(request.Config["duration"])*1000, state.NumberOfMessages)
	return prepareTarget(request, state, checkEndedPublishRabbitFixedAmount)
}

func prepare(request action_kit_api.PrepareActionRequestBody, state *PublishMessageAttackState, checkEnded func(executionRunData *ExecutionRunData, state *PublishMessageAttackState) bool) (*action_kit_api.PrepareResult, error) {
	if len(request.Target.Attributes["rabbitmq.queue.name"]) == 0 {
		return nil, fmt.Errorf("the target is missing the rabbitmq.queue.name attribute")
	}
	state.Queue = extutil.MustHaveValue(request.Target.Attributes, "rabbitmq.queue.name")[0]
	state.Exchange = extutil.ToString(request.Config["exchange"])
	if state.Exchange != "" {
		if err := guardExchangeTargetCount(request, state.Exchange, extutil.ToString(request.Config["routingKey"])); err != nil {
			return nil, err
		}
	}
	return prepareCommon(request, state, "rabbitmq.queue.vhost", checkEnded)
}

// prepareExchange is the prepare variant for the exchange-targeted publish actions. The exchange
// comes from the target instead of a config parameter and there is no queue to fall back to for
// the routing key: an empty routing key is published as-is.
func prepareExchange(request action_kit_api.PrepareActionRequestBody, state *PublishMessageAttackState, checkEnded func(executionRunData *ExecutionRunData, state *PublishMessageAttackState) bool) (*action_kit_api.PrepareResult, error) {
	if len(request.Target.Attributes["rabbitmq.exchange.name"]) == 0 {
		return nil, fmt.Errorf("the target is missing the rabbitmq.exchange.name attribute")
	}
	state.Exchange = extutil.MustHaveValue(request.Target.Attributes, "rabbitmq.exchange.name")[0]
	return prepareCommon(request, state, "rabbitmq.exchange.vhost", checkEnded)
}

func prepareCommon(request action_kit_api.PrepareActionRequestBody, state *PublishMessageAttackState, vhostAttribute string, checkEnded func(executionRunData *ExecutionRunData, state *PublishMessageAttackState) bool) (*action_kit_api.PrepareResult, error) {
	var err error
	durationMs := extutil.ToInt64(request.Config["duration"]) * 1000
	state.Timeout = time.Now().Add(time.Millisecond * time.Duration(durationMs))
	state.SuccessRate = extutil.ToInt(request.Config["successRate"])
	state.MaxConcurrent = extutil.ToInt(request.Config["maxConcurrent"])

	if state.MaxConcurrent == 0 {
		return nil, fmt.Errorf("max concurrent can't be zero")
	}
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
	if len(request.Target.Attributes[vhostAttribute]) > 0 {
		vhostAttr = request.Target.Attributes[vhostAttribute][0]
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
		if durationMs > 0 && state.NumberOfMessages > 0 {
			per = durationMs / int64(state.NumberOfMessages)
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
		executionRunData.workers.Add(1)
		go requestPublisherWorker(executionRunData, state, checkEnded)
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

func initExecutionRunData(state *PublishMessageAttackState) {
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

func createPublishRequest(state *PublishMessageAttackState) (exchange string, routingKey string, pub amqp.Publishing) {
	// Map fields:
	// - state.Body -> message body
	// - state.RoutingKey   -> routing key (fallback to queue name)
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

type publishOutcome int

const (
	publishDelivered publishOutcome = iota
	publishFailed
	// publishStateUnknown means no confirm was received for the message. The confirm stream can
	// no longer be trusted: a late ack would be attributed to the next message, so the caller
	// must drop the connection and start over with fresh channels.
	publishStateUnknown
)

const publishConfirmTimeout = 5 * time.Second

// redialMinInterval is the minimum delay between two reconnect attempts of a worker.
const redialMinInterval = time.Second

// awaitPublishOutcome waits for the broker confirm of the message just published and reports
// whether it was actually delivered. A message published with mandatory=true that cannot be
// routed is acked by the broker after a basic.return, so an ack alone is not proof of delivery.
// The broker sends the return before the corresponding ack and amqp091-go dispatches both in
// frame order, so a returned message is already readable from the returns channel when its
// confirm arrives. Each worker has at most one message in flight per channel.
func awaitPublishOutcome(confirms <-chan amqp.Confirmation, returns <-chan amqp.Return, exchange, routingKey string, timeout time.Duration) publishOutcome {
	select {
	case c, ok := <-confirms:
		if !ok {
			log.Error().Str("exchange", exchange).Str("routingKey", routingKey).Msg("confirms channel closed before publish was confirmed")
			return publishStateUnknown
		}
		if !c.Ack {
			log.Error().Str("exchange", exchange).Str("routingKey", routingKey).Msg("publish nack")
			return publishFailed
		}
		select {
		case ret, ok := <-returns:
			if !ok {
				// the returns channel closes when the connection drops; the ack for this
				// message already arrived, so it was delivered
				return publishDelivered
			}
			logReturn(ret, "message returned (unroutable), not counted as success")
			return publishFailed
		default:
			return publishDelivered
		}
	case <-time.After(timeout):
		log.Error().Str("exchange", exchange).Str("routingKey", routingKey).Msg("no publish confirm within timeout")
		return publishStateUnknown
	}
}

func requestPublisherWorker(executionRunData *ExecutionRunData, state *PublishMessageAttackState, checkEnded func(*ExecutionRunData, *PublishMessageAttackState) bool) {
	defer executionRunData.workers.Done()
	// Dial once per worker, reuse channel
	conn, ch, err := clients.CreateNewAMQPConnection(state.AmqpURL, state.AmqpUser, state.AmqpPassword, state.AmqpInsecureSkipVerify, state.AmqpCA)
	if err != nil {
		log.Error().Err(err).Msg("AMQP connect failed")
		return
	}

	// Enable confirms (best-effort). If not supported, continue without.
	confirms, returns := armChannel(ch, state.MaxConcurrent*2)

	// Prepare static publishing data once
	exchRequest, routingKeyExchange, pubTemplate := createPublishRequest(state)

	// Helper to (re)dial once on demand
	var lastRedial time.Time
	redial := func() error {
		// Throttle reconnects: a permanently failing publish (e.g. a nonexistent exchange
		// closing the channel on every attempt) must not flood the broker with a new
		// TCP/TLS connection per message.
		if wait := redialMinInterval - time.Since(lastRedial); wait > 0 {
			time.Sleep(wait)
		}
		lastRedial = time.Now()
		if conn != nil {
			_ = conn.Close()
		}
		if ch != nil {
			_ = ch.Close()
		}
		var e error
		conn, ch, e = clients.CreateNewAMQPConnection(state.AmqpURL, state.AmqpUser, state.AmqpPassword, state.AmqpInsecureSkipVerify, state.AmqpCA)
		if e != nil {
			conn = nil
			ch = nil
			return e
		}
		confirms, returns = armChannel(ch, state.MaxConcurrent*2)
		return nil
	}

	for range executionRunData.jobs {
		if !checkEnded(executionRunData, state) {
			// If the channel is nil (e.g. after a failed redial), try to reconnect before publishing.
			if ch == nil {
				if re := redial(); re != nil {
					log.Error().Err(re).Msg("amqp redial failed, skipping message")
					executionRunData.requestCounter.Add(1)
					continue
				}
			}

			// per-message payload (cheap copy)
			pub := pubTemplate
			pub.Timestamp = time.Now()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err = ch.PublishWithContext(ctx, exchRequest, routingKeyExchange, true, false, pub)
			cancel()

			executionRunData.requestCounter.Add(1)
			if err != nil {
				log.Error().Err(err).Str("exchange", exchRequest).Str("routingKey", routingKeyExchange).Msg("publish failed")
				// single retry after redial
				if re := redial(); re == nil {
					ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
					err = ch.PublishWithContext(ctx2, exchRequest, routingKeyExchange, true, false, pub)
					cancel2()
					if err != nil {
						log.Error().Err(err).Str("exchange", exchRequest).Str("routingKey", routingKeyExchange).Msg("publish failed after redial")
						continue
					}
				} else {
					log.Error().Err(re).Msg("amqp redial failed")
					continue
				}
			}

			// Wait for confirm if available, else count success immediately
			if confirms == nil {
				executionRunData.requestSuccessCounter.Add(1)
				continue
			}
			switch awaitPublishOutcome(confirms, returns, exchRequest, routingKeyExchange, publishConfirmTimeout) {
			case publishDelivered:
				executionRunData.requestSuccessCounter.Add(1)
			case publishStateUnknown:
				// drop the connection so the next job redials with fresh, in-sync channels
				if ch != nil {
					_ = ch.Close()
				}
				if conn != nil {
					_ = conn.Close()
				}
				conn, ch = nil, nil
				confirms, returns = nil, nil
			}
		}
	}
	if conn != nil {
		_ = conn.Close()
	}
	if ch != nil {
		_ = ch.Close()
	}
}

func start(state *PublishMessageAttackState) {
	executionRunData, err := loadExecutionRunData(state.ExecutionID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to load execution run data")
	}
	executionRunData.tickers = time.NewTicker(time.Duration(state.DelayBetweenRequestsInMS) * time.Millisecond)
	executionRunData.stopTicker = make(chan bool)
	executionRunData.tickerDone = make(chan struct{})

	now := time.Now()
	log.Debug().Msgf("Schedule first message at %v", now)
	select {
	case executionRunData.jobs <- now:
	case <-executionRunData.stopTicker:
		return
	}
	go func() {
		defer close(executionRunData.tickerDone)
		for {
			select {
			case <-executionRunData.stopTicker:
				log.Debug().Msg("Stop Message Scheduler")
				return
			case t := <-executionRunData.tickers.C:
				select {
				case executionRunData.jobs <- t:
					log.Debug().Msgf("Schedule Message at %v", t)
				case <-executionRunData.stopTicker:
					log.Debug().Msg("Stop Message Scheduler")
					return
				}
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

func stop(state *PublishMessageAttackState) (*action_kit_api.StopResult, error) {
	executionRunData, err := loadExecutionRunData(state.ExecutionID)
	if err != nil {
		log.Debug().Err(err).Msg("Execution run data not found, stop was already called")
		return nil, nil
	}
	stopTickers(executionRunData)
	if executionRunData.tickerDone != nil {
		<-executionRunData.tickerDone
	}
	// Guard against a concurrent/duplicate stop closing the jobs channel twice (which panics):
	// two stop() calls can both load the run data before either deletes it from the map.
	executionRunData.jobsCloseOnce.Do(func() {
		close(executionRunData.jobs)
	})

	// Wait (bounded) for the publisher workers to finish their in-flight message before
	// computing the success rate: the last message's broker confirm otherwise races the
	// verdict, and a fully successful run can report e.g. 119/120. The bound covers the
	// common case (one redial throttle plus one confirm timeout); a fully degraded worker
	// can exceed it (two 5s publish context timeouts on a dead connection), in which case
	// the warning path below applies and the rate is computed as before this fix.
	workersDone := make(chan struct{})
	go func() {
		executionRunData.workers.Wait()
		close(workersDone)
	}()
	select {
	case <-workersDone:
	case <-time.After(publishConfirmTimeout + redialMinInterval + 2*time.Second):
		log.Warn().Msg("publish workers did not finish within the stop timeout, computing the success rate with in-flight messages uncounted")
	}

	latestMetrics := retrieveLatestMetrics(executionRunData.metrics)
	// calculate the success rate
	var successRate float64
	if total := executionRunData.requestCounter.Load(); total > 0 {
		successRate = float64(executionRunData.requestSuccessCounter.Load()) / float64(total) * 100
	} else {
		successRate = 0
	}
	log.Debug().Msgf("Success Rate: %v%%", successRate)
	ExecutionRunDataMap.Delete(state.ExecutionID)
	if successRate < float64(state.SuccessRate) {
		log.Info().Msgf("Success Rate (%.2f%%) was below %v%%", successRate, state.SuccessRate)
		return new(action_kit_api.StopResult{
			Metrics: new(latestMetrics),
			Error: &action_kit_api.ActionKitError{
				Title:  fmt.Sprintf("Success Rate (%.2f%%) was below %v%%", successRate, state.SuccessRate),
				Status: extutil.Ptr(action_kit_api.Failed),
			},
		}), nil
	}
	log.Info().Msgf("Success Rate (%.2f%%) was above/equal %v%%", successRate, state.SuccessRate)
	return new(action_kit_api.StopResult{
		Metrics: new(latestMetrics),
	}), nil
}

func stopTickers(executionRunData *ExecutionRunData) {
	executionRunData.stopOnce.Do(func() {
		if t := executionRunData.tickers; t != nil {
			t.Stop()
		}
		close(executionRunData.stopTicker)
		log.Trace().Msg("Stopped ticker")
	})
}
