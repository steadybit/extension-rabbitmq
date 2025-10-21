package extrabbitmq

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/discovery-kit/go/discovery_kit_api"
	"github.com/steadybit/extension-kit/extutil"
	"testing"
	"time"

	rabbithole "github.com/michaelklishin/rabbit-hole/v3"
	"github.com/steadybit/extension-rabbitmq/clients"
	"github.com/steadybit/extension-rabbitmq/config"
)

const (
	rabbitMQIcon              = "data:image/svg+xml;base64,PHN2ZyB2aWV3Qm94PSIwIDAgMjQgMjQiIGZpbGw9Im5vbmUiIHhtbG5zPSJodHRwOi8vd3d3LnczLm9yZy8yMDAwL3N2ZyI+CjxwYXRoIGQ9Ik0yMS4xOTggMTAuNjAxSDE0Ljc5ODZDMTQuNjkzMiAxMC42MDE2IDE0LjU4ODYgMTAuNTgyMyAxNC40OTEgMTAuNTQ0M0MxNC4zOTM0IDEwLjUwNjIgMTQuMzA0OCAxMC40NTAyIDE0LjIzMDIgMTAuMzc5M0MxNC4xNTU2IDEwLjMwODUgMTQuMDk2NiAxMC4yMjQzIDE0LjA1NjUgMTAuMTMxNkMxNC4wMTY1IDEwLjAzODkgMTMuOTk2MiA5LjkzOTYxIDEzLjk5NjggOS44Mzk0M1YzLjc2MTU0QzEzLjk5NjggMy42NjE3NiAxMy45NzYxIDMuNTYyOTYgMTMuOTM1NyAzLjQ3MDgzQzEzLjg5NTQgMy4zNzg3MSAxMy44MzYyIDMuMjk1MDcgMTMuNzYxNyAzLjIyNDc0QzEzLjY4NzIgMy4xNTQ0IDEzLjU5ODggMy4wOTg3NiAxMy41MDE1IDMuMDYxMDFDMTMuNDA0MyAzLjAyMzI2IDEzLjMwMDEgMy4wMDQxNSAxMy4xOTUxIDMuMDA0NzdIMTAuNzk5N0MxMC42OTQ2IDMuMDA0MTUgMTAuNTkwNSAzLjAyMzI2IDEwLjQ5MzIgMy4wNjEwMUMxMC4zOTYgMy4wOTg3NiAxMC4zMDc2IDMuMTU0NCAxMC4yMzMgMy4yMjQ3NEMxMC4xNTg1IDMuMjk1MDcgMTAuMDk5NCAzLjM3ODcxIDEwLjA1OSAzLjQ3MDgzQzEwLjAxODcgMy41NjI5NiA5Ljk5NzkgMy42NjE3NiA5Ljk5NzkgMy43NjE1NFY5LjgzOTQzQzkuOTk4NTcgOS45Mzk2MSA5Ljk3ODI4IDEwLjAzODkgOS45MzgyMyAxMC4xMzE2QzkuODk4MTcgMTAuMjI0MyA5LjgzOTEzIDEwLjMwODUgOS43NjQ1NSAxMC4zNzkzQzkuNjg5OTYgMTAuNDUwMiA5LjYwMTMxIDEwLjUwNjIgOS41MDM3MyAxMC41NDQzQzkuNDA2MTUgMTAuNTgyMyA5LjMwMTU5IDEwLjYwMTYgOS4xOTYxMSAxMC42MDFINi44MDA3NUM2LjY5NTI3IDEwLjYwMTYgNi41OTA3MSAxMC41ODIzIDYuNDkzMTMgMTAuNTQ0M0M2LjM5NTU2IDEwLjUwNjIgNi4zMDY5IDEwLjQ1MDIgNi4yMzIzMiAxMC4zNzkzQzYuMTU3NzMgMTAuMzA4NSA2LjA5ODcgMTAuMjI0MyA2LjA1ODY0IDEwLjEzMTZDNi4wMTg1OCAxMC4wMzg5IDUuOTk4MjkgOS45Mzk2MSA1Ljk5ODk2IDkuODM5NDNWMy43NjE1NEM1Ljk5OTYzIDMuNjYxMzYgNS45NzkzNCAzLjU2MjA1IDUuOTM5MjggMy40NjkzN0M1Ljg5OTIyIDMuMzc2NjkgNS44NDAxOSAzLjI5MjQ5IDUuNzY1NiAzLjIyMTY1QzUuNjkxMDIgMy4xNTA4MSA1LjYwMjM2IDMuMDk0NzQgNS41MDQ3OSAzLjA1NjY5QzUuNDA3MjEgMy4wMTg2NSA1LjMwMjY1IDIuOTk5MzggNS4xOTcxNyAzLjAwMDAySDIuNzk2OEMyLjY5MTc0IDMuMDAwMDEgMi41ODc3MiAzLjAxOTc0IDIuNDkwNzIgMy4wNTgwN0MyLjM5MzcyIDMuMDk2NCAyLjMwNTY2IDMuMTUyNTcgMi4yMzE2MSAzLjIyMzM1QzIuMTU3NTUgMy4yOTQxMiAyLjA5ODk3IDMuMzc4MTEgMi4wNTkyMiAzLjQ3MDQ3QzIuMDE5NDggMy41NjI4NCAxLjk5OTM2IDMuNjYxNzYgMi4wMDAwMiAzLjc2MTU0VjIxLjIzODVDMS45OTkzNSAyMS4zMzg2IDIuMDE5NjMgMjEuNDM4IDIuMDU5NjkgMjEuNTMwNkMyLjA5OTc1IDIxLjYyMzMgMi4xNTg3OSAyMS43MDc1IDIuMjMzMzcgMjEuNzc4NEMyLjMwNzk2IDIxLjg0OTIgMi4zOTY2MSAyMS45MDUzIDIuNDk0MTkgMjEuOTQzM0MyLjU5MTc3IDIxLjk4MTQgMi42OTYzMyAyMi4wMDA2IDIuODAxODEgMjJIMjEuMTk4QzIxLjMwMzQgMjIuMDAwNiAyMS40MDggMjEuOTgxNCAyMS41MDU2IDIxLjk0MzNDMjEuNjAzMiAyMS45MDUzIDIxLjY5MTggMjEuODQ5MiAyMS43NjY0IDIxLjc3ODRDMjEuODQxIDIxLjcwNzUgMjEuOSAyMS42MjMzIDIxLjk0MDEgMjEuNTMwNkMyMS45ODAxIDIxLjQzOCAyMi4wMDA0IDIxLjMzODYgMjEuOTk5NyAyMS4yMzg1VjExLjM3NjhDMjIuMDAyNCAxMS4yNzU0IDIxLjk4MzYgMTEuMTc0NSAyMS45NDQ1IDExLjA4MDJDMjEuOTA1MyAxMC45ODU5IDIxLjg0NjYgMTAuODk5OSAyMS43NzE4IDEwLjgyNzZDMjEuNjk3IDEwLjc1NTIgMjEuNjA3NyAxMC42OTc5IDIxLjUwOTEgMTAuNjU4OUMyMS40MTA1IDEwLjYyIDIxLjMwNDcgMTAuNjAwMyAyMS4xOTggMTAuNjAxWk0xNy45ODA4IDE3LjA1NDlDMTcuOTgxNCAxNy4yMDQ2IDE3Ljk1MDkgMTcuMzUzMSAxNy44OTEgMTcuNDkxNkMxNy44MzExIDE3LjYzMDIgMTcuNzQzIDE3Ljc1NjEgMTcuNjMxNyAxNy44NjIzQzE3LjUyMDUgMTcuOTY4NCAxNy4zODgyIDE4LjA1MjYgMTcuMjQyNiAxOC4xMTAxQzE3LjA5NjkgMTguMTY3NiAxNi45NDA4IDE4LjE5NzEgMTYuNzgzMSAxOC4xOTcxSDE1LjE3OTVDMTUuMDIxOCAxOC4xOTcxIDE0Ljg2NTYgMTguMTY3NiAxNC43MiAxOC4xMTAxQzE0LjU3NDQgMTguMDUyNiAxNC40NDIxIDE3Ljk2ODQgMTQuMzMwOCAxNy44NjIzQzE0LjIxOTUgMTcuNzU2MSAxNC4xMzE0IDE3LjYzMDIgMTQuMDcxNSAxNy40OTE2QzE0LjAxMTYgMTcuMzUzMSAxMy45ODEyIDE3LjIwNDYgMTMuOTgxOCAxNy4wNTQ5VjE1LjUzNjZDMTMuOTgxMiAxNS4zODY4IDE0LjAxMTYgMTUuMjM4NCAxNC4wNzE1IDE1LjA5OThDMTQuMTMxNCAxNC45NjEyIDE0LjIxOTUgMTQuODM1MyAxNC4zMzA4IDE0LjcyOTFDMTQuNDQyMSAxNC42MjMgMTQuNTc0NCAxNC41Mzg4IDE0LjcyIDE0LjQ4MTNDMTQuODY1NiAxNC40MjM5IDE1LjAyMTggMTQuMzk0MyAxNS4xNzk1IDE0LjM5NDNIMTYuNzgzMUMxNi45NDA4IDE0LjM5NDMgMTcuMDk2OSAxNC40MjM5IDE3LjI0MjYgMTQuNDgxM0MxNy4zODgyIDE0LjUzODggMTcuNTIwNSAxNC42MjMgMTcuNjMxNyAxNC43MjkxQzE3Ljc0MyAxNC44MzUzIDE3LjgzMTEgMTQuOTYxMiAxNy44OTEgMTUuMDk5OEMxNy45NTA5IDE1LjIzODQgMTcuOTgxNCAxNS4zODY4IDE3Ljk4MDggMTUuNTM2NlYxNy4wNTQ5WiIgZmlsbD0iY3VycmVudENvbG9yIi8+Cjwvc3ZnPg=="
	stateCheckModeAtLeastOnce = "atLeastOnce"
	stateCheckModeAllTheTime  = "allTheTime"
)

