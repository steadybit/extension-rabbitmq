// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package extrabbitmq

import (
	"context"
	"errors"
	"fmt"
	"github.com/steadybit/extension-rabbitmq/config"
	"strconv"
	"time"

	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/action-kit/go/action_kit_sdk"
	extension_kit "github.com/steadybit/extension-kit"
	"github.com/steadybit/extension-kit/extbuild"
	"github.com/steadybit/extension-kit/extutil"
	"github.com/steadybit/extension-rabbitmq/clients"
)

// Adjust to your actual target type id for queues
const rabbitQueueTargetId = "com.steadybit.extension_rabbitmq.queue"

type QueueBacklogCheckAction struct{}

type QueueBacklogCheckState struct {
	Vhost   string
	Queue   string
	MgmtURL string

	AcceptableBacklog int64
	End               time.Time

	StateCheckSuccess bool
	StateCheckFailed  bool
}

// Interface assertions
var (
	_ action_kit_sdk.Action[QueueBacklogCheckState]           = (*QueueBacklogCheckAction)(nil)
	_ action_kit_sdk.ActionWithStatus[QueueBacklogCheckState] = (*QueueBacklogCheckAction)(nil)
)

func NewQueueBacklogCheckAction() action_kit_sdk.Action[QueueBacklogCheckState] {
	return &QueueBacklogCheckAction{}
}

func (a *QueueBacklogCheckAction) NewEmptyState() QueueBacklogCheckState {
	return QueueBacklogCheckState{}
}

func (a *QueueBacklogCheckAction) Describe() action_kit_api.ActionDescription {
	return action_kit_api.ActionDescription{
		Id:          fmt.Sprintf("%s.check-backlog", rabbitQueueTargetId),
		Label:       "Check Queue Backlog",
		Description: "Check the backlog of a RabbitMQ queue (total messages in queue). Fails if backlog exceeds the threshold during the check window.",
		Version:     extbuild.GetSemverVersionStringOrUnknown(),
		Icon:        extutil.Ptr(rabbitMQIcon),
		TargetSelection: extutil.Ptr(action_kit_api.TargetSelection{
			TargetType:          rabbitQueueTargetId,
			QuantityRestriction: extutil.Ptr(action_kit_api.QuantityRestrictionAll),
			SelectionTemplates: extutil.Ptr([]action_kit_api.TargetSelectionTemplate{
				{
					Label:       "queue name",
					Description: extutil.Ptr("Find queue by name"),
					Query:       "rabbitmq.queue.name=\"\"",
				},
			}),
		}),
		Technology:  extutil.Ptr("RabbitMQ"),
		Category:    extutil.Ptr("RabbitMQ"),
		Kind:        action_kit_api.Check,
		TimeControl: action_kit_api.TimeControlInternal,
		Parameters: []action_kit_api.ActionParameter{
			{
				Name:         "duration",
				Label:        "Duration",
				Description:  extutil.Ptr("How long to observe the queue backlog"),
				Type:         action_kit_api.ActionParameterTypeDuration,
				DefaultValue: extutil.Ptr("30s"),
				Required:     extutil.Ptr(true),
			},
			{
				Name:         "acceptableBacklog",
				Label:        "Backlog alert threshold",
				Description:  extutil.Ptr("Maximum acceptable number of messages in the queue"),
				Type:         action_kit_api.ActionParameterTypeInteger,
				Required:     extutil.Ptr(true),
				DefaultValue: extutil.Ptr("10"),
			},
		},
		Widgets: extutil.Ptr([]action_kit_api.Widget{
			action_kit_api.LineChartWidget{
				Type:  action_kit_api.ComSteadybitWidgetLineChart,
				Title: "Queue Backlog",
				Identity: action_kit_api.LineChartWidgetIdentityConfig{
					MetricName: "rabbitmq_queue_backlog",
					From:       "id",
					Mode:       action_kit_api.ComSteadybitWidgetLineChartIdentityModeSelect,
				},
				Grouping: extutil.Ptr(action_kit_api.LineChartWidgetGroupingConfig{
					ShowSummary: extutil.Ptr(true),
					Groups: []action_kit_api.LineChartWidgetGroup{
						{
							Title: "Under Threshold",
							Color: "success",
							Matcher: action_kit_api.LineChartWidgetGroupMatcherFallback{
								Type: action_kit_api.ComSteadybitWidgetLineChartGroupMatcherFallback,
							},
						},
						{
							Title: "Threshold Violated",
							Color: "warn",
							Matcher: action_kit_api.LineChartWidgetGroupMatcherKeyEqualsValue{
								Type:  action_kit_api.ComSteadybitWidgetLineChartGroupMatcherKeyEqualsValue,
								Key:   "backlog_constraints_fulfilled",
								Value: "false",
							},
						},
					},
				}),
				Tooltip: extutil.Ptr(action_kit_api.LineChartWidgetTooltipConfig{
					MetricValueTitle: extutil.Ptr("Messages"),
					AdditionalContent: []action_kit_api.LineChartWidgetTooltipContent{
						{From: "queue", Title: "Queue"},
						{From: "vhost", Title: "Vhost"},
					},
				}),
			},
		}),
		Status: extutil.Ptr(action_kit_api.MutatingEndpointReferenceWithCallInterval{
			CallInterval: extutil.Ptr("5s"),
		}),
	}
}

