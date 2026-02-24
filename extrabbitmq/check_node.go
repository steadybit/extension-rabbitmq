// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package extrabbitmq

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/steadybit/extension-rabbitmq/clients"
	"github.com/steadybit/extension-rabbitmq/config"

	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/action-kit/go/action_kit_sdk"
	extension_kit "github.com/steadybit/extension-kit"
	"github.com/steadybit/extension-kit/extbuild"
	"github.com/steadybit/extension-kit/extutil"
)

// ---------- constants & types ----------

const (
	// target id must match your node discovery
	rabbitNodeTargetId = "com.steadybit.extension_rabbitmq.node"

	NodeDown        = "Node down"
	NodeAlarmRaised = "Node alarm raised"

	// reuse the same values you already use elsewhere
	stateCheckModeAllTheTime  = "all-time"
	stateCheckModeAtLeastOnce = "at-least-once"
)

type CheckNodesAction struct{}

type CheckNodesState struct {
	// config
	End             time.Time
	ExpectedChanges []string
	StateCheckMode  string

	NodeNames     []string
	ManagementURL string

	// baseline
	BaselineRunning map[string]bool // node -> running
	BaselineAlarms  map[string]bool // node -> had any alarm at baseline
	StateCheckOnce  bool
}

var (
	_ action_kit_sdk.Action[CheckNodesState]           = (*CheckNodesAction)(nil)
	_ action_kit_sdk.ActionWithStatus[CheckNodesState] = (*CheckNodesAction)(nil)
)

func NewCheckNodesAction() action_kit_sdk.Action[CheckNodesState] {
	return &CheckNodesAction{}
}

func (a *CheckNodesAction) NewEmptyState() CheckNodesState { return CheckNodesState{} }

func (a *CheckNodesAction) Describe() action_kit_api.ActionDescription {
	return action_kit_api.ActionDescription{
		Id:          fmt.Sprintf("%s.check", rabbitNodeTargetId),
		Label:       "Check Nodes",
		Description: "Observe RabbitMQ node/cluster changes during failures or restarts.",
		Version:     extbuild.GetSemverVersionStringOrUnknown(),
		Icon:        extutil.Ptr(rabbitMQIcon), // reuse your icon
		Technology:  extutil.Ptr("RabbitMQ"),
		Category:    extutil.Ptr("RabbitMQ"),
		Kind:        action_kit_api.Check,
		TimeControl: action_kit_api.TimeControlInternal,
		TargetSelection: extutil.Ptr(action_kit_api.TargetSelection{
			TargetType: rabbitNodeTargetId,
			SelectionTemplates: extutil.Ptr([]action_kit_api.TargetSelectionTemplate{
				{
					Label:       "by node name",
					Description: extutil.Ptr("Find nodes by name"),
					Query:       `rabbitmq.node.name=""`,
				},
			}),
		}),
		Parameters: []action_kit_api.ActionParameter{
			{
				Name:         "duration",
				Label:        "Duration",
				Type:         action_kit_api.ActionParameterTypeDuration,
				DefaultValue: extutil.Ptr("30s"),
				Required:     extutil.Ptr(true),
			},
			{
				Name:  "expectedChanges",
				Label: "Expected Changes",
				Type:  action_kit_api.ActionParameterTypeStringArray,
				Options: extutil.Ptr([]action_kit_api.ParameterOption{
					action_kit_api.ExplicitParameterOption{Label: "Node down", Value: NodeDown},
					action_kit_api.ExplicitParameterOption{Label: "Node alarm raised", Value: NodeAlarmRaised},
				}),
			},
			{
				Name:         "changeCheckMode",
				Label:        "Change Check Mode",
				Type:         action_kit_api.ActionParameterTypeString,
				DefaultValue: extutil.Ptr(stateCheckModeAllTheTime),
				Options: extutil.Ptr([]action_kit_api.ParameterOption{
					action_kit_api.ExplicitParameterOption{Label: "All the time", Value: stateCheckModeAllTheTime},
					action_kit_api.ExplicitParameterOption{Label: "At least once", Value: stateCheckModeAtLeastOnce},
				}),
				Required: extutil.Ptr(true),
			},
		},
		Widgets: extutil.Ptr([]action_kit_api.Widget{
			action_kit_api.StateOverTimeWidget{
				Type:  action_kit_api.ComSteadybitWidgetStateOverTime,
				Title: "RabbitMQ Node Changes",
				Identity: action_kit_api.StateOverTimeWidgetIdentityConfig{
					From: "metric.id",
				},
				Label: action_kit_api.StateOverTimeWidgetLabelConfig{
					From: "metric.id",
				},
				State: action_kit_api.StateOverTimeWidgetStateConfig{
					From: "state",
				},
				Tooltip: action_kit_api.StateOverTimeWidgetTooltipConfig{
					From: "tooltip",
				},
				Url: extutil.Ptr(action_kit_api.StateOverTimeWidgetUrlConfig{
					From: extutil.Ptr("url"),
				}),
				Value: extutil.Ptr(action_kit_api.StateOverTimeWidgetValueConfig{
					Hide: extutil.Ptr(true),
				}),
			},
		}),
		Status: extutil.Ptr(action_kit_api.MutatingEndpointReferenceWithCallInterval{
			CallInterval: extutil.Ptr("2s"),
		}),
	}
}

