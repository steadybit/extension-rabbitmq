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

func TestDescribeNode(t *testing.T) {
	desc := (&rabbitNodeDiscovery{}).Describe()
	require.Equal(t, nodeTargetId, desc.Id)
	require.NotNil(t, desc.Discover.CallInterval)
}

func TestDescribeTargetNode(t *testing.T) {
	d := &rabbitNodeDiscovery{}
	td := d.DescribeTarget()

	require.Equal(t, nodeTargetId, td.Id)
	require.Equal(t, "RabbitMQ node", td.Label.One)
	require.Equal(t, "RabbitMQ nodes", td.Label.Other)
	require.NotNil(t, td.Category)
	require.Equal(t, "rabbitmq", *td.Category)
	require.Len(t, td.Table.Columns, 5)
	require.Len(t, td.Table.OrderBy, 1)

	ob := td.Table.OrderBy[0]
	require.Equal(t, "steadybit.label", ob.Attribute)
	require.Equal(t, discovery_kit_api.OrderByDirection("ASC"), ob.Direction)

	// Verify expected columns in order
	wantCols := []string{
		"steadybit.label",
		"rabbitmq.node.name",
		"rabbitmq.node.type",
		"rabbitmq.node.running",
		"rabbitmq.cluster.name",
	}
	for i, c := range td.Table.Columns {
		require.Equalf(t, wantCols[i], c.Attribute, "column %d", i)
	}
}

func TestDescribeAttributesNode(t *testing.T) {
	attrs := (&rabbitNodeDiscovery{}).DescribeAttributes()
	expected := []string{
		"rabbitmq.node.name",
		"rabbitmq.node.type",
		"rabbitmq.node.running",
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

func TestToNodeTarget(t *testing.T) {
	n := rabbithole.NodeInfo{
		Name:      "rabbit@node-1",
		NodeType:  "disc",
		IsRunning: true,
	}
	cluster := "cluster-1"
	mgmtURL := "http://rabbit.example:15672"

	tgt := toNodeTarget(mgmtURL, n, cluster)

	// Basic fields
	assert.Equal(t, mgmtURL+"::"+n.Name, tgt.Id)
	assert.Equal(t, "rabbit@node-1", tgt.Label)
	assert.Equal(t, nodeTargetId, tgt.TargetType)

	// Attributes
	attr := tgt.Attributes
	check := func(key string, want []string) {
		v, ok := attr[key]
		assert.True(t, ok, "missing attribute %q", key)
		assert.True(t, reflect.DeepEqual(v, want), "%s = %v; want %v", key, v, want)
	}

	check("rabbitmq.node.name", []string{"rabbit@node-1"})
	check("rabbitmq.node.type", []string{"disc"})
	check("rabbitmq.node.running", []string{"true"})
	check("rabbitmq.cluster.name", []string{cluster})
}
