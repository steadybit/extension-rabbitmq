// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetEndpointByMgmtURL_ReturnsErrorOnEmptyInput(t *testing.T) {
	// Given
	Config.ManagementEndpoints = []ManagementEndpoint{}

	// When
	ep, err := GetEndpointByMgmtURL("")

	// Then
	require.Error(t, err)
	assert.Nil(t, ep)
	assert.Contains(t, err.Error(), "no endpoint configuration found for amqp url:")
}

func TestGetEndpointByMgmtURL_NotFound(t *testing.T) {
	// Given
	Config.ManagementEndpoints = []ManagementEndpoint{
		{
			URL:      "https://rabbit-a.local:15672",
			Username: "alice",
			Password: "secret",
			AMQP: &AMQPOptions{
				URL:      "amqps://rabbit-a.local:5671",
				Vhost:    "/",
				Username: "alice",
				Password: "secret",
			},
		},
		{
			URL:      "http://rabbit-b.local:15672",
			Username: "bob",
			Password: "s3cr3t",
			AMQP: &AMQPOptions{
				URL:      "amqp://rabbit-b.local:5672",
				Vhost:    "v1",
				Username: "bob",
				Password: "s3cr3t",
			},
		},
	}

	// When
	ep, err := GetEndpointByMgmtURL("http://unknown:15672")

	// Then
	require.Error(t, err)
	assert.Nil(t, ep)
	assert.Equal(t, "no endpoint configuration found for amqp url: http://unknown:15672", err.Error())
}

func TestGetEndpointByMgmtURL_Found(t *testing.T) {
	// Given
	want := ManagementEndpoint{
		URL:      "https://rabbit-a.local:15672",
		Username: "alice",
		Password: "secret",
		AMQP: &AMQPOptions{
			URL:      "amqps://rabbit-a.local:5671",
			Vhost:    "/",
			Username: "alice",
			Password: "secret",
		},
	}
	Config.ManagementEndpoints = []ManagementEndpoint{
		want,
		{
			URL:      "http://rabbit-b.local:15672",
			Username: "bob",
			Password: "s3cr3t",
			AMQP: &AMQPOptions{
				URL:      "amqp://rabbit-b.local:5672",
				Vhost:    "v1",
				Username: "bob",
				Password: "s3cr3t",
			},
		},
	}

	// When
	got, err := GetEndpointByMgmtURL("https://rabbit-a.local:15672")

	// Then
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, want.URL, got.URL)
	assert.Equal(t, want.Username, got.Username)
	assert.Equal(t, want.Password, got.Password)
	require.NotNil(t, got.AMQP)
	assert.Equal(t, want.AMQP.URL, got.AMQP.URL)
	assert.Equal(t, want.AMQP.Vhost, got.AMQP.Vhost)
	assert.Equal(t, want.AMQP.Username, got.AMQP.Username)
	assert.Equal(t, want.AMQP.Password, got.AMQP.Password)
}

func TestGetEndpointByMgmtURL_ExactMatchOnly(t *testing.T) {
	// Given: two endpoints with similar prefixes
	Config.ManagementEndpoints = []ManagementEndpoint{
		{URL: "http://rabbit.local:15672", AMQP: &AMQPOptions{URL: "amqp://rabbit.local:5672", Vhost: "/"}},
		{URL: "http://rabbit.local:15672/api", AMQP: &AMQPOptions{URL: "amqp://rabbit.local:5672/api", Vhost: "/"}},
	}

	// When
	got, err := GetEndpointByMgmtURL("http://rabbit.local:15672")

	// Then
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "http://rabbit.local:15672", got.URL)

	// And: querying the other URL returns that one
	got2, err2 := GetEndpointByMgmtURL("http://rabbit.local:15672/api")
	require.NoError(t, err2)
	require.NotNil(t, got2)
	assert.Equal(t, "http://rabbit.local:15672/api", got2.URL)
}
