// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package extrabbitmq

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/stretchr/testify/assert"
)

// ---- buildAMQPURL ----

func TestBuildAMQPURL_ErrorsOnEmptyOrMalformed(t *testing.T) {
	_, err := buildAMQPURL("", "/", "", "")
	assert.Error(t, err)

	_, err = buildAMQPURL("://bad", "/", "", "")
	assert.Error(t, err)
}

func TestBuildAMQPURL_RootVhost_NoCreds(t *testing.T) {
	got, err := buildAMQPURL("amqp://host:5672", "/", "", "")
	assert.NoError(t, err)
	assert.Equal(t, "amqp://host:5672/", got)
}

func TestBuildAMQPURL_CustomVhost_NoCreds(t *testing.T) {
	got, err := buildAMQPURL("amqp://host", "tenant-a", "", "")
	assert.NoError(t, err)
	assert.Equal(t, "amqp://host/tenant-a", got)
}

func TestBuildAMQPURL_CustomVhost_PathEscaped(t *testing.T) {
	got, err := buildAMQPURL("amqp://host", "team/a b", "", "")
	assert.NoError(t, err)
	assert.Equal(t, "amqp://host/team%252Fa%2520b", got)
}

func TestBuildAMQPURL_InjectsCreds_IfMissing(t *testing.T) {
	got, err := buildAMQPURL("amqp://rabbit.local", "v1", "user", "pass")
	assert.NoError(t, err)
	assert.Equal(t, "amqp://user:pass@rabbit.local/v1", got)
}

func TestBuildAMQPURL_PreservesExistingCreds(t *testing.T) {
	got, err := buildAMQPURL("amqp://alice:secret@rabbit.local:5672", "v1", "ignored", "ignored")
	assert.NoError(t, err)
	assert.Equal(t, "amqp://alice:secret@rabbit.local:5672/v1", got)
}

// ---- createPublishRequest ----

type headersState struct {
	ProduceMessageAttackState
}

func TestCreatePublishRequest_UsesQueueAsFallbackRoutingKey(t *testing.T) {
	s := &ProduceMessageAttackState{
		Exchange:   "",
		RoutingKey: "",
		Queue:      "orders",
		Body:       "payload",
	}
	ex, rk, pub := createPublishRequest(s)
	assert.Equal(t, "", ex)
	assert.Equal(t, "orders", rk)
	assert.Equal(t, "text/plain", pub.ContentType)
	assert.Equal(t, []byte("payload"), pub.Body)
	assert.NotZero(t, pub.Timestamp.UnixNano())
}

func TestCreatePublishRequest_UsesRoutingKey_AndHeaders(t *testing.T) {
	s := &ProduceMessageAttackState{
		Exchange:   "ex-a",
		RoutingKey: "rk-a",
		Queue:      "ignored",
		Body:       "x",
		Headers: map[string]string{
			"k1": "v1",
			"k2": "v2",
		},
	}
	ex, rk, pub := createPublishRequest(s)
	assert.Equal(t, "ex-a", ex)
	assert.Equal(t, "rk-a", rk)
	assert.Equal(t, "v1", pub.Headers["k1"])
	assert.Equal(t, "v2", pub.Headers["k2"])
}

// ---- retrieveLatestMetrics ----

func TestRetrieveLatestMetrics_DrainsAvailableMetrics(t *testing.T) {
	ch := make(chan action_kit_api.Metric, 3)
	ch <- action_kit_api.Metric{Metric: map[string]string{"a": "test"}}
	ch <- action_kit_api.Metric{Metric: map[string]string{"b": "test"}}
	got := retrieveLatestMetrics(ch)
	assert.Len(t, got, 2)

	close(ch)
	got2 := retrieveLatestMetrics(ch)
	assert.Len(t, got2, 0)
}

// ---- stop and counters ----

func TestStop_SuccessRateAboveThreshold(t *testing.T) {
	st := &ProduceMessageAttackState{
		ExecutionID: uuid.New(),
		SuccessRate: 40,
	}
	erd := &ExecutionRunData{}
	erd.requestCounter.Store(10)
	erd.requestSuccessCounter.Store(5)
	saveExecutionRunData(st.ExecutionID, erd)

	res, err := stop(st)
	assert.NoError(t, err)
	assert.Nil(t, res.Error)
}

func TestStop_SuccessRateBelowThreshold(t *testing.T) {
	st := &ProduceMessageAttackState{
		ExecutionID: uuid.New(),
		SuccessRate: 100,
	}
	erd := &ExecutionRunData{}
	erd.requestCounter.Store(11)
	erd.requestSuccessCounter.Store(4)
	saveExecutionRunData(st.ExecutionID, erd)

	res, err := stop(st)
	assert.NoError(t, err)
	if assert.NotNil(t, res.Error) {
		assert.Contains(t, res.Error.Title, "below 100%")
	}
}

func TestStop_GracefulWhenRunDataMissing(t *testing.T) {
	st := &ProduceMessageAttackState{
		ExecutionID: uuid.New(),
		SuccessRate: 0,
	}
	// do not save run data
	res, err := stop(st)
	assert.NoError(t, err)
	assert.Nil(t, res) // function returns nil result when data missing
}

// ---- tickers and scheduling ----

func TestStopTickers_Idempotent(t *testing.T) {
	erd := &ExecutionRunData{
		tickers:    time.NewTicker(10 * time.Millisecond),
		stopTicker: make(chan bool, 1),
	}
	stopTickers(erd)     // first stop
	stopTickers(erd)     // second stop should be non-blocking
	assert.True(t, true) // reached without deadlock
}

func TestStart_SchedulesImmediatelyAndTicks(t *testing.T) {
	// Prepare execution run data manually
	execID := uuid.New()
	state := &ProduceMessageAttackState{
		ExecutionID:              execID,
		DelayBetweenRequestsInMS: 5,
	}
	saveExecutionRunData(execID, &ExecutionRunData{
		stopTicker: make(chan bool, 1),
		jobs:       make(chan time.Time, 10),
		metrics:    make(chan action_kit_api.Metric, 1),
	})

	start(state)

	// First job is scheduled immediately
	erd, _ := ExecutionRunDataMap.Load(execID)
	run := erd.(*ExecutionRunData)

	select {
	case <-run.jobs:
		// ok
	case <-time.After(200 * time.Millisecond):
		t.Fatal("did not receive initial job")
	}

	// Stop and ensure ticker goroutine exits
	stopTickers(run)
}
