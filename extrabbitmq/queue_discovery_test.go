// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package extrabbitmq

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/steadybit/discovery-kit/go/discovery_kit_api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	rabbithole "github.com/michaelklishin/rabbit-hole/v3"
)

func TestDescribeQueue(t *testing.T) {
	desc := (&rabbitQueueDiscovery{}).Describe()
	require.Equal(t, queueTargetId, desc.Id)
	require.NotNil(t, desc.Discover.CallInterval)
}

func TestDescribeTargetQueue(t *testing.T) {
	d := &rabbitQueueDiscovery{}
	td := d.DescribeTarget()

	require.Equal(t, queueTargetId, td.Id)
	require.Equal(t, "RabbitMQ queue", td.Label.One)
	require.Equal(t, "RabbitMQ queues", td.Label.Other)
	require.NotNil(t, td.Category)
	require.Equal(t, "rabbitmq", *td.Category)
	require.Len(t, td.Table.Columns, 8)
	require.Len(t, td.Table.OrderBy, 1)

	ob := td.Table.OrderBy[0]
	require.Equal(t, "steadybit.label", ob.Attribute)
	require.Equal(t, discovery_kit_api.OrderByDirection("ASC"), ob.Direction)
}

func TestDescribeAttributesQueue(t *testing.T) {
	attrs := (&rabbitQueueDiscovery{}).DescribeAttributes()
	expected := []string{
		"rabbitmq.queue.vhost",
		"rabbitmq.queue.name",
		"rabbitmq.queue.status",
		"rabbitmq.queue.durable",
		"rabbitmq.queue.auto_delete",
		"rabbitmq.amqp.url",
		"rabbitmq.cluster.name",
	}

	require.Len(t, attrs, len(expected))
	for _, want := range expected {
		found := false
		for _, a := range attrs {
			if a.Attribute == want {
				found = true
				break
			}
		}
		assert.Truef(t, found, "DescribeAttributes() missing %q", want)
	}
}

func TestToQueueTarget(t *testing.T) {
	q := rabbithole.QueueInfo{
		Name:       "q-orders",
		Vhost:      "v1",
		Durable:    true,
		AutoDelete: false,
		Status:     "running",
	}
	cluster := "cluster-a"
	mgmtURL := "http://rabbit.example:15672"
	amqpURL := "amqp://rabbit.example:5672"

	tgt := toQueueTarget(mgmtURL, amqpURL, q, cluster)

	// Basic fields
	assert.Equal(t, fmt.Sprintf("%s::%s/%s", mgmtURL, q.Vhost, q.Name), tgt.Id)
	assert.Equal(t, "v1/q-orders", tgt.Label)
	assert.Equal(t, queueTargetId, tgt.TargetType)

	// Attributes present in target
	attr := tgt.Attributes
	check := func(key string, want []string) {
		v, ok := attr[key]
		assert.True(t, ok, "missing attribute %q", key)
		assert.True(t, reflect.DeepEqual(v, want), "%s = %v; want %v", key, v, want)
	}

	check("rabbitmq.queue.vhost", []string{"v1"})
	check("rabbitmq.queue.name", []string{"q-orders"})
	check("rabbitmq.cluster.name", []string{cluster})
	check("rabbitmq.amqp.url", []string{amqpURL})
	check("rabbitmq.queue.status", []string{"running"})
	check("rabbitmq.mgmt.url", []string{mgmtURL})
}
