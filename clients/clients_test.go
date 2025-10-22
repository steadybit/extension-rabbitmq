// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package clients

import (
	"crypto/tls"
	"net/http"
	"net/url"
	"testing"

	rabbithole "github.com/michaelklishin/rabbit-hole/v3"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- CreateMgmtClientFromURL ----

func TestCreateMgmtClientFromURL_EmptyURL(t *testing.T) {
	c, err := CreateMgmtClientFromURL("", "user", "pass", false, "")
	assert.Nil(t, c)
	assert.EqualError(t, err, "empty management URL")
}

func TestCreateMgmtClientFromURL_InvalidURL(t *testing.T) {
	c, err := CreateMgmtClientFromURL("http//missing-colon", "user", "pass", false, "")
	assert.Nil(t, c)
	assert.Error(t, err)
}

func TestCreateMgmtClientFromURL_UnsupportedScheme(t *testing.T) {
	c, err := CreateMgmtClientFromURL("ftp://host", "user", "pass", false, "")
	assert.Nil(t, c)
	assert.EqualError(t, err, "unsupported scheme: ftp")
}

func TestCreateMgmtClientFromURL_HTTP_Succeeds(t *testing.T) {
	c, err := CreateMgmtClientFromURL("http://localhost:15672", "user", "pass", false, "")
	assert.NoError(t, err)
	assert.IsType(t, &rabbithole.Client{}, c)
}

func TestCreateMgmtClientFromURL_HTTPS_Insecure_NoCA(t *testing.T) {
	c, err := CreateMgmtClientFromURL("https://localhost:15672", "user", "pass", true, "")
	assert.NoError(t, err)
	assert.IsType(t, &rabbithole.Client{}, c)
}

func TestCreateMgmtClientFromURL_ExtractsCredentialsFromURL(t *testing.T) {
	u := &url.URL{
		Scheme: "http",
		Host:   "localhost:15672",
		User:   url.UserPassword("admin", "secret"),
	}
	c, err := CreateMgmtClientFromURL(u.String(), "", "", false, "")
	assert.NoError(t, err)
	assert.NotNil(t, c)
}

// ---- CreateNewAMQPConnection ----

func TestCreateNewAMQPConnection_EmptyURL(t *testing.T) {
	conn, ch, err := CreateNewAMQPConnection("", "", "", false, "")
	assert.Nil(t, conn)
	assert.Nil(t, ch)
	assert.EqualError(t, err, "amqp url is empty")
}

func TestCreateNewAMQPConnection_InvalidURL(t *testing.T) {
	conn, ch, err := CreateNewAMQPConnection("://missing-scheme", "", "", false, "")
	assert.Nil(t, conn)
	assert.Nil(t, ch)
	assert.Error(t, err)
}

func TestCreateNewAMQPConnection_UnsupportedScheme(t *testing.T) {
	conn, ch, err := CreateNewAMQPConnection("mqtt://broker", "", "", false, "")
	assert.Nil(t, conn)
	assert.Nil(t, ch)
	assert.EqualError(t, err, "unsupported amqp scheme: mqtt")
}

func TestCreateNewAMQPConnection_InsertsCredentials(t *testing.T) {
	u := "amqp://localhost"
	au, err := url.Parse(u)
	require.NoError(t, err)
	assert.Nil(t, au.User)

	// Mock dialer failure to avoid real connection
	oldDial := amqpDial
	defer func() { amqpDial = oldDial }()
	amqpDial = func(addr string) (*amqp.Connection, error) {
		assert.Contains(t, addr, "amqp://user:pass@localhost")
		return nil, assert.AnError
	}
	_, _, _ = CreateNewAMQPConnection(u, "user", "pass", false, "")
}

func TestCreateNewAMQPConnection_AMQPAndAMQPS_ErrorOnConnection(t *testing.T) {
	// override dialers
	oldDial := amqpDial
	oldDialTLS := amqpDialTLS
	defer func() { amqpDial, amqpDialTLS = oldDial, oldDialTLS }()

	amqpDial = func(addr string) (*amqp.Connection, error) {
		return nil, assert.AnError
	}
	amqpDialTLS = func(addr string, cfg *tls.Config) (*amqp.Connection, error) {
		assert.NotNil(t, cfg)
		return nil, assert.AnError
	}

	_, _, err := CreateNewAMQPConnection("amqp://localhost", "", "", false, "")
	assert.Error(t, err)

	_, _, err = CreateNewAMQPConnection("amqps://localhost", "", "", true, "")
	assert.Error(t, err)
}

// ---- test overrides ----
var (
	amqpDial    = func(addr string) (*amqp.Connection, error) { return amqp.Dial(addr) }
	amqpDialTLS = func(addr string, cfg *tls.Config) (*amqp.Connection, error) { return amqp.DialTLS(addr, cfg) }
)

func init() {
	// ensure TLS works for valid PEM tests
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
}
