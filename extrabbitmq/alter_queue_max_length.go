// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package extrabbitmq

import (
	"context"
	"fmt"
	"github.com/steadybit/extension-rabbitmq/clients"
	"regexp"
	"time"

	rabbithole "github.com/michaelklishin/rabbit-hole/v3"
	"github.com/rs/zerolog/log"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/action-kit/go/action_kit_sdk"
	"github.com/steadybit/extension-kit/extbuild"
	"github.com/steadybit/extension-kit/extutil"
)

func queueNamePatternFor(queue string) string {
	if queue == "" {
		return "^.*$"
	}
	return "^" + regexp.QuoteMeta(queue) + "$"
}

type AlterQueueMaxLengthAttack struct{}

type AlterQueueMaxLengthState struct {
	// target information
	Vhost string
	Queue string

	// policy configuration
	PolicyName string
	Pattern    string
	MaxLength  int
	Priority   int
	ApplyTo    string

	// bookkeeping for rollback
	ReplacedExisting bool
	PreviousPolicy   *rabbithole.Policy

	// management endpoint to use for API calls (must be provided on target)
	MgmtURL string
}

var _ action_kit_sdk.Action[AlterQueueMaxLengthState] = (*AlterQueueMaxLengthAttack)(nil)

func NewAlterQueueMaxLengthAttack() action_kit_sdk.Action[AlterQueueMaxLengthState] {
	return &AlterQueueMaxLengthAttack{}
}

func (a *AlterQueueMaxLengthAttack) NewEmptyState() AlterQueueMaxLengthState {
	return AlterQueueMaxLengthState{
		Priority: 0,
		ApplyTo:  "queues",
	}
}

func (a *AlterQueueMaxLengthAttack) Describe() action_kit_api.ActionDescription {
	return action_kit_api.ActionDescription{
		Id:          "com.steadybit.extension_rabbitmq.vhost.alter-queue-max-length",
		Label:       "Limit Queue Length (policy)",
		Description: "Apply a RabbitMQ policy to limit queue length (x-max-length) for matching queues in the vhost. Rollback removes or restores the previous policy.",
		Version:     extbuild.GetSemverVersionStringOrUnknown(),
		Icon:        extutil.Ptr(rabbitMQIcon),
		TargetSelection: extutil.Ptr(action_kit_api.TargetSelection{
			TargetType:          "com.steadybit.extension_rabbitmq.queue",
			QuantityRestriction: extutil.Ptr(action_kit_api.QuantityRestrictionExactlyOne),
			SelectionTemplates: extutil.Ptr([]action_kit_api.TargetSelectionTemplate{
				{
					Label:       "queue name",
					Description: extutil.Ptr("Select the queue to which the policy will be applied"),
					Query:       "rabbitmq.queue.name=\"\"",
				},
			}),
		}),
		Technology:  extutil.Ptr("RabbitMQ"),
		Category:    extutil.Ptr("RabbitMQ"),
		Kind:        action_kit_api.Attack,
		TimeControl: action_kit_api.TimeControlExternal,
		Parameters: []action_kit_api.ActionParameter{
			durationAlter,
			{
				Name:         "max_length",
				Label:        "Max messages per queue",
				Description:  extutil.Ptr("Maximum number of messages allowed in queues matched by the policy."),
				Type:         action_kit_api.ActionParameterTypeInteger,
				Required:     extutil.Ptr(true),
				DefaultValue: extutil.Ptr("10000"),
			},
			{
				Name:        "pattern",
				Label:       "Queue name pattern (regex)",
				Description: extutil.Ptr("Regex pattern to match queue names. Default '^<queue-name>$' matches only the selected queue."),
				Type:        action_kit_api.ActionParameterTypeString,
				Required:    extutil.Ptr(false),
			},
			{
				Name:         "priority",
				Label:        "Policy priority",
				Description:  extutil.Ptr("Priority of the policy. Higher priority wins when multiple policies apply."),
				Type:         action_kit_api.ActionParameterTypeInteger,
				Required:     extutil.Ptr(false),
				DefaultValue: extutil.Ptr("0"),
			},
			{
				Name:        "policyName",
				Label:       "Policy name (optional)",
				Description: extutil.Ptr("Optional name for the created policy. If omitted a unique name is generated."),
				Type:        action_kit_api.ActionParameterTypeString,
				Required:    extutil.Ptr(false),
			},
		},
	}
}

func (a *AlterQueueMaxLengthAttack) Prepare(ctx context.Context, state *AlterQueueMaxLengthState, request action_kit_api.PrepareActionRequestBody) (*action_kit_api.PrepareResult, error) {
	// required target attributes
	if len(request.Target.Attributes["rabbitmq.vhost.name"]) == 0 {
		return nil, fmt.Errorf("target missing attribute rabbitmq.vhost.name")
	}
	if len(request.Target.Attributes["rabbitmq.queue.name"]) == 0 {
		return nil, fmt.Errorf("target missing attribute rabbitmq.queue.name")
	}
	if len(request.Target.Attributes["rabbitmq.mgmt.url"]) == 0 {
		return nil, fmt.Errorf("target missing attribute rabbitmq.mgmt.url")
	}

	// populate state
	state.Vhost = request.Target.Attributes["rabbitmq.vhost.name"][0]
	state.Queue = request.Target.Attributes["rabbitmq.queue.name"][0]
	state.MgmtURL = request.Target.Attributes["rabbitmq.mgmt.url"][0]

	// parameters
	state.MaxLength = extutil.ToInt(request.Config["max_length"])
	if state.MaxLength <= 0 {
		return nil, fmt.Errorf("invalid max_length: %d", state.MaxLength)
	}
	if p, ok := request.Config["pattern"].(string); ok && p != "" {
		state.Pattern = p
	} else {
		state.Pattern = queueNamePatternFor(state.Queue)
	}
	if pr := extutil.ToInt(request.Config["priority"]); pr != 0 {
		state.Priority = pr
	}
	if pn, ok := request.Config["policyName"].(string); ok && pn != "" {
		state.PolicyName = pn
	} else {
		// generate a unique name
		state.PolicyName = fmt.Sprintf("steadybit-limit-%d", time.Now().Unix())
	}

	state.ApplyTo = "queues"

	return nil, nil
}

