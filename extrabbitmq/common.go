package extrabbitmq

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/steadybit/discovery-kit/go/discovery_kit_api"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	rabbithole "github.com/michaelklishin/rabbit-hole/v3"
	"github.com/steadybit/extension-rabbitmq/config"
)

const (
	kafkaBrokerTargetId       = "com.steadybit.extension_kafka.broker"
	kafkaConsumerTargetId     = "com.steadybit.extension_kafka.consumer"
	kafkaTopicTargetId        = "com.steadybit.extension_kafka.topic"
	kafkaIcon                 = "data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZlcnNpb249IjEuMSIgdmlld0JveD0iMCAwIDI0IDI0Ij4KICA8cGF0aAogICAgZD0iTTE1LjksMTMuMmMtLjksMC0xLjYuNC0yLjIsMWwtMS4zLTFjLjEtLjQuMi0uOC4yLTEuM3MwLS45LS4yLTEuMmwxLjMtLjljLjUuNiwxLjMsMSwyLjEsMSwxLjYsMCwyLjktMS4zLDIuOS0yLjlzLTEuMy0yLjktMi45LTIuOS0yLjksMS4zLTIuOSwyLjksMCwuNi4xLjhsLTEuMy45Yy0uNi0uNy0xLjQtMS4yLTIuMy0xLjN2LTEuNmMxLjMtLjMsMi4zLTEuNCwyLjMtMi44LDAtMS42LTEuMy0yLjktMi45LTIuOXMtMi45LDEuMy0yLjksMi45LDEsMi41LDIuMiwyLjh2MS42Yy0xLjcuMy0zLjEsMS44LTMuMSwzLjZzMS4zLDMuNCwzLjEsMy42djEuN2MtMS4zLjMtMi4zLDEuNC0yLjMsMi44czEuMywyLjksMi45LDIuOSwyLjktMS4zLDIuOS0yLjktMS0yLjUtMi4zLTIuOHYtMS43Yy45LS4xLDEuNy0uNiwyLjMtMS4zbDEuNCwxYzAsLjMtLjEuNS0uMS44LDAsMS42LDEuMywyLjksMi45LDIuOXMyLjktMS4zLDIuOS0yLjktMS4zLTIuOS0yLjktMi45aDBaTTE1LjksNi41Yy44LDAsMS40LjYsMS40LDEuNHMtLjYsMS40LTEuNCwxLjQtMS40LS42LTEuNC0xLjQuNi0xLjQsMS40LTEuNGgwWk03LjUsMy45YzAtLjguNi0xLjQsMS40LTEuNHMxLjQuNiwxLjQsMS40LS42LDEuNC0xLjQsMS40LTEuNC0uNi0xLjQtMS40aDBaTTEwLjMsMjAuMWMwLC44LS42LDEuNC0xLjQsMS40cy0xLjQtLjYtMS40LTEuNC42LTEuNCwxLjQtMS40LDEuNC42LDEuNCwxLjRaTTguOSwxMy45Yy0xLjEsMC0xLjktLjktMS45LTEuOXMuOS0xLjksMS45LTEuOSwxLjkuOSwxLjksMS45LS45LDEuOS0xLjksMS45Wk0xNS45LDE3LjRjLS44LDAtMS40LS42LTEuNC0xLjRzLjYtMS40LDEuNC0xLjQsMS40LjYsMS40LDEuNC0uNiwxLjQtMS40LDEuNFoiCiAgICBmaWxsPSJjdXJyZW50Q29sb3IiIC8+Cjwvc3ZnPg=="
	stateCheckModeAtLeastOnce = "atLeastOnce"
	stateCheckModeAllTheTime  = "allTheTime"
)

