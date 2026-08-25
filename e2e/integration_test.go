// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package e2e

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/steadybit/action-kit/go/action_kit_test/e2e"
	actValidate "github.com/steadybit/action-kit/go/action_kit_test/validate"
	"github.com/steadybit/discovery-kit/go/discovery_kit_api"
	discValidate "github.com/steadybit/discovery-kit/go/discovery_kit_test/validate"
	"github.com/stretchr/testify/assert"
)

// Topology created by helmInstallRabbitMQ and used by both the discovery and the action tests.
const (
	rabbitNamespace = "default"
	rabbitPod       = "my-rabbitmq-0"
	rabbitUser      = "user"
	rabbitPassword  = "bitnami" //NOSONAR go:S2068 - fixture credentials of a throwaway broker
	rabbitVhost     = "order"
	rabbitQueue     = "order"
	rabbitExchange  = "e2e.topic"
)

func TestWithMinikube(t *testing.T) {
	// Use Bitnami RabbitMQ (non-TLS) for e2e. We pass the extension its endpoints via Helm env.
	extFactory := e2e.HelmExtensionFactory{
		Name: "extension-rabbitmq",
		Port: 8083,
		ExtraArgs: func(m *e2e.Minikube) []string {
			// The AMQP credentials are configured separately from the management ones: the publish
			// actions dial AMQP with endpoint.amqp.username/password only and do not fall back to
			// the management credentials.
			endpointsJSON := fmt.Sprintf(
				`[{"url":"http://my-rabbitmq.default.svc.cluster.local:15672","username":%q,"password":%q,"amqp":{"url":"amqp://my-rabbitmq.default.svc.cluster.local:5672/","vhost":"/","username":%q,"password":%q}}]`,
				rabbitUser, rabbitPassword, rabbitUser, rabbitPassword)
			return []string{
				"--set", "logging.level=debug",
				"--set-json", "rabbitmq.auth.managementEndpoints=" + endpointsJSON,
			}
		},
	}

	e2e.WithMinikube(t,
		e2e.DefaultMinikubeOpts().AfterStart(helmInstallRabbitMQ),
		&extFactory,
		[]e2e.WithMinikubeTestCase{
			{Name: "validate discovery", Test: validateDiscovery},
			{Name: "validate actions", Test: validateActions},
			{Name: "discover vhosts", Test: testDiscoverVhosts},
			{Name: "discover queues", Test: testDiscoverQueues},
			{Name: "discover nodes", Test: testDiscoverNodes},
			{Name: "discover exchanges", Test: testDiscoverExchanges},
			// The action tests share the queue created above and run in order: the backlog check
			// expecting an empty queue must run before anything publishes into it.
			{Name: "check node reports no changes", Test: testCheckNodeReportsNoChanges},
			{Name: "check node fails without the expected change", Test: testCheckNodeFailsWithoutExpectedChange},
			{Name: "check queue backlog below threshold", Test: testCheckQueueBacklogBelowThreshold},
			{Name: "alter queue max length", Test: testAlterQueueMaxLength},
			{Name: "publish fixed amount to queue", Test: testPublishFixedAmountToQueue},
			{Name: "publish periodically to queue", Test: testPublishPeriodicallyToQueue},
			{Name: "publish fixed amount to exchange", Test: testPublishFixedAmountToExchange},
			{Name: "check queue backlog above threshold", Test: testCheckQueueBacklogAboveThreshold},
			{Name: "check queue backlog fails early", Test: testCheckQueueBacklogFailsEarly},
		},
	)
}

func validateDiscovery(t *testing.T, _ *e2e.Minikube, e *e2e.Extension) {
	assert.NoError(t, discValidate.ValidateEndpointReferences("/", e.Client))
}

func validateActions(t *testing.T, _ *e2e.Minikube, e *e2e.Extension) {
	assert.NoError(t, actValidate.ValidateEndpointReferences("/", e.Client))
}

func testDiscoverVhosts(t *testing.T, _ *e2e.Minikube, e *e2e.Extension) {
	target := pollTarget(t, e, "com.steadybit.extension_rabbitmq.vhost", func(target discovery_kit_api.Target) bool {
		return len(target.Attributes["rabbitmq.vhost.name"]) > 0
	})
	assert.Equal(t, "com.steadybit.extension_rabbitmq.vhost", target.TargetType)
	assert.NotEmpty(t, target.Attributes["rabbitmq.vhost.name"])
	assert.NotEmpty(t, target.Attributes["rabbitmq.cluster.name"])
}

func testDiscoverQueues(t *testing.T, _ *e2e.Minikube, e *e2e.Extension) {
	target := pollTarget(t, e, "com.steadybit.extension_rabbitmq.queue", func(target discovery_kit_api.Target) bool {
		return len(target.Attributes["rabbitmq.queue.name"]) > 0
	})
	assert.Equal(t, "com.steadybit.extension_rabbitmq.queue", target.TargetType)
	assert.NotEmpty(t, target.Attributes["rabbitmq.queue.vhost"])
	assert.NotEmpty(t, target.Attributes["rabbitmq.queue.name"])
	assert.NotEmpty(t, target.Attributes["rabbitmq.mgmt.url"])
	assert.NotEmpty(t, target.Attributes["rabbitmq.amqp.url"])
}

