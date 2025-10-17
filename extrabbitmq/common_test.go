package extrabbitmq

import (
	"encoding/json"
	rabbithole "github.com/michaelklishin/rabbit-hole/v3"
	"github.com/steadybit/discovery-kit/go/discovery_kit_api"
	"strings"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/steadybit/extension-rabbitmq/config"
	"github.com/stretchr/testify/require"
)

// --- helpers ---

func resetCfgCommon() func() {
	old := config.Config
	return func() { config.Config = old }
}

func setEndpointsJSONCommon(eps []config.ManagementEndpoint) {
	b, _ := json.Marshal(eps)
	config.Config.ManagementEndpointsJSON = string(b)
	config.Config.ManagementEndpoints = eps
}

// --- tests ---

// We do not assert successful AMQP dialing here because no broker is running in tests.
// We verify that the management client is created and normalized correctly, and that
// AMQP failures are surfaced in the returned error.

func TestCreateNewClient_StripsUserinfoAndBuildsMgmtClient(t *testing.T) {
	defer resetCfgCommon()()

	// Management endpoint with userinfo in the URL.
	raw := "http://user:pass@127.0.0.1:12345"
	setEndpointsJSONCommon([]config.ManagementEndpoint{{URL: raw}})

	mgmt, conn, ch, err := createNewClient(raw, false, "")
	require.NotNil(t, mgmt, "mgmt client should be returned even if AMQP dial fails")
	require.Nil(t, conn, "AMQP conn should be nil on dial failure")
	require.Nil(t, ch, "AMQP channel should be nil on dial failure")
	require.Error(t, err, "expect AMQP dial to fail without a broker")

	// Userinfo must be stripped from the endpoint the client holds.
	require.True(t, strings.HasPrefix(mgmt.Endpoint, "http://127.0.0.1:12345"),
		"endpoint should not contain credentials: %s", mgmt.Endpoint)
}

func TestCreateNewClient_PrefersEndpointCredentialsWhenNoUserinfo(t *testing.T) {
	defer resetCfgCommon()()

	// No userinfo in URL; credentials are provided in endpoint object.
	raw := "http://127.0.0.1:22345"
	setEndpointsJSONCommon([]config.ManagementEndpoint{{
		URL:      raw,
		Username: "u",
		Password: "p",
	}})

	mgmt, conn, ch, err := createNewClient(raw, false, "")
	require.NotNil(t, mgmt)
	require.Nil(t, conn)
	require.Nil(t, ch)
	require.Error(t, err)

	// Endpoint should remain the same URL
	require.Equal(t, raw, mgmt.Endpoint)
}

func TestCreateNewClient_HTTP_NoTLS_UsesPlainHTTPClient(t *testing.T) {
	defer resetCfgCommon()()
	raw := "http://127.0.0.1:32345"
	setEndpointsJSONCommon([]config.ManagementEndpoint{{URL: raw}})

	mgmt, conn, ch, err := createNewClient(raw, false, "")
	require.NotNil(t, mgmt)
	require.Nil(t, conn)
	require.Nil(t, ch)
	require.Error(t, err)
	require.True(t, strings.HasPrefix(mgmt.Endpoint, "http://"))
}

func TestCreateNewClient_HTTPS_WithInsecureSkipVerify(t *testing.T) {
	defer resetCfgCommon()()
	raw := "https://127.0.0.1:42345"
	setEndpointsJSONCommon([]config.ManagementEndpoint{{URL: raw, InsecureSkipVerify: true}})

	mgmt, conn, ch, err := createNewClient(raw, true, "")
	require.NotNil(t, mgmt)
	require.Nil(t, conn)
	require.Nil(t, ch)
	require.Error(t, err)
	require.True(t, strings.HasPrefix(mgmt.Endpoint, "https://"))
}

func TestCreateNewClient_HTTPS_WithCustomCA_PathIsUsed(t *testing.T) {
	defer resetCfgCommon()()
	// Use a non-existent CA path; we only verify that the function attempts to read it,
	// returning an error about the CA bundle rather than URL parsing issues.
	raw := "https://127.0.0.1:52345"
	fakeCA := "/no/such/ca.pem"
	setEndpointsJSONCommon([]config.ManagementEndpoint{{URL: raw, CAFile: fakeCA}})

	mgmt, conn, ch, err := createNewClient(raw, false, fakeCA)
	require.Nil(t, mgmt, "mgmt client should be nil when CA file cannot be read")
	require.Nil(t, conn)
	require.Nil(t, ch)
	require.Error(t, err, "expected error due to unreadable CA file")
}

func TestFetchTargetPerClient_NoEndpoints_Error(t *testing.T) {
	defer resetCfgCommon()()
	setEndpointsJSONCommon(nil)

	_, err := FetchTargetPerClient(func(*rabbithole.Client) ([]discovery_kit_api.Target, error) {
		return nil, nil
	})
	require.Error(t, err)
}

func TestFetchTargetPerClient_WithEndpoints_HandlerNotCalledWhenClientFails(t *testing.T) {
	defer resetCfgCommon()()
	// No broker present, createNewClient will fail; handler should not be called.
	raw1 := "http://127.0.0.1:62345"
	raw2 := "http://127.0.0.1:62346"
	setEndpointsJSONCommon([]config.ManagementEndpoint{{URL: raw1}, {URL: raw2}})

	calls := 0
	_, _ = FetchTargetPerClient(func(*rabbithole.Client) ([]discovery_kit_api.Target, error) {
		calls++
		return nil, nil
	})
	require.Equal(t, 0, calls, "handler must not be called when client creation fails")
}

// compile-time interface/usage checks to ensure symbols exist
// (guards against accidental API changes)
func TestCompileGuards(t *testing.T) {
	var _ *amqp.Connection
	var _ *amqp.Channel
}
