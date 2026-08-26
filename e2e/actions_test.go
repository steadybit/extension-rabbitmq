// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2026 Steadybit GmbH

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	rabbithole "github.com/michaelklishin/rabbit-hole/v3"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/action-kit/go/action_kit_test/e2e"
	"github.com/steadybit/discovery-kit/go/discovery_kit_api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	nodeCheckActionId            = "com.steadybit.extension_rabbitmq.node.check"
	queueBacklogCheckActionId    = "com.steadybit.extension_rabbitmq.queue.check-backlog"
	alterMaxLengthActionId       = "com.steadybit.extension_rabbitmq.queue.alter-max-length"
	queuePublishFixedActionId    = "com.steadybit.extension_rabbitmq.queue.publish-fixed-amount"
	queuePublishPeriodicActionId = "com.steadybit.extension_rabbitmq.queue.publish-periodically"
	exchangePublishFixedActionId = "com.steadybit.extension_rabbitmq.exchange.publish-fixed-amount"
	stateCheckModeAllTheTime     = "all-time"
)

// ---------- node check ----------

func testCheckNodeReportsNoChanges(t *testing.T, _ *e2e.Minikube, e *e2e.Extension) {
	target := pollNodeTarget(t, e)

	config := struct {
		Duration        int    `json:"duration"`
		ChangeCheckMode string `json:"changeCheckMode"`
		FailEarly       bool   `json:"failEarly"`
	}{Duration: 3000, ChangeCheckMode: stateCheckModeAllTheTime, FailEarly: true}

	action, err := e.RunAction(nodeCheckActionId, target, config, &action_kit_api.ExecutionContext{})
	require.NoError(t, err)
	defer func() { _ = action.Cancel() }()

	require.NoError(t, action.Wait(), "a healthy node must not report a change")

	metric := findMetric(action.Metrics(), "rabbit_node_state")
	require.NotNil(t, metric, "the node check must emit rabbit_node_state metrics")
	assert.Equal(t, "info", metric.Metric["state"])
	assert.Equal(t, "No changes", metric.Metric["tooltip"])
}

func testCheckNodeFailsWithoutExpectedChange(t *testing.T, _ *e2e.Minikube, e *e2e.Extension) {
	target := pollNodeTarget(t, e)

	config := struct {
		Duration        int      `json:"duration"`
		ExpectedChanges []string `json:"expectedChanges"`
		ChangeCheckMode string   `json:"changeCheckMode"`
		FailEarly       bool     `json:"failEarly"`
	}{Duration: 3000, ExpectedChanges: []string{"Node down"}, ChangeCheckMode: stateCheckModeAllTheTime, FailEarly: true}

	action, err := e.RunAction(nodeCheckActionId, target, config, &action_kit_api.ExecutionContext{})
	require.NoError(t, err)
	defer func() { _ = action.Cancel() }()

	err = action.Wait()
	require.Error(t, err, "the check must fail when the expected node change never happens")
	assert.Contains(t, err.Error(), "didn't get the expected changes")
}

// ---------- queue backlog check ----------

func testCheckQueueBacklogBelowThreshold(t *testing.T, m *e2e.Minikube, e *e2e.Extension) {
	target := pollQueueTarget(t, e)

	backlog, err := queueMessageCount(m)
	require.NoError(t, err)

	config := struct {
		Duration          int  `json:"duration"`
		AcceptableBacklog int  `json:"acceptableBacklog"`
		FailEarly         bool `json:"failEarly"`
	}{Duration: 3000, AcceptableBacklog: backlog + 1000, FailEarly: true}

	action, err := e.RunAction(queueBacklogCheckActionId, target, config, &action_kit_api.ExecutionContext{})
	require.NoError(t, err)
	defer func() { _ = action.Cancel() }()

	require.NoError(t, action.Wait())

	metric := findMetric(action.Metrics(), "rabbitmq_queue_backlog")
	require.NotNil(t, metric, "the backlog check must emit rabbitmq_queue_backlog metrics")
	assert.Equal(t, "true", metric.Metric["backlog_constraints_fulfilled"])
	assert.Equal(t, rabbitQueue, metric.Metric["queue"])
	assert.Equal(t, rabbitVhost, metric.Metric["vhost"])
}

