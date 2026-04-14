package extrabbitmq

import (
	"encoding/json"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
)

func mustParse(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return u
}

func Test_buildAMQPURL_injectsCreds_and_VhostRoot(t *testing.T) {
	got, err := buildAMQPURL("amqps://broker.internal:5671", "/", "u", "p")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	u := mustParse(t, got)
	if u.User == nil {
		t.Fatalf("expected userinfo")
	}
	user := u.User.Username()
	pass, _ := u.User.Password()
	if user != "u" || pass != "p" {
		t.Fatalf("userinfo got %s:%s", user, pass)
	}
	if u.Path != "/" {
		t.Fatalf("vhost path got %q", u.Path)
	}
}

func Test_buildAMQPURL_preservesExistingCreds_and_Vhost(t *testing.T) {
	got, err := buildAMQPURL("amqp://x:y@host/vh", "ignored", "u", "p") //NOSONAR go:S2068
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	u := mustParse(t, got)
	user := u.User.Username()
	pass, _ := u.User.Password()
	if user != "x" || pass != "y" {
		t.Fatalf("should keep existing creds, got %s:%s", user, pass)
	}
}

func Test_buildAMQPURL_setsCustomVhost(t *testing.T) {
	got, err := buildAMQPURL("amqp://host", "team A/vh", "u", "p")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	u := mustParse(t, got)
	// space must be escaped
	if u.Path != "/team%20A%2Fvh" {
		t.Fatalf("escaped vhost got %q", u.Path)
	}
}

func Test_createPublishRequest_defaultsAndHeaders(t *testing.T) {
	state := &PublishMessageAttackState{
		Exchange:   "",
		RoutingKey: "",
		Queue:      "queueA",
		Body:       "hello",
		Headers: map[string]string{
			"k1": "v1",
			"k2": "v2",
		},
	}
	ex, rk, pub := createPublishRequest(state)
	if ex != "" {
		t.Fatalf("exchange expected empty, got %q", ex)
	}
	if rk != "queueA" {
		t.Fatalf("routingKey fallback to queue, got %q", rk)
	}
	if string(pub.Body) != "hello" {
		t.Fatalf("body mismatch")
	}
	if pub.ContentType != "text/plain" {
		t.Fatalf("content-type mismatch")
	}
	want := amqp.Table{"k1": "v1", "k2": "v2"}
	if !reflect.DeepEqual(pub.Headers, want) {
		t.Fatalf("headers mismatch: got %#v", pub.Headers)
	}
	if pub.DeliveryMode != amqp.Persistent {
		t.Fatalf("delivery mode not persistent")
	}
	if pub.Timestamp.IsZero() {
		t.Fatalf("timestamp must be set")
	}
}

func Test_retrieveLatestMetrics_drainsChannel(t *testing.T) {
	ch := make(chan action_kit_api.Metric, 3)
	// minimal Metric struct fields used only for identity; avoid nil panics
	ch <- action_kit_api.Metric{Timestamp: time.Now()}
	ch <- action_kit_api.Metric{Timestamp: time.Now().Add(time.Second)}
	res := retrieveLatestMetrics(ch)
	if len(res) != 2 {
		t.Fatalf("expected 2 metrics, got %d", len(res))
	}
	// channel should still be open; default case should return immediately if empty
	got := retrieveLatestMetrics(ch)
	if len(got) != 0 {
		t.Fatalf("expected no more metrics, got %d", len(got))
	}
}

