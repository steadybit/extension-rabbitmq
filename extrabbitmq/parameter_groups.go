// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2026 Steadybit GmbH

package extrabbitmq

import "github.com/steadybit/action-kit/go/action_kit_api/v2"

const (
	groupMessageContent    = "Message Content"
	groupRouting           = "Routing"
	groupPublisherBehavior = "Publisher Behavior"
)

var (
	// An empty exchange publishes via the default exchange, and an empty routing key defaults to the queue name.
	routingGroup = action_kit_api.ParameterGroup{
		Name:        groupRouting,
		Description: "Leave unchanged to publish directly to the selected queue.",
	}

	publisherBehaviorGroup = action_kit_api.ParameterGroup{
		Name:        groupPublisherBehavior,
		Description: "Only matters when the broker responds slowly: if all requests are still in flight, the next ones wait and the target rate is not reached.",
	}

	exchangeRouting   = withGroup(exchange, groupRouting)
	routingKeyRouting = withGroup(routingKey, groupRouting)

	headersContent = withGroup(headers, groupMessageContent)
	bodyContent    = withGroup(body, groupMessageContent)
)

func withGroup(p action_kit_api.ActionParameter, group string) action_kit_api.ActionParameter {
	p.Group = &group
	return p
}
