// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2026 Steadybit GmbH

package extrabbitmq

import (
	"testing"

	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/stretchr/testify/assert"
)

func TestParameterGroups(t *testing.T) {
	content := []string{"headers@" + groupMessageContent, "body@" + groupMessageContent}
	publishToQueue := func(load ...string) []string {
		l := append(append([]string{}, content...), load...)
		return append(l, "exchange@"+groupRouting, "routingKey@"+groupRouting, "maxConcurrent@"+groupPublisherBehavior)
	}
	publishToExchange := func(load ...string) []string {
		// The routing key stays visible: direct and topic exchanges need it.
		l := append(append([]string{}, content...), "routingKey")
		return append(append(l, load...), "maxConcurrent@"+groupPublisherBehavior)
	}
	queueGroups := []action_kit_api.ParameterGroup{routingGroup, publisherBehaviorGroup}
	exchangeGroups := []action_kit_api.ParameterGroup{publisherBehaviorGroup}

	tests := []struct {
		name    string
		desc    action_kit_api.ActionDescription
		layout  []string
		defined []action_kit_api.ParameterGroup
	}{
		{"queueBacklog", NewQueueBacklogCheckAction().Describe(), []string{"duration", "acceptableBacklog", "failEarly"}, nil},
		{"nodes", NewCheckNodesAction().Describe(), []string{"duration", "expectedChanges", "changeCheckMode", "failEarly"}, nil},
		{"publishFixedAmount", NewPublishRabbitFixedAmount().Describe(), publishToQueue("numberOfMessages", "duration"), queueGroups},
		{"publishPeriodically", NewPublishRabbitPeriodically().Describe(), publishToQueue("messagesPerSecond", "duration", "successRate"), queueGroups},
		{"publishExchangeFixedAmount", NewPublishExchangeFixedAmount().Describe(), publishToExchange("numberOfMessages", "duration", "successRate"), exchangeGroups},
		{"publishExchangePeriodically", NewPublishExchangePeriodically().Describe(), publishToExchange("messagesPerSecond", "duration", "successRate"), exchangeGroups},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.layout, layout(t, tt.desc))
			assert.Equal(t, tt.defined, definedGroups(tt.desc))
		})
	}
}

// layout lists the parameters in the order platforms show them, as "name" or "name@group". None of these actions sets
// an order, so the list order decides and a group is shown where its first parameter is.
func layout(t *testing.T, desc action_kit_api.ActionDescription) []string {
	t.Helper()
	var names []string
	for _, p := range desc.Parameters {
		assert.Nil(t, p.Order, "%s: parameter %s has an order, so the list order no longer decides", desc.Id, p.Name)
		if p.Group != nil {
			names = append(names, p.Name+"@"+*p.Group)
		} else {
			names = append(names, p.Name)
		}
		// No advanced parameter is left to the platform's fallback group, and the grouped ones that were advanced stay
		// advanced for platforms without groups.
		if p.Advanced != nil && *p.Advanced {
			assert.NotNil(t, p.Group, "%s: advanced parameter %s has no group", desc.Id, p.Name)
		}
		if p.Name == "failEarly" {
			assert.Nil(t, p.Advanced, "%s: failEarly is shown on every platform", desc.Id)
		}
	}
	return names
}

func definedGroups(desc action_kit_api.ActionDescription) []action_kit_api.ParameterGroup {
	if desc.ParameterGroups == nil {
		return nil
	}
	return *desc.ParameterGroups
}
