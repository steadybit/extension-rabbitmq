package extrabbitmq

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	rabbithole "github.com/michaelklishin/rabbit-hole/v3"
	"github.com/steadybit/discovery-kit/go/discovery_kit_api"
	"github.com/steadybit/extension-rabbitmq/config"
	"github.com/stretchr/testify/assert"
)

// --- Helper functions ---

func mockConfig() {
	config.Config.ManagementEndpoints = []config.ManagementEndpoint{
		{
			URL:                "http://localhost:15672",
			Username:           "guest",
			Password:           "guest",
			InsecureSkipVerify: false,
		},
	}
}

// --- Tests for createNewManagementClient ---

func TestCreateNewManagementClient_HTTP(t *testing.T) {
	mockConfig()
	client, err := createNewManagementClient("http://localhost:15672", false, "")
	assert.NoError(t, err)
	assert.NotNil(t, client)
	assert.IsType(t, &rabbithole.Client{}, client)
}

func TestCreateNewManagementClient_HTTPS_WithCA(t *testing.T) {
	mockConfig()
	tmpFile := filepath.Join(t.TempDir(), "ca.pem")
	os.WriteFile(tmpFile, []byte(testCA), 0644)

	_, err := createNewManagementClient("https://localhost:15671", true, tmpFile)
	assert.Error(t, err) // likely connection error, just check no panic
}

func TestCreateNewManagementClient_InvalidURL(t *testing.T) {
	_, err := createNewManagementClient(":::/badurl", false, "")
	assert.Error(t, err)
}

func TestCreateNewManagementClient_NoEndpoints(t *testing.T) {
	config.Config.ManagementEndpoints = []config.ManagementEndpoint{}
	_, err := createNewManagementClient("", false, "")
	assert.Error(t, err)
}

// --- Tests for createNewAMQPConnection ---

func TestCreateNewAMQPConnection_InvalidScheme(t *testing.T) {
	_, _, err := createNewAMQPConnection("invalid://host", "", "", false, "")
	assert.Error(t, err)
}

func TestCreateNewAMQPConnection_EmptyURL(t *testing.T) {
	_, _, err := createNewAMQPConnection("", "", "", false, "")
	assert.Error(t, err)
}

func TestCreateNewAMQPConnection_InjectCredentials(t *testing.T) {
	url := "amqp://localhost:5672/"
	conn, ch, err := createNewAMQPConnection(url, "guest", "guest", false, "")
	assert.Error(t, err) // No server expected locally
	assert.Nil(t, conn)
	assert.Nil(t, ch)
}

// --- Tests for setEndpointsJSON ---

func TestSetEndpointsJSON(t *testing.T) {
	eps := []config.ManagementEndpoint{
		{URL: "http://localhost:15672", Username: "guest", Password: "guest"},
	}
	setEndpointsJSON(eps)
	assert.NotEmpty(t, config.Config.ManagementEndpointsJSON)
	assert.Contains(t, config.Config.ManagementEndpointsJSON, "localhost:15672")
	assert.Equal(t, eps, config.Config.ManagementEndpoints)
}

// --- Tests for findTargetByLabel ---

func TestFindTargetByLabel(t *testing.T) {
	targets := []discovery_kit_api.Target{
		{Label: "A"},
		{Label: "B"},
	}
	found := findTargetByLabel(targets, "B")
	assert.NotNil(t, found)
	assert.Equal(t, "B", found.Label)
	notFound := findTargetByLabel(targets, "C")
	assert.Nil(t, notFound)
}

// --- Tests for assertAttr ---

func TestAssertAttr(t *testing.T) {
	tgt := discovery_kit_api.Target{
		Attributes: map[string][]string{
			"key": {"value"},
		},
	}
	assertAttr(t, tgt, "key", "value")
}

// --- Tests for FetchTargetPerClient ---

func TestFetchTargetPerClient(t *testing.T) {
	mockConfig()
	handler := func(client *rabbithole.Client) ([]discovery_kit_api.Target, error) {
		return []discovery_kit_api.Target{{Label: "ok"}}, nil
	}
	targets, err := FetchTargetPerClient(handler)
	assert.NoError(t, err)
	assert.NotNil(t, targets)
}

func TestFetchTargetPerClient_HandlerError(t *testing.T) {
	mockConfig()
	handler := func(client *rabbithole.Client) ([]discovery_kit_api.Target, error) {
		return nil, errors.New("test")
	}
	targets, err := FetchTargetPerClient(handler)
	assert.NoError(t, err)
	assert.Empty(t, targets)
}

// --- Test Data ---

const testCA = `
-----BEGIN CERTIFICATE-----
MIIB9TCCAVugAwIBAgIUfQkBkQf5E+j1tB5EpXyRBB2q28EwCgYIKoZIzj0EAwIw
EjEQMA4GA1UEAwwHdGVzdENBMB4XDTI0MDYyMjA5MDUwMFoXDTM0MDYxOTA5MDUw
MFowEjEQMA4GA1UEAwwHdGVzdENBMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE
P0oZl08OGsUm6m5H1RGFtSgGB7LrYMT8M0vP04Xv0qvTVrf0LPPDs/vR8H7ZbOjx
Aq1dOUnJjO1EhP10/6u7wKNtMGswHQYDVR0OBBYEFOp+F8rP1qFXTY9H+F3AJpyH
piZSMB8GA1UdIwQYMBaAFOp+F8rP1qFXTY9H+F3AJpyHpiZSMBIGA1UdEwEB/wQI
MAAwDgYDVR0PAQH/BAQDAgXgMAoGCCqGSM49BAMCA0cAMEQCIG+1baxuP9aDhh5u
fPzIdv4K5uO1D4IUnEw5ZlW7L1VwAiBYmWzq47e0GChpIo1Hz61XKGHGv2Kr7CvT
CdLUdYz7iQ==
-----END CERTIFICATE-----
`
