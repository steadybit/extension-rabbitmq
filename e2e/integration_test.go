// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package e2e

import (
	"context"
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
	"github.com/stretchr/testify/require"
)

func TestWithMinikube(t *testing.T) {
	// Use Bitnami RabbitMQ (non-TLS) for e2e. We pass the extension its endpoints via Helm env.
	extFactory := e2e.HelmExtensionFactory{
		Name: "extension-rabbitmq",
		Port: 8083,
		ExtraArgs: func(m *e2e.Minikube) []string {
			endpointsJSON := `[{"url":"http://my-rabbitmq.default.svc.cluster.local:15672","username":"user","password":"bitnami","amqp":{"url":"amqp://my-rabbitmq.default.svc.cluster.local:5672/","vhost":"/"}}]`
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
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	target, err := e2e.PollForTarget(ctx, e, "com.steadybit.extension_rabbitmq.vhost", func(t discovery_kit_api.Target) bool {
		return len(t.Attributes["rabbitmq.vhost.name"]) > 0
	})
	require.NoError(t, err)
	assert.Equal(t, "com.steadybit.extension_rabbitmq.vhost", target.TargetType)
	assert.NotEmpty(t, target.Attributes["rabbitmq.vhost.name"])
	assert.NotEmpty(t, target.Attributes["rabbitmq.cluster.name"])
}

func testDiscoverQueues(t *testing.T, _ *e2e.Minikube, e *e2e.Extension) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	target, err := e2e.PollForTarget(ctx, e, "com.steadybit.extension_rabbitmq.queue", func(t discovery_kit_api.Target) bool {
		return len(t.Attributes["rabbitmq.queue.name"]) > 0
	})
	require.NoError(t, err)
	assert.Equal(t, "com.steadybit.extension_rabbitmq.queue", target.TargetType)
	assert.NotEmpty(t, target.Attributes["rabbitmq.queue.vhost"])
	assert.NotEmpty(t, target.Attributes["rabbitmq.queue.name"])
	assert.NotEmpty(t, target.Attributes["rabbitmq.mgmt.url"])
	assert.NotEmpty(t, target.Attributes["rabbitmq.amqp.url"])
}

func testDiscoverNodes(t *testing.T, _ *e2e.Minikube, e *e2e.Extension) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	target, err := e2e.PollForTarget(ctx, e, "com.steadybit.extension_rabbitmq.node", func(t discovery_kit_api.Target) bool {
		return len(t.Attributes["rabbitmq.node.name"]) > 0
	})
	require.NoError(t, err)
	assert.Equal(t, "com.steadybit.extension_rabbitmq.node", target.TargetType)
	assert.NotEmpty(t, target.Attributes["rabbitmq.node.running"])
}

func helmInstallRabbitMQ(minikube *e2e.Minikube) error {
	if out, err := exec.Command("helm", "repo", "add", "bitnami", "https://charts.bitnami.com/bitnami").CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add repo: %s: %s", err, out)
	}
	// Single replica, default vhost, user/password, service ClusterIP.
	// Management 15672, AMQP 5672.
	args := []string{
		"upgrade", "--install",
		"--kube-context", minikube.Profile,
		"--namespace", "default",
		"--create-namespace",
		"my-rabbitmq", "bitnami/rabbitmq",
		"--set", "auth.username=user",
		"--set", "auth.password=bitnami",
		"--set", "metrics.enabled=true",
		"--set", "image.repository=bitnamilegacy/rabbitmq",
		"--set", "image.tag=4.1.3-debian-12-r0",
		"--set", "global.security.allowInsecureImages=true",
		"--wait",
		"--timeout=10m0s",
	}
	if out, err := exec.Command("helm", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to install rabbitmq chart: %s: %s", err, string(out))
	}

	// Optionally wait for management to be ready by probing the service DNS from within the cluster,
	// but the Helm --wait is typically enough for the statefulset and service readiness.
	_ = os.Setenv("RABBITMQ_SERVICE", "my-rabbitmq.default.svc.cluster.local")
	// Create vhost and queue via management API from inside the pod
	if err := ensureRabbitMQTopology(minikube, "default", "my-rabbitmq-0", "user", "bitnami", "order", "order"); err != nil {
		return fmt.Errorf("failed to create vhost/queue: %w", err)
	}
	return nil
}

func ensureRabbitMQTopology(minikube *e2e.Minikube, ns, pod, user, pass, vhost, queue string) error {
	// Retry loop because management may need a few seconds even after --wait
	deadline := time.Now().Add(2 * time.Minute)
	for {
		cmd := exec.Command(
			"kubectl", "--context", minikube.Profile, "-n", ns, "exec", pod, "--", "bash", "-ceu",
			fmt.Sprintf(`
set -o pipefail
curl -fsS -u %s:%s -H 'content-type: application/json' -X PUT http://localhost:15672/api/vhosts/%s >/dev/null
curl -fsS -u %s:%s -H 'content-type: application/json' -X PUT http://localhost:15672/api/permissions/%s/%s -d '{"configure":".*","write":".*","read":".*"}' >/dev/null
curl -fsS -u %s:%s -H 'content-type: application/json' -X PUT http://localhost:15672/api/queues/%s/%s -d '{"durable":true}' >/dev/null
`, user, pass, vhost, user, pass, vhost, user, user, pass, vhost, queue),
		)
		out, err := cmd.CombinedOutput()
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("kubectl exec failed: %v: %s", err, string(out))
		}
		time.Sleep(5 * time.Second)
	}
}
