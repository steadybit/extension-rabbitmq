// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2026 Steadybit GmbH

package e2e

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steadybit/action-kit/go/action_kit_test/e2e"
	"github.com/steadybit/discovery-kit/go/discovery_kit_api"
	"github.com/steadybit/extension-kit/extlogging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	selfSignedCaConfigMap = "rabbitmq-self-signed-ca"
	vhostTargetType       = "com.steadybit.extension_rabbitmq.vhost"
)

// TestWithMinikubeSelfSignedCertificate runs the extension against a management API served over HTTPS with a
// self-signed certificate: it must refuse the connection by default, and accept it once the operator either
// skips verification or mounts the CA. Each configuration gets its own cluster, as the e2e harness cannot
// reconfigure a Deployment in place (it waits on the terminating pod of the previous revision).
//
// Only the management API is covered: AMQPS would need a broker with TLS listeners, and both protocols build
// their TLS configuration through the same applyEndpointTLS, which is unit tested.
func TestWithMinikubeSelfSignedCertificate(t *testing.T) {
	cert, caFile := generateSelfSignedCert(t)
	server := createMockManagementServerWithTLS(cert)
	defer server.http.Close()
	split := strings.SplitAfter(server.http.URL, ":")
	port := split[len(split)-1]

	extlogging.InitZeroLog()

	installCaConfigMap := func(m *e2e.Minikube) error {
		return m.CreateConfigMap("default", selfSignedCaConfigMap, caFile)
	}

	scenarios := []struct {
		name string
		args []string
		test func(t *testing.T, m *e2e.Minikube, e *e2e.Extension)
	}{
		{
			name: "rejects self-signed certificate by default",
			test: testNoVhostsDiscovered(server),
		},
		{
			name: "accepts self-signed certificate with insecureSkipVerify",
			args: []string{"--set", "rabbitmq.insecureSkipVerify=true"},
			test: testMockVhostDiscovered,
		},
		{
			name: "accepts self-signed certificate with mounted CA",
			args: []string{
				"--set", "extraEnv[0].name=SSL_CERT_DIR",
				"--set", "extraEnv[0].value=/etc/ssl/extra-certs:/etc/ssl/certs",
				"--set", "extraVolumeMounts[0].name=extra-certs",
				"--set", "extraVolumeMounts[0].mountPath=/etc/ssl/extra-certs",
				"--set", "extraVolumeMounts[0].readOnly=true",
				"--set", "extraVolumes[0].name=extra-certs",
				"--set", "extraVolumes[0].configMap.name=" + selfSignedCaConfigMap,
			},
			test: testMockVhostDiscovered,
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			// Count only this scenario's handshake errors, so the assertion does not depend on scenario order.
			server.tlsHandshakeErrors.Store(0)
			extFactory := e2e.HelmExtensionFactory{
				Name: "extension-rabbitmq",
				Port: 8083,
				ExtraArgs: func(m *e2e.Minikube) []string {
					endpointsJSON := fmt.Sprintf(
						`[{"url":"https://host.minikube.internal:%s","username":"user","password":"pass","amqp":{"url":"amqps://host.minikube.internal:5671/","vhost":"/"}}]`,
						port)
					return append([]string{
						"--set", "logging.level=debug",
						"--set-json", "rabbitmq.auth.managementEndpoints=" + endpointsJSON,
					}, scenario.args...)
				},
			}
			e2e.WithMinikube(t, e2e.DefaultMinikubeOpts().AfterStart(installCaConfigMap), &extFactory, []e2e.WithMinikubeTestCase{
				{Name: scenario.name, Test: scenario.test},
			})
		})
	}
}

func testMockVhostDiscovered(t *testing.T, _ *e2e.Minikube, e *e2e.Extension) {
	target := pollTarget(t, e, vhostTargetType, func(target discovery_kit_api.Target) bool {
		return e2e.HasAttribute(target, "rabbitmq.vhost.name", mockVhost)
	})
	assert.Equal(t, []string{"rabbit@mock"}, target.Attributes["rabbitmq.cluster.name"])
}

// testNoVhostsDiscovered asserts the extension reached the mock but aborted the TLS handshake, and so
// discovered nothing. A failing discovery may answer with an error or an empty list, so the handshake errors
// are what proves the certificate was rejected. Targets are refreshed on startup, so the window covers the
// first refresh.
func testNoVhostsDiscovered(server *mockManagementServer) func(t *testing.T, m *e2e.Minikube, e *e2e.Extension) {
	return func(t *testing.T, _ *e2e.Minikube, e *e2e.Extension) {
		for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(2 * time.Second) {
			targets, err := e.DiscoverTargets(vhostTargetType)
			if err == nil {
				require.Empty(t, targets, "no vhost must be discovered from a server with an untrusted certificate")
			}
		}
		assert.Positive(t, server.tlsHandshakeErrors.Load(), "the extension must have reached the mock and rejected its certificate")
	}
}

// generateSelfSignedCert returns a certificate valid for host.minikube.internal, and the path of its PEM file
// to mount as CA.
func generateSelfSignedCert(t *testing.T) (tls.Certificate, string) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "host.minikube.internal"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"host.minikube.internal", "localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	require.NoError(t, err)

	caFile := filepath.Join(t.TempDir(), "self-signed-ca.crt")
	require.NoError(t, os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, caFile
}
