// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package config

import (
	"github.com/kelseyhightower/envconfig"
	"github.com/rs/zerolog/log"
)

// Specification is the configuration specification for the extension. Configuration values can be applied
// through environment variables. Learn more through the documentation of the envconfig package.
// https://github.com/kelseyhightower/envconfig
type Specification struct {
	ManagementURL                     string   `json:"managementURL" required:"true" split_words:"true"`
	Username                          string   `json:"username" required:"false" split_words:"true"`
	Password                          string   `json:"password" required:"false" split_words:"true"`
	UseTLS                            string   `json:"useTLS" required:"false" split_words:"true"`
	InsecureSkipVerify                bool     `json:"insecureSkipVerify" required:"false" split_words:"true"`
	RabbitClusterCertChainFile        string   `json:"rabbitClusterCertChainFile" required:"false" split_words:"true"`
	RabbitClusterCertKeyFile          string   `json:"rabbitClusterCertKeyFile" required:"false" split_words:"true"`
	RabbitClusterCaFile               string   `json:"rabbitClusterCaFile" required:"false" split_words:"true"`
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
	err := envconfig.Process("steadybit_extension", &Config)
	if err != nil {
		log.Fatal().Err(err).Msgf("Failed to parse configuration from environment.")
	}
}

func ValidateConfiguration() {
	// You may optionally validate the configuration here.
}
