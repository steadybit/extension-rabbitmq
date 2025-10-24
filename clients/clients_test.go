// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package clients

import (
	"crypto/tls"
	"errors"
	"github.com/steadybit/extension-rabbitmq/config"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	rabbithole "github.com/michaelklishin/rabbit-hole/v3"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- helpers used by tests ----

func createConfig(url string, user string, pass string, insecure bool, ca string) *config.ManagementEndpoint {
	return &config.ManagementEndpoint{
		URL:                url,
		Username:           user,
		Password:           pass,
		InsecureSkipVerify: insecure,
		CAFile:             ca,
	}
}

// ---- CreateMgmtClientFromURL ----

func TestCreateMgmtClientFromURL_EmptyURL(t *testing.T) {
	c, err := CreateMgmtClientFromURL(createConfig("", "user", "pass", false, ""))
	assert.Nil(t, c)
	assert.EqualError(t, err, "empty management URL")
}

func TestCreateMgmtClientFromURL_InvalidURL(t *testing.T) {
	c, err := CreateMgmtClientFromURL(createConfig("http//missing-colon", "user", "pass", false, ""))
	assert.Nil(t, c)
	assert.Error(t, err)
}

func TestCreateMgmtClientFromURL_UnsupportedScheme(t *testing.T) {
	c, err := CreateMgmtClientFromURL(createConfig("ftp://host", "user", "pass", false, ""))
	assert.Nil(t, c)
	assert.EqualError(t, err, "unsupported scheme: ftp")
}

func TestCreateMgmtClientFromURL_HTTP_Succeeds(t *testing.T) {
	c, err := CreateMgmtClientFromURL(createConfig("http://localhost:15672", "user", "pass", false, ""))
	assert.NoError(t, err)
	assert.IsType(t, &rabbithole.Client{}, c)
}

func TestCreateMgmtClientFromURL_HTTPS_Insecure_NoCA(t *testing.T) {
	c, err := CreateMgmtClientFromURL(createConfig("https://localhost:15672", "user", "pass", true, ""))
	assert.NoError(t, err)
	assert.IsType(t, &rabbithole.Client{}, c)
}

func TestCreateMgmtClientFromURL_ExtractsCredentialsFromURL(t *testing.T) {
	u := &url.URL{
		Scheme: "http",
		Host:   "localhost:15672",
		User:   url.UserPassword("admin", "secret"),
	}
	c, err := CreateMgmtClientFromURL(createConfig(u.String(), "", "", false, ""))
	assert.NoError(t, err)
	assert.NotNil(t, c)
}

func TestCreateMgmtClientFromURL_HTTPS_WithInvalidCAFile_ReadError(t *testing.T) {
	// file does not exist -> read error
	tmp := filepath.Join(os.TempDir(), "nonexistent-ca.pem")
	cfg := createConfig("https://localhost:15672", "user", "pass", false, tmp)
	c, err := CreateMgmtClientFromURL(cfg)
	assert.Nil(t, c)
	require.Error(t, err)
	// error message should contain the file path
	assert.Contains(t, err.Error(), tmp)
}

func TestCreateMgmtClientFromURL_HTTPS_WithBadPEM(t *testing.T) {
	// create a temp file with invalid PEM
	f, err := os.CreateTemp("", "badca-*.pem")
	require.NoError(t, err)
	_, _ = f.WriteString("this is not a pem")
	_ = f.Close()
	defer os.Remove(f.Name())

	cfg := createConfig("https://localhost:15672", "user", "pass", false, f.Name())
	c, err := CreateMgmtClientFromURL(cfg)
	assert.Nil(t, c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid CA")
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

func TestCreateNewAMQPConnection_AMQPAndAMQPS_ErrorOnConnection(t *testing.T) {
	// override dialers
	oldDial := amqpDial
	oldDialTLS := amqpDialTLS
	defer func() { amqpDial, amqpDialTLS = oldDial, oldDialTLS }()

	amqpDial = func(addr string) (*amqp.Connection, error) {
		return nil, errors.New("amqp dial error")
	}
	amqpDialTLS = func(addr string, cfg *tls.Config) (*amqp.Connection, error) {
		// TLS config should be provided for amqps
		require.NotNil(t, cfg)
		return nil, errors.New("amqps dial error")
	}

	_, _, err := CreateNewAMQPConnection("amqp://localhost", "", "", false, "")
	assert.Error(t, err)

	_, _, err = CreateNewAMQPConnection("amqps://localhost", "", "", true, "")
	assert.Error(t, err)
}

func TestCreateNewAMQPConnection_AMQPS_WithInvalidCAFile(t *testing.T) {
	oldDialTLS := amqpDialTLS
	defer func() { amqpDialTLS = oldDialTLS }()

	// prepare a bad CA file
	f, err := os.CreateTemp("", "invalid-ca-*.pem")
	require.NoError(t, err)
	_, _ = f.WriteString("not a pem")
	_ = f.Close()
	defer os.Remove(f.Name())

	// amqpDialTLS shouldn't be called because CA parsing fails earlier
	amqpDialTLS = func(addr string, cfg *tls.Config) (*amqp.Connection, error) {
		return nil, errors.New("should not be called")
	}

	_, _, err = CreateNewAMQPConnection("amqps://localhost", "", "", false, f.Name())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid CA")
}

func TestCreateNewAMQPConnection_AMQPS_WithCAFile_ReadError(t *testing.T) {
	// non existent file
	_, _, err := CreateNewAMQPConnection("amqps://localhost", "", "", false, "/nonexistent/path/ca.pem")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no such file")
}

// ---- test overrides used by tests (these must match the package-level variables in clients.go) ----
var (
	amqpDial    = func(addr string) (*amqp.Connection, error) { return amqp.Dial(addr) }
	amqpDialTLS = func(addr string, cfg *tls.Config) (*amqp.Connection, error) { return amqp.DialTLS(addr, cfg) }
)

func init() {
	// ensure TLS works for valid-ish tests (if they rely on default transport)
	if tr, ok := http.DefaultTransport.(*http.Transport); ok {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
}
