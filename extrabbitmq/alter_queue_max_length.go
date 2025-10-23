// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package extrabbitmq

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	rabbithole "github.com/michaelklishin/rabbit-hole/v3"
	"github.com/rs/zerolog/log"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/action-kit/go/action_kit_sdk"
	"github.com/steadybit/extension-kit/extbuild"
	"github.com/steadybit/extension-kit/extutil"
	"github.com/steadybit/extension-rabbitmq/clients"
	"github.com/steadybit/extension-rabbitmq/config"
)

type AlterQueueMaxLengthAttack struct{}

type AlterQueueMaxLengthState struct {
	// runtime
	PolicyName string `json:"policyName,omitempty"`

	// inputs / intent
	Queue           string    `json:"queue,omitempty"`
	Vhost           string    `json:"vhost,omitempty"`
	ManagementURL   string    `json:"managementUrl,omitempty"`
	TargetMaxLength int       `json:"targetMaxLength,omitempty"`
	Duration        time.Time `json:"end,omitempty"`

	// store existing policy snapshots (if we want to restore — optional)
	// For simplicity we only delete policies we created on Stop.
}

var _ action_kit_sdk.Action[AlterQueueMaxLengthState] = (*AlterQueueMaxLengthAttack)(nil)

func NewAlterQueueMaxLengthAttack() action_kit_sdk.Action[AlterQueueMaxLengthState] {
	return &AlterQueueMaxLengthAttack{}
}

func (a *AlterQueueMaxLengthAttack) NewEmptyState() AlterQueueMaxLengthState {
	return AlterQueueMaxLengthState{}
}

func (a *AlterQueueMaxLengthAttack) Describe() action_kit_api.ActionDescription {
	return action_kit_api.ActionDescription{
		Id:          fmt.Sprintf("%s.alter-max-length", queueTargetId),
		Label:       "Alter Queue Max Length (Policy)",
		Description: "Dynamically set a queue max length via a RabbitMQ policy. Creates a policy that matches the target queue and deletes it on stop.",
		Version:     extbuild.GetSemverVersionStringOrUnknown(),
		Icon:        extutil.Ptr(rabbitMQIcon),
		Technology:  extutil.Ptr("RabbitMQ"),
		Category:    extutil.Ptr("RabbitMQ"),
		Kind:        action_kit_api.Attack,
		TimeControl: action_kit_api.TimeControlExternal,
		TargetSelection: extutil.Ptr(action_kit_api.TargetSelection{
			TargetType: queueTargetId,
			SelectionTemplates: extutil.Ptr([]action_kit_api.TargetSelectionTemplate{
				{
					Label:       "queue name",
					Description: extutil.Ptr("Select queue by name"),
					Query:       "rabbitmq.queue.name=\"\"",
				},
			}),
			QuantityRestriction: extutil.Ptr(action_kit_api.QuantityRestrictionExactlyOne),
		}),
		Parameters: []action_kit_api.ActionParameter{
			durationAlter,
			{
				Name:         "maxLength",
				Label:        "Max Length",
				Description:  extutil.Ptr("Maximum number of messages the queue will hold. Set 0 for unlimited."),
				Type:         action_kit_api.ActionParameterTypeInteger,
				DefaultValue: extutil.Ptr("1000"),
				Required:     extutil.Ptr(true),
			},
		},
	}
}

func (a *AlterQueueMaxLengthAttack) Prepare(ctx context.Context, state *AlterQueueMaxLengthState, request action_kit_api.PrepareActionRequestBody) (*action_kit_api.PrepareResult, error) {
	// duration
	d := request.Config["duration"].(float64)
	state.Duration = time.Now().Add(time.Duration(d) * time.Millisecond)

	state.TargetMaxLength = extutil.ToInt(request.Config["maxLength"])
	state.Queue = extutil.MustHaveValue(request.Target.Attributes, "rabbitmq.queue.name")[0]
	state.Vhost = extutil.MustHaveValue(request.Target.Attributes, "rabbitmq.queue.vhost")[0]
	state.ManagementURL = extutil.MustHaveValue(request.Target.Attributes, "rabbitmq.mgmt.url")[0]

	return nil, nil
}