func (a *AlterQueueMaxLengthAttack) Start(ctx context.Context, state *AlterQueueMaxLengthState) (*action_kit_api.StartResult, error) {
	c, ok := clients.GetByMgmtURL(state.MgmtURL)
	if c == nil || !ok || c.Mgmt == nil {
		return nil, fmt.Errorf("no pooled management client for endpoint %s", state.MgmtURL)
	}

	// check for existing policy with the same name in the vhost
	existing, err := c.Mgmt.ListPoliciesIn(state.Vhost)
	if err != nil {
		return nil, fmt.Errorf("failed to list policies in vhost %s: %w", state.Vhost, err)
	}
	for _, p := range existing {
		if p.Name == state.PolicyName {
			// store previous and mark replaced
			state.ReplacedExisting = true
			cpy := p
			state.PreviousPolicy = &cpy
			break
		}
	}

	// policy definition uses RabbitMQ management API canonical keys
	definition := map[string]interface{}{
		"max-length": state.MaxLength,
		"overflow":   "reject-publish",
	}

	policy := rabbithole.Policy{
		Pattern:    state.Pattern,
		Definition: definition,
		Priority:   state.Priority,
		ApplyTo:    state.ApplyTo,
	}

	// apply policy
	if _, err := c.Mgmt.PutPolicy(state.Vhost, state.PolicyName, policy); err != nil {
		return nil, fmt.Errorf("failed to put policy %s in vhost %s for queue %s: %w", state.PolicyName, state.Vhost, state.Queue, err)
	}

	log.Info().Str("vhost", state.Vhost).Str("queue", state.Queue).Str("policy", state.PolicyName).Int("max_length", state.MaxLength).Msg("policy applied")

	return &action_kit_api.StartResult{
		Messages: &[]action_kit_api.Message{{Level: extutil.Ptr(action_kit_api.Info), Message: fmt.Sprintf("Applied policy %s on queue %s in vhost %s", state.PolicyName, state.Queue, state.Vhost)}},
	}, nil
}

func (a *AlterQueueMaxLengthAttack) Stop(ctx context.Context, state *AlterQueueMaxLengthState) (*action_kit_api.StopResult, error) {
	c, ok := clients.GetByMgmtURL(state.MgmtURL)
	if c == nil || !ok || c.Mgmt == nil {
		return nil, fmt.Errorf("no pooled management client for endpoint %s", state.MgmtURL)
	}

	// rollback: if we replaced an existing policy with same name, restore it.
	if state.ReplacedExisting && state.PreviousPolicy != nil {
		if _, err := c.Mgmt.PutPolicy(state.Vhost, state.PolicyName, *state.PreviousPolicy); err != nil {
			return nil, fmt.Errorf("failed to restore previous policy %s in vhost %s for queue %s: %w", state.PolicyName, state.Vhost, state.Queue, err)
		}
		log.Info().Str("vhost", state.Vhost).Str("queue", state.Queue).Str("policy", state.PolicyName).Msg("restored previous policy")
		return &action_kit_api.StopResult{
			Messages: &[]action_kit_api.Message{{Level: extutil.Ptr(action_kit_api.Info), Message: fmt.Sprintf("Restored previous policy %s on queue %s in vhost %s", state.PolicyName, state.Queue, state.Vhost)}},
		}, nil
	}

	// otherwise delete the policy we created
	resp, err := c.Mgmt.DeletePolicy(state.Vhost, state.PolicyName)
	if err != nil {
		// If delete fails because policy not found, treat as success
		if resp != nil && resp.StatusCode == 404 {
			log.Info().Str("vhost", state.Vhost).Str("queue", state.Queue).Str("policy", state.PolicyName).Msg("policy already removed")
		} else {
			return nil, fmt.Errorf("failed to delete policy %s in vhost %s for queue %s: %w", state.PolicyName, state.Vhost, state.Queue, err)
		}
	} else if resp != nil && resp.StatusCode >= 400 {
		// Non-2xx response without Go error
		return nil, fmt.Errorf("failed to delete policy %s in vhost %s for queue %s: HTTP %d", state.PolicyName, state.Vhost, state.Queue, resp.StatusCode)
	}

	log.Info().Str("vhost", state.Vhost).Str("queue", state.Queue).Str("policy", state.PolicyName).Msg("policy removed")

	return &action_kit_api.StopResult{
		Messages: &[]action_kit_api.Message{{Level: extutil.Ptr(action_kit_api.Info), Message: fmt.Sprintf("Removed policy %s from queue %s in vhost %s", state.PolicyName, state.Queue, state.Vhost)}},
	}, nil
}