func createNewClient(host string, insecureSkipVerify bool, caFile string) (*rabbithole.Client, error) {
	// host may be empty -> use first configured endpoint
	if host == "" {
		if len(config.Config.ManagementEndpoints) == 0 {
			return nil, fmt.Errorf("no management endpoints configured")
		}
		host = config.Config.ManagementEndpoints[0].URL
	}

	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		host = "http://" + host
	}

	u, err := url.Parse(host)
	if err != nil {
		return nil, fmt.Errorf("invalid host URL: %w", err)
	}

	// if TLS settings not provided explicitly use provided params
	insecure := insecureSkipVerify
	ca := strings.TrimSpace(caFile)

	// If URL contains userinfo it will be preferred by rabbit-hole NewClient calls via credentials params
	username := ""
	password := ""
	if u.User != nil {
		if n := u.User.Username(); n != "" {
			username = n
		}
		if pw, ok := u.User.Password(); ok {
			password = pw
		}
		u.User = nil
		host = u.String()
	}

	// If endpoint is configured, prefer its credentials and TLS settings when caller passed empty values
	for i := range config.Config.ManagementEndpoints {
		ep := &config.Config.ManagementEndpoints[i]
		if epURL, err := url.Parse(ep.URL); err == nil && epURL.Host == u.Host {
			if username == "" {
				username = ep.Username
			}
			if password == "" {
				password = ep.Password
			}
			// endpoint-level TLS overrides caller provided ones unless caller explicitly passed them
			if ca == "" && ep.CAFile != "" {
				ca = ep.CAFile
			}
			if !insecure && ep.InsecureSkipVerify {
				insecure = true
			}
			break
		}
	}

	// If HTTP and no TLS customization, use plain client
	if u.Scheme == "http" && !insecure && ca == "" {
		return rabbithole.NewClient(u.String(), username, password)
	}

	// Build TLS config
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if insecure {
		tlsCfg.InsecureSkipVerify = true
	}
	if ca != "" {
		pemBytes, readErr := os.ReadFile(ca)
		if readErr != nil {
			return nil, fmt.Errorf("read CA bundle: %w", readErr)
		}
		pool := x509.NewCertPool()
		if ok := pool.AppendCertsFromPEM(pemBytes); !ok {
			return nil, fmt.Errorf("failed to parse CA bundle in %s", ca)
		}
		tlsCfg.RootCAs = pool
	}

	transport := &http.Transport{TLSClientConfig: tlsCfg}
	return rabbithole.NewTLSClient(u.String(), username, password, transport)
}

// FetchTargetPerClient iterates over all configured management endpoints, creates a client for each
// and calls the provided handler. The handler may return zero or more targets. All collected targets from
// all endpoints are concatenated and returned. Individual endpoint errors are logged and do not stop iteration.
func FetchTargetPerClient(fn func(client *rabbithole.Client) ([]discovery_kit_api.Target, error)) ([]discovery_kit_api.Target, error) {
	if len(config.Config.ManagementEndpoints) == 0 {
		return nil, fmt.Errorf("no management endpoints configured")
	}

	all := make([]discovery_kit_api.Target, 0)
	for _, ep := range config.Config.ManagementEndpoints {
		client, err := createNewClient(ep.URL, ep.InsecureSkipVerify, ep.CAFile)
		if err != nil {
			log.Warn().Err(err).Str("endpoint", ep.URL).Msg("failed to create management client for endpoint")
			continue
		}
		tgts, err := fn(client)
		if err != nil {
			log.Warn().Err(err).Str("endpoint", ep.URL).Msg("handler returned error for endpoint")
			continue
		}
		all = append(all, tgts...)
	}
	return all, nil
}

type ProduceMessageAttackState struct {
	Queue                    string
	DelayBetweenRequestsInMS int64
	SuccessRate              int
	Timeout                  time.Time
	MaxConcurrent            int
	RecordKey                string
	RecordValue              string
	RecordPartition          int
	NumberOfRecords          uint64
	ExecutionID              uuid.UUID
	RecordHeaders            map[string]string
	ConsumerGroup            string
	BrokerHosts              []string
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
