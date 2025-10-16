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

// Specification is the configuration specification for the extension. Configuration values can be applied
// through environment variables. Learn more through the documentation of the envconfig package.
// https://github.com/kelseyhightower/envconfig
type ManagementEndpoint struct {
	URL                string `json:"url"`
	Username           string `json:"username,omitempty"`
	Password           string `json:"password,omitempty"`
	InsecureSkipVerify bool   `json:"insecureSkipVerify,omitempty"`
	CAFile             string `json:"caFile,omitempty"`
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
		if u.User != nil {
			if name := u.User.Username(); name != "" {
				ep.Username = name
			}
			if pw, ok := u.User.Password(); ok {
				ep.Password = pw
			}
			u.User = nil
			ep.URL = u.String()
		}
		if ep.CAFile == "" && Config.RabbitClusterCertChainFile != "" {
			ep.CAFile = Config.RabbitClusterCertChainFile
		}
		if !ep.InsecureSkipVerify && Config.InsecureSkipVerify {
			ep.InsecureSkipVerify = true
		}
	}

	Config.ManagementEndpoints = endpoints
}

func ValidateConfiguration() {
	// You may optionally validate the configuration here.
}