func Test_stopAndCloseJobs_noRace(t *testing.T) {
	// Regression test: stop() must wait for the ticker goroutine to exit
	// before closing the jobs channel, otherwise we get "send on closed channel" panic.
	// Run with -race and -count=100 for best coverage.
	for i := 0; i < 50; i++ {
		erd := &ExecutionRunData{
			stopTicker: make(chan bool),
			jobs:       make(chan time.Time, 1),
			tickers:    time.NewTicker(1 * time.Millisecond), // very fast to maximise race window
			metrics:    make(chan action_kit_api.Metric, 10),
			tickerDone: make(chan struct{}),
		}

		// Start the ticker goroutine exactly as start() does
		go func() {
			defer close(erd.tickerDone)
			for {
				select {
				case <-erd.stopTicker:
					return
				case t := <-erd.tickers.C:
					select {
					case erd.jobs <- t:
					case <-erd.stopTicker:
						return
					}
				}
			}
		}()

		// Drain jobs in background to prevent the goroutine from blocking
		drain := make(chan struct{})
		go func() {
			defer close(drain)
			for range erd.jobs {
			}
		}()

		// Let ticks accumulate briefly
		time.Sleep(5 * time.Millisecond)

		// Reproduce the stop() sequence: stopTickers -> wait for goroutine -> close(jobs)
		stopTickers(erd)
		<-erd.tickerDone
		close(erd.jobs)

		<-drain
	}
}

func Test_tickerGoroutine_doesNotBlockOnFullJobsChannel(t *testing.T) {
	// Regression test: if no workers are consuming jobs (e.g. all died from
	// connection failures), the ticker goroutine must not block forever on
	// jobs <- t. The nested select on stopTicker must allow it to exit.
	for i := 0; i < 50; i++ {
		erd := &ExecutionRunData{
			stopTicker: make(chan bool),
			jobs:       make(chan time.Time, 1), // buffer of 1
			tickers:    time.NewTicker(1 * time.Millisecond),
			metrics:    make(chan action_kit_api.Metric, 10),
			tickerDone: make(chan struct{}),
		}

		// Start ticker goroutine — nobody consumes from jobs
		go func() {
			defer close(erd.tickerDone)
			for {
				select {
				case <-erd.stopTicker:
					return
				case t := <-erd.tickers.C:
					select {
					case erd.jobs <- t:
					case <-erd.stopTicker:
						return
					}
				}
			}
		}()

		// Let ticks fill the buffer and block
		time.Sleep(5 * time.Millisecond)

		// Stop must unblock the goroutine even though nobody reads jobs
		stopTickers(erd)

		select {
		case <-erd.tickerDone:
			// success — goroutine exited
		case <-time.After(2 * time.Second):
			t.Fatal("ticker goroutine did not exit within 2s — deadlock")
		}

		close(erd.jobs)
	}
}

func Test_firstSendSelect_unblockOnStop(t *testing.T) {
	// Unit test for the select pattern used in start() for the first message send.
	// If the jobs channel is full and stopTicker is closed, the send must not block.
	for i := 0; i < 50; i++ {
		jobs := make(chan time.Time) // unbuffered — send will block
		stopCh := make(chan bool)

		done := make(chan struct{})
		go func() {
			defer close(done)
			select {
			case jobs <- time.Now():
			case <-stopCh:
				return
			}
		}()

		// Give the goroutine time to block on the select
		time.Sleep(1 * time.Millisecond)

		close(stopCh)

		select {
		case <-done:
			// success
		case <-time.After(2 * time.Second):
			t.Fatal("first send select did not unblock via stopTicker — deadlock")
		}
	}
}