func testCheckQueueBacklogAboveThreshold(t *testing.T, m *e2e.Minikube, e *e2e.Extension) {
	target := pollQueueTarget(t, e)

	// The publish tests before this one left messages on the queue, so any backlog exceeds 0.
	requireQueueNotEmpty(t, m)

	config := struct {
		Duration          int  `json:"duration"`
		AcceptableBacklog int  `json:"acceptableBacklog"`
		FailEarly         bool `json:"failEarly"`
	}{Duration: 3000, AcceptableBacklog: 0, FailEarly: false}

	// Without fail early the check keeps polling for the whole duration and reports the breach
	// once the step ends.
	action, err := e.RunAction(queueBacklogCheckActionId, target, config, &action_kit_api.ExecutionContext{})
	require.NoError(t, err)
	defer func() { _ = action.Cancel() }()

	err = action.Wait()
	require.Error(t, err, "the check must fail when the backlog exceeded the threshold")
	assert.Contains(t, err.Error(), "Queue backlog exceeded threshold 0 at least once")

	metric := findMetric(action.Metrics(), "rabbitmq_queue_backlog")
	require.NotNil(t, metric)
	assert.Equal(t, "false", metric.Metric["backlog_constraints_fulfilled"])
	assert.Positive(t, metric.Value)
}

func testCheckQueueBacklogFailsEarly(t *testing.T, m *e2e.Minikube, e *e2e.Extension) {
	target := pollQueueTarget(t, e)

	requireQueueNotEmpty(t, m)

	config := struct {
		Duration          int  `json:"duration"`
		AcceptableBacklog int  `json:"acceptableBacklog"`
		FailEarly         bool `json:"failEarly"`
	}{Duration: 30000, AcceptableBacklog: 0, FailEarly: true}

	// With fail early the breach is already detected while the action starts, so the failure
	// surfaces on the start call instead of during the status polling.
	_, err := e.RunAction(queueBacklogCheckActionId, target, config, &action_kit_api.ExecutionContext{})
	require.Error(t, err, "the check must fail as soon as the backlog exceeds the threshold")
	assert.Contains(t, err.Error(), "Queue backlog exceeded threshold 0")
}

// ---------- alter queue max length ----------

func testAlterQueueMaxLength(t *testing.T, m *e2e.Minikube, e *e2e.Extension) {
	target := pollQueueTarget(t, e)

	config := struct {
		Duration  int `json:"duration"`
		MaxLength int `json:"maxLength"`
	}{Duration: 5000, MaxLength: 5}

	action, err := e.RunAction(alterMaxLengthActionId, target, config, &action_kit_api.ExecutionContext{})
	require.NoError(t, err)
	defer func() { _ = action.Cancel() }()

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		policy, err := findAlterMaxLengthPolicy(m)
		assert.NoError(c, err)
		if assert.NotNil(c, policy, "the attack must create a max-length policy") {
			assert.Equal(c, fmt.Sprintf("^%s$", rabbitQueue), policy.Pattern)
			assert.Equal(c, "queues", policy.ApplyTo)
			assert.EqualValues(c, 5, policy.Definition["max-length"])
		}
	}, 20*time.Second, 500*time.Millisecond)

	require.NoError(t, action.Wait())
	assert.NotEmpty(t, action.Messages(), "the attack must report the created policy")

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		policy, err := findAlterMaxLengthPolicy(m)
		assert.NoError(c, err)
		assert.Nil(c, policy, "the policy must be removed when the attack ends")
	}, 20*time.Second, 500*time.Millisecond)
}

// ---------- publish ----------