func testDiscoverNodes(t *testing.T, _ *e2e.Minikube, e *e2e.Extension) {
	target := pollTarget(t, e, "com.steadybit.extension_rabbitmq.node", func(target discovery_kit_api.Target) bool {
		return len(target.Attributes["rabbitmq.node.name"]) > 0
	})
	assert.Equal(t, "com.steadybit.extension_rabbitmq.node", target.TargetType)
	assert.NotEmpty(t, target.Attributes["rabbitmq.node.running"])
}

func testDiscoverExchanges(t *testing.T, _ *e2e.Minikube, e *e2e.Extension) {
	target := pollTarget(t, e, "com.steadybit.extension_rabbitmq.exchange", func(target discovery_kit_api.Target) bool {
		return e2e.HasAttribute(target, "rabbitmq.exchange.name", rabbitExchange)
	})
	assert.Equal(t, "com.steadybit.extension_rabbitmq.exchange", target.TargetType)
	assert.Equal(t, []string{rabbitVhost}, target.Attributes["rabbitmq.exchange.vhost"])
	assert.Equal(t, []string{"topic"}, target.Attributes["rabbitmq.exchange.type"])
	assert.NotEmpty(t, target.Attributes["rabbitmq.amqp.url"])
}

func helmInstallRabbitMQ(minikube *e2e.Minikube) error {
	if out, err := exec.Command("helm", "repo", "add", "bitnami", "https://charts.bitnami.com/bitnami").CombinedOutput(); err != nil { //NOSONAR go:S4036
		return fmt.Errorf("failed to add repo: %s: %s", err, out)
	}
	// Single replica, default vhost, user/password, service ClusterIP.
	// Management 15672, AMQP 5672.
	args := []string{
		"upgrade", "--install",
		"--kube-context", minikube.Profile,
		"--namespace", rabbitNamespace,
		"--create-namespace",
		"my-rabbitmq", "bitnami/rabbitmq",
		"--set", "auth.username=" + rabbitUser,
		"--set", "auth.password=" + rabbitPassword, //NOSONAR go:S2068
		"--set", "metrics.enabled=true",
		"--set", "image.repository=bitnamilegacy/rabbitmq",
		"--set", "image.tag=4.1.3-debian-12-r0",
		"--set", "global.security.allowInsecureImages=true",
		"--wait",
		"--timeout=10m0s",
	}
	if out, err := exec.Command("helm", args...).CombinedOutput(); err != nil { //NOSONAR go:S4036
		return fmt.Errorf("failed to install rabbitmq chart: %s: %s", err, string(out))
	}

	// Optionally wait for management to be ready by probing the service DNS from within the cluster,
	// but the Helm --wait is typically enough for the statefulset and service readiness.
	_ = os.Setenv("RABBITMQ_SERVICE", "my-rabbitmq.default.svc.cluster.local")
	// Create vhost and queue via management API from inside the pod
	if err := ensureRabbitMQTopology(minikube); err != nil {
		return fmt.Errorf("failed to create vhost/queue: %w", err)
	}
	return nil
}

// ensureRabbitMQTopology creates the vhost, queue and exchange the tests work with. The exchange is
// bound to the queue with a catch-all routing key so that messages published by the exchange-targeted
// publish actions are routable and observable on the queue.
// ensureRabbitMQTopology creates the vhost, queue and exchange the tests work with. The exchange is
// bound to the queue with a catch-all routing key so that messages published by the exchange-targeted
// publish actions are routable and observable on the queue.
func ensureRabbitMQTopology(minikube *e2e.Minikube) error {
	script := fmt.Sprintf(`
curl -fsS -u %[1]s:%[2]s -H 'content-type: application/json' -X PUT http://localhost:15672/api/vhosts/%[3]s >/dev/null
curl -fsS -u %[1]s:%[2]s -H 'content-type: application/json' -X PUT http://localhost:15672/api/permissions/%[3]s/%[1]s -d '{"configure":".*","write":".*","read":".*"}' >/dev/null
curl -fsS -u %[1]s:%[2]s -H 'content-type: application/json' -X PUT http://localhost:15672/api/queues/%[3]s/%[4]s -d '{"durable":true}' >/dev/null
curl -fsS -u %[1]s:%[2]s -H 'content-type: application/json' -X PUT http://localhost:15672/api/exchanges/%[3]s/%[5]s -d '{"type":"topic","durable":true}' >/dev/null
curl -fsS -u %[1]s:%[2]s -H 'content-type: application/json' -X POST http://localhost:15672/api/bindings/%[3]s/e/%[5]s/q/%[4]s -d '{"routing_key":"#"}' >/dev/null
`, rabbitUser, rabbitPassword, rabbitVhost, rabbitQueue, rabbitExchange)

	// Retry loop because management may need a few seconds even after --wait
	deadline := time.Now().Add(2 * time.Minute)
	for {
		_, err := rabbitMgmtExec(minikube, script)
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(5 * time.Second)
	}
}

// rabbitMgmtExec runs a shell script inside the broker pod and returns its stdout. Only stdout is
// returned: kubectl writes notices such as "Defaulted container ..." to stderr, which would
// otherwise corrupt a JSON response.
func rabbitMgmtExec(minikube *e2e.Minikube, script string) (string, error) {
	var stderr bytes.Buffer
	cmd := exec.Command( //NOSONAR go:S4036
		"kubectl", "--context", minikube.Profile, "-n", rabbitNamespace, "exec", rabbitPod, "-c", "rabbitmq", "--",
		"bash", "-ceu", "set -o pipefail\n"+script,
	)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("kubectl exec failed: %w: %s", err, stderr.String())
	}
	return string(out), nil
}