func (a *AlterQueueMaxLengthAttack) Start(ctx context.Context, state *AlterQueueMaxLengthState) (*action_kit_api.StartResult, error) {
	configManagement, err := config.GetEndpointByMgmtURL(state.ManagementURL)
	if err != nil {
		return nil, err
	}
	client, err := clients.CreateMgmtClientFromURL(configManagement)
	if err != nil {
		return nil, fmt.Errorf("no management client for %s", state.ManagementURL)
	}

	// create a unique policy name per queue
	policyName := fmt.Sprintf("steadybit-alter-maxlen-%s-%s", state.Queue, uuid.New().String()[:8])

	// pattern must match only this queue name (exact)
	// RabbitMQ policy regex is applied against resource name, so anchor it
	pattern := fmt.Sprintf("^%s$", rabbitsafeRegex(state.Queue))

	def := map[string]interface{}{
		"max-length": state.TargetMaxLength,
	}

	// rabbithole client: PutPolicy(vhost, name, policy)
	policy := rabbithole.Policy{
		Pattern:    pattern,
		Definition: def,
		Priority:   0,
		ApplyTo:    "queues",
	}
	if _, err = client.PutPolicy(state.Vhost, policyName, policy); err != nil {
		// If we fail to put policy for a queue, try to cleanup created policies so far
		_ = safeDeletePolicy(client, state.Vhost, state.PolicyName)

		return nil, fmt.Errorf("failed to create policy for queue %s: %w", state.Queue, err)
	}

	log.Info().Str("vhost", state.Vhost).Str("queue", state.Queue).Str("policy", policyName).Int("maxLength", state.TargetMaxLength).Msg("created policy")

	state.PolicyName = policyName

	msg := fmt.Sprintf("Created policy to set max-length=%d for queue %s", state.TargetMaxLength, state.Queue)
	return &action_kit_api.StartResult{
		Messages: &[]action_kit_api.Message{{
			Level:   extutil.Ptr(action_kit_api.Info),
			Message: msg,
		}},
	}, nil
}

func (a *AlterQueueMaxLengthAttack) Stop(ctx context.Context, state *AlterQueueMaxLengthState) (*action_kit_api.StopResult, error) {
	if len(state.PolicyName) == 0 {
		return &action_kit_api.StopResult{}, nil
	}

	configManagement, err := config.GetEndpointByMgmtURL(state.ManagementURL)
	if err != nil {
		return nil, err
	}
	client, err := clients.CreateMgmtClientFromURL(configManagement)
	if err != nil {
		return nil, fmt.Errorf("no management client for %s", state.ManagementURL)
	}

	if err = safeDeletePolicy(client, state.Vhost, state.PolicyName); err != nil {
		return nil, fmt.Errorf("failed to delete policy: %s", err)
	} else {
		log.Info().Str("policy", state.PolicyName).Msg("deleted policy")
	}

	return &action_kit_api.StopResult{
		Messages: &[]action_kit_api.Message{{
			Level:   extutil.Ptr(action_kit_api.Info),
			Message: fmt.Sprintf("Deleted %s policy", state.PolicyName),
		}},
	}, nil
}

// safeDeletePolicy calls DeletePolicy and ignores certain response types to avoid bubbling non-fatal errors.
func safeDeletePolicy(c *rabbithole.Client, vhost, name string) error {
	resp, err := c.DeletePolicy(vhost, name)
	if err != nil {
		return err
	}
	if resp != nil && resp.StatusCode == 404 {
		return fmt.Errorf("policy %s not found in vhost %s", name, vhost)
	}
	return nil
}

// rabbitsafeRegex escapes any regex meta-chars in queue name so we match exact literal.
// Minimal escaping: replace `.` and other typical regex chars. Adjust if needed.
func rabbitsafeRegex(name string) string {
	repl := strings.NewReplacer(
		".", "\\.",
		"+", "\\+",
		"*", "\\*",
		"?", "\\?",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"{", "\\{",
		"}", "\\}",
		"^", "\\^",
		"$", "\\$",
		"|", "\\|",
		"/", "\\/",
	)
	return repl.Replace(name)
}