func testPublishFixedAmountToQueue(t *testing.T, m *e2e.Minikube, e *e2e.Extension) {
	target := pollQueueTarget(t, e)

	before, err := queueMessageCount(m)
	require.NoError(t, err)

	config := struct {
		NumberOfMessages int    `json:"numberOfMessages"`
		Duration         int    `json:"duration"`
		Body             string `json:"body"`
		SuccessRate      int    `json:"successRate"`
		MaxConcurrent    int    `json:"maxConcurrent"`
	}{NumberOfMessages: 20, Duration: 3, Body: "e2e-queue", SuccessRate: 100, MaxConcurrent: 2}

	action, err := e.RunAction(queuePublishFixedActionId, target, config, &action_kit_api.ExecutionContext{})
	require.NoError(t, err)
	defer func() { _ = action.Cancel() }()

	require.NoError(t, action.Wait(), "all messages must be published successfully")
	requireQueueGrewBy(t, m, before, 20)
}

func testPublishPeriodicallyToQueue(t *testing.T, m *e2e.Minikube, e *e2e.Extension) {
	target := pollQueueTarget(t, e)

	before, err := queueMessageCount(m)
	require.NoError(t, err)

	// External time control: the action_kit_test client ends the run and reads "duration" from the
	// config as milliseconds, whatever the action declares. Hence 4000 here and seconds elsewhere.
	config := struct {
		MessagesPerSecond int    `json:"messagesPerSecond"`
		Duration          int    `json:"duration"`
		Body              string `json:"body"`
		SuccessRate       int    `json:"successRate"`
		MaxConcurrent     int    `json:"maxConcurrent"`
	}{MessagesPerSecond: 5, Duration: 4000, Body: "e2e-periodic", SuccessRate: 100, MaxConcurrent: 1}

	action, err := e.RunAction(queuePublishPeriodicActionId, target, config, &action_kit_api.ExecutionContext{})
	require.NoError(t, err)
	defer func() { _ = action.Cancel() }()

	require.NoError(t, action.Wait(), "all messages must be published successfully")
	// 5 messages/s for ~4s, asserted conservatively to stay robust on a loaded CI runner.
	requireQueueGrewBy(t, m, before, 10)
}

func testPublishFixedAmountToExchange(t *testing.T, m *e2e.Minikube, e *e2e.Extension) {
	target := pollExchangeTarget(t, e)

	before, err := queueMessageCount(m)
	require.NoError(t, err)

	// The exchange is bound to the queue with a catch-all routing key, so the messages are routable
	// and land on the queue. Unroutable messages would be returned by the broker and counted as
	// failures against the required success rate.
	config := struct {
		NumberOfMessages int    `json:"numberOfMessages"`
		Duration         int    `json:"duration"`
		RoutingKey       string `json:"routingKey"`
		Body             string `json:"body"`
		SuccessRate      int    `json:"successRate"`
		MaxConcurrent    int    `json:"maxConcurrent"`
	}{NumberOfMessages: 10, Duration: 2, RoutingKey: "e2e.key", Body: "e2e-exchange", SuccessRate: 100, MaxConcurrent: 1}

	action, err := e.RunAction(exchangePublishFixedActionId, target, config, &action_kit_api.ExecutionContext{})
	require.NoError(t, err)
	defer func() { _ = action.Cancel() }()

	require.NoError(t, action.Wait(), "all messages must be published successfully")
	requireQueueGrewBy(t, m, before, 10)
}

// ---------- helpers ----------

func pollQueueTarget(t *testing.T, e *e2e.Extension) *action_kit_api.Target {
	return toActionTarget(pollTarget(t, e, "com.steadybit.extension_rabbitmq.queue", func(target discovery_kit_api.Target) bool {
		return e2e.HasAttribute(target, "rabbitmq.queue.name", rabbitQueue) &&
			e2e.HasAttribute(target, "rabbitmq.queue.vhost", rabbitVhost)
	}))
}