func Test_stop_completesWhenNoWorkersAlive(t *testing.T) {
	// End-to-end test: simulate the full prepare → start → stop lifecycle
	// with no AMQP workers consuming from jobs.
	id := uuid.New()
	state := &PublishMessageAttackState{
		ExecutionID:              id,
		DelayBetweenRequestsInMS: 5,
		MaxConcurrent:            2,
		SuccessRate:              0, // don't fail on success rate
	}

	// Set up execution run data (normally done by prepare/initExecutionRunData)
	erd := &ExecutionRunData{
		stopTicker: make(chan bool),
		jobs:       make(chan time.Time, state.MaxConcurrent),
		metrics:    make(chan action_kit_api.Metric, state.MaxConcurrent),
	}
	ExecutionRunDataMap.Store(id, erd)

	// Drain jobs in background to simulate workers that started but then died
	// after consuming the initial job — this lets start() proceed past the first send.
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		for range erd.jobs {
		}
	}()

	// Start the ticker
	start(state)

	// Let some ticks fire
	time.Sleep(20 * time.Millisecond)

	// Call stop — must not hang
	done := make(chan struct{})
	var result *action_kit_api.StopResult
	var stopErr error
	go func() {
		defer close(done)
		result, stopErr = stop(state)
	}()

	select {
	case <-done:
		// success
	case <-time.After(5 * time.Second):
		t.Fatal("stop() did not complete within 5s — deadlock")
	}

	<-drainDone

	if stopErr != nil {
		t.Fatalf("stop returned error: %v", stopErr)
	}
	if result != nil && result.Error != nil {
		// With SuccessRate=0, any success rate is acceptable
		t.Fatalf("unexpected error in stop result: %v", result.Error.Title)
	}

	// Verify execution data was cleaned up
	if _, ok := ExecutionRunDataMap.Load(id); ok {
		t.Fatal("execution run data should have been deleted after stop")
	}
}

func Test_stop_calledTwiceDoesNotPanic(t *testing.T) {
	// Calling stop() twice should be safe — the second call should return nil
	// because the execution data was already deleted.
	id := uuid.New()
	state := &PublishMessageAttackState{
		ExecutionID:              id,
		DelayBetweenRequestsInMS: 50,
		MaxConcurrent:            1,
		SuccessRate:              0,
	}

	erd := &ExecutionRunData{
		stopTicker: make(chan bool),
		jobs:       make(chan time.Time, 1),
		metrics:    make(chan action_kit_api.Metric, 1),
	}
	ExecutionRunDataMap.Store(id, erd)

	// Drain jobs
	go func() {
		for range erd.jobs {
		}
	}()

	start(state)
	time.Sleep(10 * time.Millisecond)

	// First stop
	_, err := stop(state)
	if err != nil {
		t.Fatalf("first stop returned error: %v", err)
	}

	// Second stop — should return nil, nil (not found)
	result, err := stop(state)
	if err != nil {
		t.Fatalf("second stop returned error: %v", err)
	}
	if result != nil {
		t.Fatal("second stop should return nil result")
	}
}

func Test_credentialsNotInJSON(t *testing.T) {
	state := PublishMessageAttackState{
		Queue:                  "test-queue",
		AmqpURL:                "amqp://user:secret@host:5672/vhost", //NOSONAR go:S2068
		AmqpUser:               "user",
		AmqpPassword:           "secret",
		AmqpInsecureSkipVerify: true,
		AmqpCA:                 "/path/to/ca.pem",
	}

	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	s := string(data)
	for _, sensitive := range []string{"AmqpURL", "AmqpUser", "AmqpPassword", "AmqpInsecureSkipVerify", "AmqpCA", "secret", "amqp://user"} {
		if strings.Contains(s, sensitive) {
			t.Errorf("JSON contains sensitive field %q: %s", sensitive, s)
		}
	}

	// Non-sensitive fields should still be present
	if !strings.Contains(s, "test-queue") {
		t.Errorf("JSON missing expected field Queue: %s", s)
	}
}

func Test_stopTickers_stopsAndSignalsOnce(t *testing.T) {
	erd := &ExecutionRunData{
		tickers:    time.NewTicker(time.Hour),
		stopTicker: make(chan bool),
	}

	// First stop: should close channel
	stopTickers(erd)

	select {
	case _, ok := <-erd.stopTicker:
		if ok {
			t.Fatalf("expected closed channel (ok=false), got ok=true")
		}
	default:
		t.Fatalf("expected stopTicker to be closed and immediately readable")
	}

	// Second stop: should be a no-op (no panic)
	stopTickers(erd)
}
