package extrabbitmq

import (
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"net/url"
	"reflect"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
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
	got, err := buildAMQPURL("amqp://x:y@host/vh", "ignored", "u", "p")
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
					erd.jobs <- t
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