var (
	exchange = action_kit_api.ActionParameter{
		Name:        "exchange",
		Label:       "Exchange",
		Description: extutil.Ptr("By default it will be the queue unless you specify a specific exchange"),
		Type:        action_kit_api.ActionParameterTypeString,
		Required:    extutil.Ptr(true),
		Options: extutil.Ptr([]action_kit_api.ParameterOption{
			action_kit_api.ParameterOptionsFromTargetAttribute{
				Attribute: "rabbitmq.queue.bound_exchanges",
			},
		}),
		DefaultValue: extutil.Ptr(""),
	}
	routingKey = action_kit_api.ActionParameter{
		Name:         "routingKey",
		Label:        "Routing Key",
		Type:         action_kit_api.ActionParameterTypeString,
		Required:     extutil.Ptr(false),
		DefaultValue: extutil.Ptr(""),
	}
	body = action_kit_api.ActionParameter{
		Name:         "body",
		Label:        "Message body",
		Type:         action_kit_api.ActionParameterTypeString,
		Required:     extutil.Ptr(true),
		DefaultValue: extutil.Ptr("test-message"),
	}
	headers = action_kit_api.ActionParameter{
		Name:        "headers",
		Label:       "Message Headers",
		Required:    extutil.Ptr(false),
		Description: extutil.Ptr("The Record Headers."),
		Type:        action_kit_api.ActionParameterTypeKeyValue,
	}
	durationAlter = action_kit_api.ActionParameter{
		Label:        "Duration",
		Description:  extutil.Ptr("The duration of the action. The broker configuration will be reverted at the end of the action."),
		Name:         "duration",
		Type:         action_kit_api.ActionParameterTypeDuration,
		DefaultValue: extutil.Ptr("60s"),
		Required:     extutil.Ptr(true),
	}
	duration = action_kit_api.ActionParameter{
		Name:         "duration",
		Label:        "Duration",
		Description:  extutil.Ptr("In which timeframe should the specified records be produced?"),
		Type:         action_kit_api.ActionParameterTypeDuration,
		DefaultValue: extutil.Ptr("10s"),
		Required:     extutil.Ptr(true),
	}
	successRate = action_kit_api.ActionParameter{
		Name:         "successRate",
		Label:        "Required Success Rate",
		Description:  extutil.Ptr("How many percent of the records must be at least successful (in terms of the following response verifications) to continue the experiment execution? The result will be evaluated and the end of the given duration."),
		Type:         action_kit_api.ActionParameterTypePercentage,
		DefaultValue: extutil.Ptr("100"),
		Required:     extutil.Ptr(true),
		MinValue:     extutil.Ptr(0),
		MaxValue:     extutil.Ptr(100),
	}
	maxConcurrent = action_kit_api.ActionParameter{
		Name:         "maxConcurrent",
		Label:        "Max concurrent requests",
		Description:  extutil.Ptr("Maximum count on parallel producing requests. (min 1, max 10)"),
		Type:         action_kit_api.ActionParameterTypeInteger,
		DefaultValue: extutil.Ptr("5"),
		MinValue:     extutil.Ptr(1),
		MaxValue:     extutil.Ptr(10),
		Required:     extutil.Ptr(true),
		Advanced:     extutil.Ptr(true),
	}
)