func (a *CheckNodesAction) Prepare(ctx context.Context, state *CheckNodesState, req action_kit_api.PrepareActionRequestBody) (*action_kit_api.PrepareResult, error) {
	// duration & check mode
	duration := extutil.ToInt64(req.Config["duration"])
	state.End = time.Now().Add(time.Duration(duration) * time.Millisecond)
	if req.Config["expectedChanges"] != nil {
		expectedChanges := extutil.ToStringArray(req.Config["expectedChanges"])
		slices.Sort(expectedChanges)
		state.ExpectedChanges = expectedChanges
	}
	if req.Config["changeCheckMode"] != nil {
		state.StateCheckMode = fmt.Sprintf("%v", req.Config["changeCheckMode"])
	} else {
		state.StateCheckMode = stateCheckModeAllTheTime
	}
	state.ManagementURL = extutil.MustHaveValue(req.Target.Attributes, "rabbitmq.mgmt.url")[0]

	configManagement, err := config.GetEndpointByMgmtURL(state.ManagementURL)
	if err != nil {
		return nil, err
	}
	client, err := clients.CreateMgmtClientFromURL(configManagement)
	if err != nil {
		return nil, fmt.Errorf("no management client for %s", state.ManagementURL)
	}
	// collect selected node names from target attributes (may be one or more)
	names := extutil.MustHaveValue(req.Target.Attributes, "rabbitmq.node.name")
	state.NodeNames = append([]string(nil), names...)

	// fetch each selected node individually to avoid listing all nodes
	state.BaselineRunning = make(map[string]bool, len(state.NodeNames))
	state.BaselineAlarms = make(map[string]bool, len(state.NodeNames))
	for _, name := range state.NodeNames {
		n, err := client.GetNode(name)
		if err != nil {
			return nil, extutil.Ptr(extension_kit.ToError(fmt.Sprintf("Failed to get node %s from RabbitMQ.", name), err))
		}
		state.BaselineRunning[n.Name] = n.IsRunning
		state.BaselineAlarms[n.Name] = n.MemAlarm || n.DiskFreeAlarm
	}

	return nil, nil
}

func (a *CheckNodesAction) Start(_ context.Context, _ *CheckNodesState) (*action_kit_api.StartResult, error) {
	return nil, nil
}

