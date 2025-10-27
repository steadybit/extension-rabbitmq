// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package extrabbitmq

import (
	"context"
	"fmt"
	"regexp"
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
	PolicyName      string
	Queue           string
	Vhost           string
	ManagementURL   string
	TargetMaxLength int
	Duration        time.Time

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

// highestPriorityForQueue scans existing policies in a vhost and returns the
// highest priority among policies that would apply to the given queue name.
// We consider policies with ApplyTo == "queues" or "all" or "" (unspecified),
// and whose regex Pattern matches the queue name.
func highestPriorityForQueue(c *rabbithole.Client, vhost, queue string) (int, error) {
	policies, err := c.ListPoliciesIn(vhost)
	if err != nil {
		return 0, err
	}
	max := 0
	found := false
	for _, p := range policies {
		// Only policies that could affect queues
		if p.ApplyTo != "" && p.ApplyTo != "queues" && p.ApplyTo != "all" {
			continue
		}
		if p.Pattern == "" {
			continue
		}
		re, err := regexp.Compile(p.Pattern)
		if err != nil {
			// Ignore invalid policy regexes rather than failing
			continue
		}
		if re.MatchString(queue) {
			found = true
			if p.Priority > max {
				max = p.Priority
			}
		}
	}
	if !found {
		return 0, nil
	}
	return max, nil
}

func (a *AlterQueueMaxLengthAttack) Describe() action_kit_api.ActionDescription {
	return action_kit_api.ActionDescription{
		Id:          fmt.Sprintf("%s.alter-max-length", queueTargetId),
		Label:       "Alter Queue Max Length",
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

	// Determine a priority higher than any existing policy that matches this queue
	highest, err := highestPriorityForQueue(client, state.Vhost, state.Queue)
	if err != nil {
		// Do not proceed with an unknown priority situation
		return nil, fmt.Errorf("failed to list policies in vhost %s: %w", state.Vhost, err)
	}
	desiredPriority := highest + 1

	// rabbithole client: PutPolicy(vhost, name, policy)
	policy := rabbithole.Policy{
		Pattern:    pattern,
		Definition: def,
		Priority:   desiredPriority,
		ApplyTo:    "queues",
	}
	if _, err = client.PutPolicy(state.Vhost, policyName, policy); err != nil {
		// If we fail to put policy for a queue, try to cleanup created policies so far
		_ = safeDeletePolicy(client, state.Vhost, state.PolicyName)

		return nil, fmt.Errorf("failed to create policy for queue %s: %w", state.Queue, err)
	}

	log.Info().
		Str("vhost", state.Vhost).
		Str("queue", state.Queue).
		Str("policy", policyName).
		Int("maxLength", state.TargetMaxLength).
		Int("priority", desiredPriority).
		Msg("created policy")

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
