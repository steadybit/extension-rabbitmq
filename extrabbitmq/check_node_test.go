// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package extrabbitmq

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/extension-kit/extutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_toNodeChangeMetric_NoChanges(t *testing.T) {
	ts := time.Now()
	m := toNodeChangeMetric("http://mgmt", nil, []string{NoEvents}, map[string][]string{}, ts)
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

func Test_toNodeChangeMetric_NoChanges_NoEventsExpected_setsSuccess(t *testing.T) {
	ts := time.Now()
	m := toNodeChangeMetric("http://mgmt", []string{NoEvents}, []string{NoEvents}, map[string][]string{}, ts)
	require.NotNil(t, m)
	assert.Equal(t, "success", m.Metric["state"])
	assert.Equal(t, "No changes", m.Metric["tooltip"])
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

// --- Status evaluation tests (exercising the check logic without network calls) ---

func newTestState(expected []string, mode string, end time.Time) *CheckNodesState {
	return &CheckNodesState{
		End:             end,
		ExpectedChanges: expected,
		StateCheckMode:  mode,
		NodeNames:       []string{"rabbit@node1"},
		ManagementURL:   "http://mgmt",
		BaselineRunning: map[string]bool{"rabbit@node1": true},
		BaselineAlarms:  map[string]bool{"rabbit@node1": false},
	}
}

// evaluateCheck simulates the evaluation part of Status() without needing a real RabbitMQ connection.
func evaluateCheck(state *CheckNodesState, changes map[string][]string, completed bool) *action_kit_api.ActionKitError {
	observedStates := make([]string, 0, len(changes)+1)
	for k := range changes {
		observedStates = append(observedStates, k)
	}
	if len(changes) == 0 {
		observedStates = append(observedStates, NoEvents)
	}

	var checkErr *action_kit_api.ActionKitError
	if len(state.ExpectedChanges) > 0 {
		switch state.StateCheckMode {
		case stateCheckModeAllTheTime:
			for _, s := range observedStates {
				if s == NoEvents && !slices.Contains(state.ExpectedChanges, NoEvents) {
					continue
				}
				if !slices.Contains(state.ExpectedChanges, s) {
					checkErr = &action_kit_api.ActionKitError{
						Title:  fmt.Sprintf("Unexpected '%s' — expected %v.", s, state.ExpectedChanges),
						Status: extutil.Ptr(action_kit_api.Failed),
					}
				}
			}
			if completed && checkErr == nil {
				wantsRealChanges := slices.ContainsFunc(state.ExpectedChanges, func(s string) bool { return s != NoEvents })
				sawRealChanges := slices.ContainsFunc(observedStates, func(s string) bool { return s != NoEvents })
				if wantsRealChanges && !sawRealChanges {
					checkErr = &action_kit_api.ActionKitError{
						Title:  fmt.Sprintf("Expected changes %v never occurred.", state.ExpectedChanges),
						Status: extutil.Ptr(action_kit_api.Failed),
					}
				}
			}
		case stateCheckModeAtLeastOnce:
			for _, s := range observedStates {
				if slices.Contains(state.ExpectedChanges, s) {
					state.StateCheckOnce = true
				}
			}
			if completed && !state.StateCheckOnce {
				checkErr = &action_kit_api.ActionKitError{
					Title:  fmt.Sprintf("Expected %v was never observed.", state.ExpectedChanges),
					Status: extutil.Ptr(action_kit_api.Failed),
				}
			}
		}
	}
	return checkErr
}

// All the time + No events: succeed when no events the entire time
func Test_AllTheTime_NoEvents_AllClear(t *testing.T) {
	state := newTestState([]string{NoEvents}, stateCheckModeAllTheTime, time.Now().Add(-time.Second))
	err := evaluateCheck(state, map[string][]string{}, true)
	assert.Nil(t, err)
}

// All the time + No events: fail when Node down appears
func Test_AllTheTime_NoEvents_FailOnNodeDown(t *testing.T) {
	state := newTestState([]string{NoEvents}, stateCheckModeAllTheTime, time.Now().Add(time.Minute))
	err := evaluateCheck(state, map[string][]string{NodeDown: {"rabbit@node1"}}, false)
	require.NotNil(t, err)
	assert.Contains(t, err.Title, NodeDown)
}

// At least once + No events: succeed when first tick is clear then Node down
func Test_AtLeastOnce_NoEvents_SucceedAfterNodeDown(t *testing.T) {
	state := newTestState([]string{NoEvents}, stateCheckModeAtLeastOnce, time.Now().Add(time.Minute))

	// tick 1: no events
	err := evaluateCheck(state, map[string][]string{}, false)
	assert.Nil(t, err)
	assert.True(t, state.StateCheckOnce)

	// tick 2: Node down — still OK because we saw NoEvents once
	err = evaluateCheck(state, map[string][]string{NodeDown: {"rabbit@node1"}}, true)
	assert.Nil(t, err)
}

// At least once + No events: succeed through NoEvents -> alarm -> NodeDown sequence
func Test_AtLeastOnce_NoEvents_SucceedThroughMultipleChanges(t *testing.T) {
	state := newTestState([]string{NoEvents}, stateCheckModeAtLeastOnce, time.Now().Add(time.Minute))

	evaluateCheck(state, map[string][]string{}, false) // tick 1: no events
	assert.True(t, state.StateCheckOnce)

	evaluateCheck(state, map[string][]string{NodeAlarmRaised: {"rabbit@node1: mem_alarm"}}, false) // tick 2
	err := evaluateCheck(state, map[string][]string{NodeDown: {"rabbit@node1"}}, true)             // tick 3
	assert.Nil(t, err)
}

// At least once + No events: fail when alarm raised the entire time
func Test_AtLeastOnce_NoEvents_FailWhenAlwaysAlarm(t *testing.T) {
	state := newTestState([]string{NoEvents}, stateCheckModeAtLeastOnce, time.Now().Add(-time.Second))

	// every tick has alarm, never NoEvents
	err := evaluateCheck(state, map[string][]string{NodeAlarmRaised: {"rabbit@node1: mem_alarm"}}, true)
	require.NotNil(t, err)
	assert.Contains(t, err.Title, NoEvents)
}

// All the time + Node down: ticks with no changes should NOT fail (backward compat)
func Test_AllTheTime_NodeDown_NoChangeTickIsNeutral(t *testing.T) {
	state := newTestState([]string{NodeDown}, stateCheckModeAllTheTime, time.Now().Add(time.Minute))
	err := evaluateCheck(state, map[string][]string{}, false)
	assert.Nil(t, err) // no error — ticks with no changes are neutral
}

// All the time + Node down: fail at completion if node down never happened
func Test_AllTheTime_NodeDown_FailIfNeverSeen(t *testing.T) {
	state := newTestState([]string{NodeDown}, stateCheckModeAllTheTime, time.Now().Add(-time.Second))
	err := evaluateCheck(state, map[string][]string{}, true)
	require.NotNil(t, err)
	assert.Contains(t, err.Title, "never occurred")
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