func (a *CheckNodesAction) Status(ctx context.Context, state *CheckNodesState) (*action_kit_api.StatusResult, error) {
	now := time.Now()
	configManagement, err := config.GetEndpointByMgmtURL(state.ManagementURL)
	if err != nil {
		return nil, err
	}
	client, err := clients.CreateMgmtClientFromURL(configManagement)
	if err != nil {
		return nil, fmt.Errorf("no management client for %s", state.ManagementURL)
	}

	// detect changes
	changes := make(map[string][]string, 2)

	current := make(map[string]bool, len(state.NodeNames))
	for _, name := range state.NodeNames {
		n, err := client.GetNode(name)
		if err != nil {
			// treat lookup failure as node down/unavailable
			if was, ok := state.BaselineRunning[name]; ok && was {
				changes[NodeDown] = append(changes[NodeDown], name)
			}
		} else {
			current[n.Name] = n.IsRunning

			mem := n.MemAlarm
			disk := n.DiskFreeAlarm
			any := mem || disk
			if any {
				labels := make([]string, 0, 2)
				if mem {
					labels = append(labels, "mem_alarm")
				}
				if disk {
					labels = append(labels, "disk_free_alarm")
				}
				changes[NodeAlarmRaised] = append(changes[NodeAlarmRaised], fmt.Sprintf("%s: %s", n.Name, strings.Join(labels, ",")))
			}

			if was, ok := state.BaselineRunning[n.Name]; ok && was && !n.IsRunning {
				changes[NodeDown] = append(changes[NodeDown], n.Name)
			}
		}
	}

	// expected change evaluation
	completed := now.After(state.End)
	var checkErr *action_kit_api.ActionKitError
	changeKeys := make([]string, 0, len(changes))
	for k := range changes {
		changeKeys = append(changeKeys, k)
	}

	if len(state.ExpectedChanges) > 0 {
		switch state.StateCheckMode {
		case stateCheckModeAllTheTime:
			for _, c := range changeKeys {
				if !slices.Contains(state.ExpectedChanges, c) {
					checkErr = extutil.Ptr(action_kit_api.ActionKitError{
						Title:  fmt.Sprintf("Nodes got an unexpected change '%s' whereas '%v' is expected.", c, state.ExpectedChanges),
						Status: extutil.Ptr(action_kit_api.Failed),
					})
				}
			}
			if completed && checkErr == nil && len(changeKeys) == 0 {
				checkErr = extutil.Ptr(action_kit_api.ActionKitError{
					Title:  fmt.Sprintf("Nodes didn't get the expected changes '%v'.", state.ExpectedChanges),
					Status: extutil.Ptr(action_kit_api.Failed),
				})
			}
		case stateCheckModeAtLeastOnce:
			for _, c := range changeKeys {
				if slices.Contains(state.ExpectedChanges, c) {
					state.StateCheckOnce = true
				}
			}
			if completed && !state.StateCheckOnce {
				checkErr = extutil.Ptr(action_kit_api.ActionKitError{
					Title:  fmt.Sprintf("Nodes didn't get the expected changes '%v' at least once.", state.ExpectedChanges),
					Status: extutil.Ptr(action_kit_api.Failed),
				})
			}
		}
	}

	metrics := []action_kit_api.Metric{
		*toNodeChangeMetric(state.ManagementURL, state.ExpectedChanges, changeKeys, changes, now),
	}

	return &action_kit_api.StatusResult{
		Completed: completed,
		Error:     checkErr,
		Metrics:   extutil.Ptr(metrics),
	}, nil
}

func toNodeChangeMetric(mgmtURL string, expected, changeNames []string, changes map[string][]string, ts time.Time) *action_kit_api.Metric {
	var tooltip, st string

	if len(changes) > 0 {
		recap := "NODE ACTIVITY"
		keys := make([]string, 0, len(changes))
		for k := range changes {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			recap += "\n" + k + ":\n"
			vals := changes[k]
			sort.Strings(vals)
			for _, v := range vals {
				recap += v + "\n"
			}
		}
		tooltip = recap

		sort.Strings(expected)
		sort.Strings(changeNames)

		st = "warn"
		for _, c := range changeNames {
			if slices.Contains(expected, c) {
				st = "success"
			}
		}
	} else {
		tooltip = "No changes"
		st = "info"
	}

	return extutil.Ptr(action_kit_api.Metric{
		Name: extutil.Ptr("rabbit_node_state"),
		Metric: map[string]string{
			"metric.id": fmt.Sprintf("Expected: %s", strings.Join(expected, ",")),
			"url":       mgmtURL,
			"state":     st,
			"tooltip":   tooltip,
		},
		Timestamp: ts,
		Value:     0,
	})
}
