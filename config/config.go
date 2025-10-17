// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package config

import (
	"encoding/json"
	"github.com/kelseyhightower/envconfig"
	"github.com/rs/zerolog/log"
	"net/url"
	"strings"
)

type AMQPOptions struct {
	URL                string `json:"url,omitempty"`
	Vhost              string `json:"vhost,omitempty"`
	Username           string `json:"username,omitempty"`
	Password           string `json:"password,omitempty"`
	InsecureSkipVerify bool   `json:"insecureSkipVerify,omitempty"`
	CAFile             string `json:"caFile,omitempty"`
}

// Specification is the configuration specification for the extension. Configuration values can be applied
// through environment variables. Learn more through the documentation of the envconfig package.
// https://github.com/kelseyhightower/envconfig
type ManagementEndpoint struct {
	URL                string       `json:"url"`
	Username           string       `json:"username,omitempty"`
	Password           string       `json:"password,omitempty"`
	InsecureSkipVerify bool         `json:"insecureSkipVerify,omitempty"`
	CAFile             string       `json:"caFile,omitempty"`
	AMQP               *AMQPOptions `json:"amqp,omitempty"`
}

type Specification struct {
	// Required: structured endpoints as JSON (array of ManagementEndpoint)
	ManagementEndpointsJSON string               `json:"managementEndpointsJson" split_words:"true" required:"true"`
	ManagementEndpoints     []ManagementEndpoint `json:"-"`

	// Optional global TLS defaults applied when an endpoint omits its own
	InsecureSkipVerify         bool   `json:"insecureSkipVerify" required:"false" split_words:"true"`
	RabbitClusterCertChainFile string `json:"rabbitClusterCertChainFile" required:"false" split_words:"true"`
	RabbitClusterCertKeyFile   string `json:"rabbitClusterCertKeyFile" required:"false" split_words:"true"`
	RabbitClusterCaFile        string `json:"rabbitClusterCaFile" required:"false" split_words:"true"`

	// Discovery
	DiscoveryIntervalConsumerGroup    int      `json:"discoveryIntervalrabbitConsumerGroup" split_words:"true" required:"false" default:"30"`
	DiscoveryIntervalrabbitBroker     int      `json:"discoveryIntervalrabbitBroker" split_words:"true" required:"false" default:"30"`
	DiscoveryIntervalrabbitTopic      int      `json:"discoveryIntervalrabbitTopic" split_words:"true" required:"false" default:"30"`
	DiscoveryAttributesExcludesVhosts []string `json:"discoveryAttributesExcludesVhosts" split_words:"true" required:"false"`
	DiscoveryAttributesExcludesQueues []string `json:"discoveryAttributesExcludesQueues" split_words:"true" required:"false"`
}

var (
	Config Specification
)

func ParseConfiguration() {
	if err := envconfig.Process("steadybit_extension", &Config); err != nil {
		log.Fatal().Err(err).Msgf("Failed to parse configuration from environment.")
	}

	// Require a JSON array of endpoints
	s := strings.TrimSpace(Config.ManagementEndpointsJSON)
	if s == "" {
		log.Fatal().Msg("STEADYBIT_EXTENSION_MANAGEMENT_ENDPOINTS_JSON is required and must be a JSON array of endpoints")
	}

	var endpoints []ManagementEndpoint
	if err := json.Unmarshal([]byte(s), &endpoints); err != nil {
		log.Fatal().Err(err).Msg("Invalid STEADYBIT_EXTENSION_MANAGEMENT_ENDPOINTS_JSON")
	}

	// Normalize: extract userinfo from URL and apply optional global TLS defaults
	for i := range endpoints {
		ep := &endpoints[i]
		u, err := url.Parse(ep.URL)
		if err != nil {
			log.Fatal().Err(err).Str("url", ep.URL).Msg("Invalid management endpoint URL")
		}
		if strings.TrimSpace(ep.URL) == "" {
			log.Fatal().Msg("management endpoint 'url' is required in each object")
		}
		isHTTPS := u.Scheme == "https"
		if ep.CAFile == "" && isHTTPS && Config.RabbitClusterCertChainFile != "" {
			ep.CAFile = Config.RabbitClusterCertChainFile
		}
		if !ep.InsecureSkipVerify && isHTTPS && Config.InsecureSkipVerify {
			ep.InsecureSkipVerify = true
		}

		// --- AMQP required & normalization ---
		if ep.AMQP == nil {
			log.Fatal().Str("management_url", ep.URL).Msg("amqp object is required per endpoint")
		}
		if strings.TrimSpace(ep.AMQP.URL) == "" {
			log.Fatal().Str("management_url", ep.URL).Msg("amqp.url is required per endpoint")
		}
		if strings.TrimSpace(ep.AMQP.Vhost) == "" {
			log.Fatal().Str("management_url", ep.URL).Msg("amqp.vhost is required per endpoint")
		}

		// If AMQP URL contains userinfo, extract it and strip from URL
		if ep.AMQP.URL != "" {
			amqpURL, err := url.Parse(ep.AMQP.URL)
			if err != nil {
				log.Fatal().Err(err).Str("url", ep.AMQP.URL).Msg("Invalid AMQP endpoint URL")
			}
			if amqpURL.User != nil {
				if name := amqpURL.User.Username(); name != "" {
					ep.AMQP.Username = name
				}
				if pw, ok := amqpURL.User.Password(); ok {
					ep.AMQP.Password = pw
				}
				amqpURL.User = nil
				ep.AMQP.URL = amqpURL.String()
			}
			isAMQPS := amqpURL.Scheme == "amqps"
			if ep.AMQP.CAFile == "" && isAMQPS && Config.RabbitClusterCertChainFile != "" {
				ep.AMQP.CAFile = Config.RabbitClusterCertChainFile
			}
			if !ep.AMQP.InsecureSkipVerify && isAMQPS && Config.InsecureSkipVerify {
				ep.AMQP.InsecureSkipVerify = true
			}
		}

		// Inherit credentials from management if missing
		if ep.AMQP.Username == "" {
			ep.AMQP.Username = ep.Username
		}
		if ep.AMQP.Password == "" {
			ep.AMQP.Password = ep.Password
		}

		// Apply global TLS defaults if AMQP omits its own
		if ep.AMQP.CAFile == "" && Config.RabbitClusterCertChainFile != "" {
			ep.AMQP.CAFile = Config.RabbitClusterCertChainFile
		}
		if !ep.AMQP.InsecureSkipVerify && Config.InsecureSkipVerify {
			ep.AMQP.InsecureSkipVerify = true
		}
	}

	Config.ManagementEndpoints = endpoints
}

func ValidateConfiguration() {
	// You may optionally validate the configuration here.
}

// GetEndpointByAMQPURL returns the management endpoint configuration matching the provided AMQP URL.
// It compares against the AMQP.URL field of each configured ManagementEndpoint.
func GetEndpointByAMQPURL(amqpURL string) (*ManagementEndpoint, bool) {
	if strings.TrimSpace(amqpURL) == "" {
		return nil, false
	}
	for i := range Config.ManagementEndpoints {
		ep := &Config.ManagementEndpoints[i]
		if ep.AMQP != nil && ep.AMQP.URL == amqpURL {
			return ep, true
		}
	}
	return nil, false
}
