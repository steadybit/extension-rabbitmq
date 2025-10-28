// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package extrabbitmq

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/steadybit/discovery-kit/go/discovery_kit_api"
	"github.com/steadybit/extension-rabbitmq/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetEndpointsJSON(t *testing.T) {
	eps := []config.ManagementEndpoint{
		{
			URL:      "http://localhost:15672",
			Username: "user",
			Password: "pass",
			AMQP: &config.AMQPOptions{
				URL:                "amqp://localhost:5672",
				Vhost:              "/",
				InsecureSkipVerify: false,
			},
		},
	}

	setEndpointsJSON(eps)
	assert.NotEmpty(t, config.Config.ManagementEndpointsJSON)

	var parsed []config.ManagementEndpoint
	err := json.Unmarshal([]byte(config.Config.ManagementEndpointsJSON), &parsed)
	require.NoError(t, err)
	require.Len(t, parsed, 1)
	assert.Equal(t, "http://localhost:15672", parsed[0].URL)
	assert.Equal(t, "amqp://localhost:5672", parsed[0].AMQP.URL)
}

func TestFindTargetByLabel(t *testing.T) {
	targets := []discovery_kit_api.Target{
		{Label: "queue-A"},
		{Label: "queue-B"},
	}
	tgt := findTargetByLabel(targets, "queue-B")
	require.NotNil(t, tgt)
	assert.Equal(t, "queue-B", tgt.Label)

	tgt = findTargetByLabel(targets, "non-existent")
	assert.Nil(t, tgt)
}

func TestAssertAttr(t *testing.T) {
	tgt := discovery_kit_api.Target{
		Attributes: map[string][]string{
			"rabbitmq.queue.name": {"order"},
		},
	}

	// Should not panic
	assert.NotPanics(t, func() {
		assertAttr(t, tgt, "rabbitmq.queue.name", "order")
	})
}

func TestPublishMessageAttackState_DefaultValues(t *testing.T) {
	s := PublishMessageAttackState{}
	assert.Empty(t, s.Vhost)
	assert.Empty(t, s.Queue)
	assert.Zero(t, s.DelayBetweenRequestsInMS)
	assert.Equal(t, 0, s.SuccessRate)
	assert.True(t, s.Timeout.IsZero())
	assert.Equal(t, 0, s.MaxConcurrent)
	assert.Empty(t, s.RoutingKey)
	assert.Empty(t, s.Body)
	assert.Equal(t, uint64(0), s.NumberOfMessages)
	assert.Empty(t, s.Headers)
	assert.Empty(t, s.AmqpURL)
	assert.Empty(t, s.AmqpUser)
	assert.Empty(t, s.AmqpPassword)
	assert.False(t, s.AmqpInsecureSkipVerify)
	assert.Empty(t, s.AmqpCA)
}

func TestPublishMessageAttackState_WithValues(t *testing.T) {
	headers := map[string]string{"k": "v"}
	timeout := time.Now().Add(10 * time.Minute)
	state := PublishMessageAttackState{
		Vhost:                    "/",
		Exchange:                 "ex1",
		Queue:                    "queue1",
		DelayBetweenRequestsInMS: 100,
		SuccessRate:              95,
		Timeout:                  timeout,
		MaxConcurrent:            5,
		RoutingKey:               "rk",
		Body:                     "body",
		NumberOfMessages:         10,
		Headers:                  headers,
		AmqpURL:                  "amqp://localhost:5672",
		AmqpUser:                 "user",
		AmqpPassword:             "pass",
		AmqpInsecureSkipVerify:   true,
		AmqpCA:                   "/tmp/ca.pem",
	}

	assert.Equal(t, "/", state.Vhost)
	assert.Equal(t, "queue1", state.Queue)
	assert.Equal(t, 100, int(state.DelayBetweenRequestsInMS))
	assert.Equal(t, 95, state.SuccessRate)
	assert.Equal(t, timeout, state.Timeout)
	assert.Equal(t, 5, state.MaxConcurrent)
	assert.Equal(t, "rk", state.RoutingKey)
	assert.Equal(t, "body", state.Body)
	assert.Equal(t, uint64(10), state.NumberOfMessages)
	assert.Equal(t, headers, state.Headers)
	assert.Equal(t, "amqp://localhost:5672", state.AmqpURL)
	assert.True(t, state.AmqpInsecureSkipVerify)
	assert.Equal(t, "/tmp/ca.pem", state.AmqpCA)
}