// FetchTargetPerClient iterates over all configured management endpoints, reusing pooled clients,
// and calls the provided handler. The handler may return zero or more targets. All collected targets from
// all endpoints are concatenated and returned. Individual endpoint errors are logged and do not stop iteration.
func FetchTargetPerClient(fn func(client *rabbithole.Client) ([]discovery_kit_api.Target, error)) ([]discovery_kit_api.Target, error) {
	if len(config.Config.ManagementEndpoints) == 0 {
		return nil, fmt.Errorf("no management endpoints configured")
	}

	all := make([]discovery_kit_api.Target, 0)
	for _, ep := range config.Config.ManagementEndpoints {
		c, ok := clients.GetByMgmtURL(ep.URL)
		if !ok || c == nil || c.Mgmt == nil {
			log.Warn().Str("endpoint", ep.URL).Msg("no pooled management client for endpoint")
			continue
		}
		tgts, err := fn(c.Mgmt)
		if err != nil {
			log.Warn().Err(err).Str("endpoint", ep.URL).Msg("handler returned error for endpoint")
			continue
		}
		all = append(all, tgts...)
	}
	return all, nil
}

type ProduceMessageAttackState struct {
	Vhost                    string
	Exchange                 string
	Queue                    string
	DelayBetweenRequestsInMS uint64
	SuccessRate              int
	Timeout                  time.Time
	MaxConcurrent            int
	RoutingKey               string
	Body                     string
	NumberOfMessages         uint64
	ExecutionID              uuid.UUID
	Headers                  map[string]string
	// AMQP configuration
	AmqpURL                string
	AmqpUser               string
	AmqpPassword           string
	AmqpInsecureSkipVerify bool
	AmqpCA                 string
}

func setEndpointsJSON(eps []config.ManagementEndpoint) {
	b, _ := json.Marshal(eps)
	config.Config.ManagementEndpointsJSON = string(b)
	config.Config.ManagementEndpoints = eps
}

func findTargetByLabel(ts []discovery_kit_api.Target, label string) *discovery_kit_api.Target {
	for i := range ts {
		if ts[i].Label == label {
			return &ts[i]
		}
	}
	return nil
}

func assertAttr(t *testing.T, tgt discovery_kit_api.Target, key, want string) {
	t.Helper()
	vals, ok := tgt.Attributes[key]
	if !ok {
		t.Fatalf("attribute %q missing", key)
	}
	if len(vals) == 0 || vals[0] != want {
		t.Fatalf("attribute %q = %v, want %q", key, vals, want)
	}
}
