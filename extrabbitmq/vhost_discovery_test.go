// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package extrabbitmq

import (
	"reflect"
	"testing"

	"github.com/steadybit/discovery-kit/go/discovery_kit_api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	rabbithole "github.com/michaelklishin/rabbit-hole/v3"
)

func TestDescribeVhost(t *testing.T) {
	desc := (&rabbitVhostDiscovery{}).Describe()
	require.Equal(t, vhostTargetId, desc.Id)
	require.NotNil(t, desc.Discover.CallInterval)
}

func TestDescribeTargetVhost(t *testing.T) {
	d := &rabbitVhostDiscovery{}
	td := d.DescribeTarget()

	require.Equal(t, vhostTargetId, td.Id)
	require.Equal(t, "RabbitMQ vhost", td.Label.One)
	require.Equal(t, "RabbitMQ vhosts", td.Label.Other)
	require.NotNil(t, td.Category)
	require.Equal(t, "rabbitmq", *td.Category)

	require.Len(t, td.Table.Columns, 4)
	require.Len(t, td.Table.OrderBy, 1)

	ob := td.Table.OrderBy[0]
	require.Equal(t, "steadybit.label", ob.Attribute)
	require.Equal(t, discovery_kit_api.OrderByDirection("ASC"), ob.Direction)
}

func TestDescribeAttributesVhost(t *testing.T) {
	attrs := (&rabbitVhostDiscovery{}).DescribeAttributes()
	expected := []string{
		"rabbitmq.vhost.name",
		"rabbitmq.vhost.tracing",
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

func TestToVhostTarget(t *testing.T) {
	vh := rabbithole.VhostInfo{
		Name:    "my-vhost",
		Tracing: true,
	}
	cluster := "cluster-a"
	mgmtURL := "http://rabbit.example:15672"

	tgt := toVhostTarget(mgmtURL, vh, cluster)

	// Basic fields
	assert.Equal(t, mgmtURL+"::"+vh.Name, tgt.Id)
	assert.Equal(t, "my-vhost", tgt.Label)
	assert.Equal(t, vhostTargetId, tgt.TargetType)

	// Attributes
	attr := tgt.Attributes
	check := func(key string, want []string) {
		v, ok := attr[key]
		assert.True(t, ok, "missing attribute %q", key)
		assert.True(t, reflect.DeepEqual(v, want), "%s = %v; want %v", key, v, want)
	}

	check("rabbitmq.vhost.name", []string{"my-vhost"})
	check("rabbitmq.vhost.tracing", []string{"true"})
	check("rabbitmq.cluster.name", []string{cluster})
}