func (a *QueueBacklogCheckAction) Prepare(_ context.Context, state *QueueBacklogCheckState, request action_kit_api.PrepareActionRequestBody) (*action_kit_api.PrepareResult, error) {
	// required target attributes
	if len(request.Target.Attributes["rabbitmq.queue.name"]) == 0 {
		return nil, fmt.Errorf("target missing attribute rabbitmq.queue.name")
	}
	if len(request.Target.Attributes["rabbitmq.queue.vhost"]) == 0 {
		return nil, fmt.Errorf("target missing attribute rabbitmq.queue.vhost")
	}
	if len(request.Target.Attributes["rabbitmq.mgmt.url"]) == 0 {
		return nil, fmt.Errorf("target missing attribute rabbitmq.mgmt.url")
	}

	state.Queue = request.Target.Attributes["rabbitmq.queue.name"][0]
	state.Vhost = request.Target.Attributes["rabbitmq.queue.vhost"][0]
	state.MgmtURL = extutil.MustHaveValue(request.Target.Attributes, "rabbitmq.mgmt.url")[0]

	state.AcceptableBacklog = extutil.ToInt64(request.Config["acceptableBacklog"])
	state.StateCheckFailed = false

	duration := request.Config["duration"].(float64)
	state.End = time.Now().Add(time.Millisecond * time.Duration(duration))

	return nil, nil
}

func (a *QueueBacklogCheckAction) Start(_ context.Context, _ *QueueBacklogCheckState) (*action_kit_api.StartResult, error) {
	return nil, nil
}

func (a *QueueBacklogCheckAction) Status(ctx context.Context, state *QueueBacklogCheckState) (*action_kit_api.StatusResult, error) {
	return QueueBacklogCheckStatus(ctx, state)
}

func QueueBacklogCheckStatus(ctx context.Context, state *QueueBacklogCheckState) (*action_kit_api.StatusResult, error) {
	now := time.Now()
	managementEndpoint, err := config.GetEndpointByMgmtURL(state.MgmtURL)
	if err != nil {
		return nil, err
	}
	c, err := clients.CreateMgmtClientFromURL(managementEndpoint)
	if err != nil {
		return nil, extutil.Ptr(extension_kit.ToError("no initialized client for target endpoint", errors.New("client not found")))
	}

	qi, err := c.GetQueue(state.Vhost, state.Queue)
	if err != nil {
		return nil, extutil.Ptr(extension_kit.ToError(fmt.Sprintf("failed to retrieve queue %s in vhost %s: %v", state.Queue, state.Vhost, err), err))
	}

	// RabbitMQ reports:
	// - Messages: total messages (ready + unacked)
	// - MessagesReady: ready to deliver
	// - MessagesUnacknowledged: in-flight
	backlog := qi.Messages

	completed := now.After(state.End)
	var checkError *action_kit_api.ActionKitError
	if backlog <= int(state.AcceptableBacklog) {
		state.StateCheckSuccess = true
	} else {
		state.StateCheckFailed = true
	}
	if completed && state.StateCheckFailed {
		checkError = extutil.Ptr(action_kit_api.ActionKitError{
			Title:  fmt.Sprintf("Queue backlog exceeded threshold %d at least once.", state.AcceptableBacklog),
			Status: extutil.Ptr(action_kit_api.Failed),
		})
	}

	metrics := []action_kit_api.Metric{
		*toQueueMetric(backlog, state, now),
	}

	return &action_kit_api.StatusResult{
		Completed: completed,
		Error:     checkError,
		Metrics:   extutil.Ptr(metrics),
	}, nil
}

func toQueueMetric(backlog int, state *QueueBacklogCheckState, now time.Time) *action_kit_api.Metric {
	return extutil.Ptr(action_kit_api.Metric{
		Name: extutil.Ptr("rabbitmq_queue_backlog"),
		Metric: map[string]string{
			"backlog_constraints_fulfilled": strconv.FormatBool(int64(backlog) <= state.AcceptableBacklog),
			"queue":                         state.Queue,
			"vhost":                         state.Vhost,
			"id":                            state.Vhost + "/" + state.Queue,
		},
		Timestamp: now,
		Value:     float64(backlog),
	})
}
