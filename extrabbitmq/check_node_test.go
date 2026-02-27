// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package extrabbitmq

import (
	"strings"
	"testing"
	"time"

	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_toNodeChangeMetric_NoChanges(t *testing.T) {
	ts := time.Now()
	m := toNodeChangeMetric("http://mgmt", nil, nil, map[string][]string{}, ts)
	require.NotNil(t, m)

	assert.Equal(t, "rabbit_node_state", *m.Name)
	assert.Equal(t, ts, m.Timestamp)
	assert.Equal(t, float64(0), m.Value)

	require.NotNil(t, m.Metric)
	assert.Equal(t, "http://mgmt", m.Metric["url"])
	assert.Equal(t, "info", m.Metric["state"])
	assert.Equal(t, "No changes", m.Metric["tooltip"])
	assert.Equal(t, "Expected: ", m.Metric["metric.id"])
}

func Test_toNodeChangeMetric_ExpectedChangePresent_setsSuccess(t *testing.T) {
	ts := time.Now()

	// Intentionally provide keys and values unsorted to verify deterministic sorting
	changes := map[string][]string{
		NodeAlarmRaised: {"b->c", "a->b"},
		NodeDown:        {"rabbit@z", "rabbit@a"},
	}
	changeNames := []string{NodeDown, NodeAlarmRaised}
	expected := []string{NodeDown, "something-else-not-present"} // NodeDown is expected

	m := toNodeChangeMetric("https://rmq", expected, changeNames, changes, ts)
	require.NotNil(t, m)

	assert.Equal(t, "success", m.Metric["state"])
	assert.Equal(t, "https://rmq", m.Metric["url"])
	assert.Equal(t, "Expected: "+strings.Join(expected, ","), m.Metric["metric.id"])

	// Tooltip must include section headers and sorted values
	tt := m.Metric["tooltip"]
	assert.Contains(t, tt, "NODE ACTIVITY")
	// keys are sorted lexicographically; NodeAlarmRaised before NodeDown
	firstIdx := findIndex(tt, "Node alarm raised:\n")
	secondIdx := findIndex(tt, "Node down:\n")
	require.NotEqual(t, -1, firstIdx)
	require.NotEqual(t, -1, secondIdx)
	assert.Less(t, firstIdx, secondIdx, "node alarm section should appear before node section")

	// values under each key are sorted
	// For NodeAlarmRaised: a->b before b->c
	assert.True(t, indexOrder(tt, "a->b\n", "b->c\n"))
	// For NodeDown: rabbit@a before rabbit@z
	assert.True(t, indexOrder(tt, "rabbit@a\n", "rabbit@z\n"))
}

func Test_toNodeChangeMetric_UnexpectedChange_setsWarn(t *testing.T) {
	ts := time.Now()

	changes := map[string][]string{
		NodeDown: {"rabbit@a"},
	}
	changeNames := []string{NodeDown}
	expected := []string{NodeAlarmRaised} // NodeDown not expected

	m := toNodeChangeMetric("http://x", expected, changeNames, changes, ts)
	require.NotNil(t, m)
	assert.Equal(t, "warn", m.Metric["state"])
	assert.Contains(t, m.Metric["tooltip"], "NODE ACTIVITY")
	assert.Contains(t, m.Metric["tooltip"], "Node down:")
	assert.Contains(t, m.Metric["tooltip"], "rabbit@a")
}

// --- small helpers for assertions on tooltip ordering ---

func findIndex(s, sub string) int {
	return indexOf(s, sub)
}

func indexOrder(s, first, second string) bool {
	i1 := indexOf(s, first)
	i2 := indexOf(s, second)
	return i1 >= 0 && i2 >= 0 && i1 < i2
}

func indexOf(haystack, needle string) int {
	// naive search is fine for tests
	n := len(needle)
	if n == 0 {
		return 0
	}
	for i := 0; i+n <= len(haystack); i++ {
		if haystack[i:i+n] == needle {
			return i
		}
	}
	return -1
}

func Test_CheckNodesAction_Describe_Basics(t *testing.T) {
	desc := (&CheckNodesAction{}).Describe()
	assert.Equal(t, rabbitNodeTargetId+".check", desc.Id)
	assert.Equal(t, action_kit_api.Check, desc.Kind)
	require.NotNil(t, desc.Status)
	require.NotNil(t, desc.Parameters)
	require.NotNil(t, desc.TargetSelection)
}

func Test_CheckNodesAction_NewEmptyState(t *testing.T) {
	var a CheckNodesAction
	st := a.NewEmptyState()
	// zero-value sanity checks
	assert.Equal(t, st.ManagementURL, "")
	assert.False(t, st.StateCheckOnce)
	// Note: End is set during Prepare; here we only ensure type compiles and zero-value fields are present.
}