func pollExchangeTarget(t *testing.T, e *e2e.Extension) *action_kit_api.Target {
	return toActionTarget(pollTarget(t, e, "com.steadybit.extension_rabbitmq.exchange", func(target discovery_kit_api.Target) bool {
		return e2e.HasAttribute(target, "rabbitmq.exchange.name", rabbitExchange) &&
			e2e.HasAttribute(target, "rabbitmq.exchange.vhost", rabbitVhost)
	}))
}

func pollNodeTarget(t *testing.T, e *e2e.Extension) *action_kit_api.Target {
	return toActionTarget(pollTarget(t, e, "com.steadybit.extension_rabbitmq.node", func(target discovery_kit_api.Target) bool {
		return e2e.HasAttribute(target, "rabbitmq.node.running", "true")
	}))
}

// pollTarget waits until the discovery reports a target matching the predicate.
func pollTarget(t *testing.T, e *e2e.Extension, targetType string, predicate func(discovery_kit_api.Target) bool) discovery_kit_api.Target {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	target, err := e2e.PollForTarget(ctx, e, targetType, predicate)
	require.NoError(t, err, "no %s target discovered", targetType)
	return target
}

// toActionTarget converts a discovered target into the shape the action endpoints expect.
func toActionTarget(discovered discovery_kit_api.Target) *action_kit_api.Target {
	return &action_kit_api.Target{Name: discovered.Label, Attributes: discovered.Attributes}
}

func findMetric(metrics []action_kit_api.Metric, name string) *action_kit_api.Metric {
	for i := range metrics {
		if metrics[i].Name != nil && *metrics[i].Name == name {
			return &metrics[i]
		}
	}
	return nil
}

func requireQueueNotEmpty(t *testing.T, m *e2e.Minikube) {
	t.Helper()
	count, err := queueMessageCount(m)
	require.NoError(t, err)
	require.Positive(t, count, "the publish tests must have left messages on the queue")
}

func requireQueueGrewBy(t *testing.T, m *e2e.Minikube, before, published int) {
	t.Helper()
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		count, err := queueMessageCount(m)
		assert.NoError(c, err)
		assert.GreaterOrEqual(c, count, before+published)
	}, 30*time.Second, time.Second, "the published messages must arrive on the queue")
}

func findAlterMaxLengthPolicy(m *e2e.Minikube) (*rabbithole.Policy, error) {
	body, err := rabbitMgmtGET(m, "/api/policies/"+rabbitVhost)
	if err != nil {
		return nil, err
	}
	var policies []rabbithole.Policy
	if err := json.Unmarshal([]byte(body), &policies); err != nil {
		return nil, fmt.Errorf("failed to parse policies %q: %w", body, err)
	}
	for i := range policies {
		if strings.HasPrefix(policies[i].Name, "steadybit-alter-maxlen-") {
			return &policies[i], nil
		}
	}
	return nil, nil
}

func queueMessageCount(m *e2e.Minikube) (int, error) {
	body, err := rabbitMgmtGET(m, fmt.Sprintf("/api/queues/%s/%s", rabbitVhost, rabbitQueue))
	if err != nil {
		return 0, err
	}
	var queue rabbithole.QueueInfo
	if err := json.Unmarshal([]byte(body), &queue); err != nil {
		return 0, fmt.Errorf("failed to parse queue %q: %w", body, err)
	}
	return queue.Messages, nil
}

// rabbitMgmtGET queries the RabbitMQ management API from inside the broker pod. Going through the
// pod keeps the assertions independent of the extension under test and avoids a port-forward for
// the management port.
func rabbitMgmtGET(m *e2e.Minikube, path string) (string, error) {
	out, err := rabbitMgmtExec(m, fmt.Sprintf("curl -fsS -u %s:%s http://localhost:15672%s", rabbitUser, rabbitPassword, path))
	if err != nil {
		return "", fmt.Errorf("GET %s failed: %w", path, err)
	}
	return out, nil
}
