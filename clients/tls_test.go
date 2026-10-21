// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2026 Steadybit GmbH

package clients

import (
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mgmtTransport(t *testing.T, url string, insecure bool, ca string) *http.Transport {
	rt, err := newMgmtTransport(createConfig(url, "user", "pass", insecure, ca))
	require.NoError(t, err)
	base, ok := rt.(*retryTransport).base.(*http.Transport)
	require.True(t, ok)
	return base
}

func newSelfSignedServer(t *testing.T) *httptest.Server {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	return server
}

func get(t *testing.T, transport *http.Transport, url string) error {
	resp, err := (&http.Client{Transport: transport}).Get(url)
	if err == nil {
		_ = resp.Body.Close()
	}
	return err
}

func TestNewMgmtTransport_HTTPS_keeps_standard_transport_settings_and_system_pool(t *testing.T) {
	t.Setenv("STEADYBIT_EXTENSION_INSECURE_SKIP_VERIFY", "")

	transport := mgmtTransport(t, "https://rabbit:15671", false, "")

	assert.NotNil(t, transport.Proxy, "proxy from environment must be kept")
	assert.NotNil(t, transport.DialContext, "dial timeout must be kept")
	assert.Nil(t, transport.TLSClientConfig.RootCAs, "the system pool must be used, so SSL_CERT_DIR is honoured")
	assert.False(t, transport.TLSClientConfig.InsecureSkipVerify)
	assert.GreaterOrEqual(t, transport.TLSClientConfig.MinVersion, uint16(0x0303))
}

func TestNewMgmtTransport_HTTPS_rejects_self_signed_by_default(t *testing.T) {
	t.Setenv("STEADYBIT_EXTENSION_INSECURE_SKIP_VERIFY", "")
	server := newSelfSignedServer(t)

	assert.ErrorContains(t, get(t, mgmtTransport(t, server.URL, false, ""), server.URL), "certificate")
}

func TestNewMgmtTransport_HTTPS_skips_verification_per_endpoint_or_globally(t *testing.T) {
	server := newSelfSignedServer(t)

	t.Setenv("STEADYBIT_EXTENSION_INSECURE_SKIP_VERIFY", "")
	assert.NoError(t, get(t, mgmtTransport(t, server.URL, true, ""), server.URL))

	t.Setenv("STEADYBIT_EXTENSION_INSECURE_SKIP_VERIFY", "true")
	assert.NoError(t, get(t, mgmtTransport(t, server.URL, false, ""), server.URL))
}

func TestNewMgmtTransport_HTTPS_trusts_endpoint_ca_file(t *testing.T) {
	t.Setenv("STEADYBIT_EXTENSION_INSECURE_SKIP_VERIFY", "")
	server := newSelfSignedServer(t)
	caFile := filepath.Join(t.TempDir(), "ca.crt")
	require.NoError(t, os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0o600))

	assert.NoError(t, get(t, mgmtTransport(t, server.URL, false, caFile), server.URL))
}
