// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2026 Steadybit GmbH

package e2e

import (
	"crypto/tls"
	"encoding/json"
	stdlog "log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"

	"github.com/rs/zerolog/log"
)

const mockVhost = "self-signed"

// mockManagementServer serves the part of the RabbitMQ management API that the vhost discovery uses, over
// HTTPS with the given certificate.
type mockManagementServer struct {
	http *httptest.Server
	// tlsHandshakeErrors counts handshakes the client aborted, e.g. because it does not trust the certificate.
	tlsHandshakeErrors atomic.Int64
}

// errorLogWriter forwards the mock's server error log to stderr and counts TLS handshake errors.
type errorLogWriter struct{ mock *mockManagementServer }

func (w errorLogWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), "TLS handshake error") {
		w.mock.tlsHandshakeErrors.Add(1)
	}
	return os.Stderr.Write(p)
}

func createMockManagementServerWithTLS(cert tls.Certificate) *mockManagementServer {
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		panic(err)
	}
	mock := &mockManagementServer{}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/vhosts", writeJSON([]map[string]any{{"name": mockVhost, "tracing": false}}))
	clusterName := writeJSON(map[string]any{"name": "rabbit@mock"})
	mux.HandleFunc("GET /api/cluster-name", clusterName)
	mux.HandleFunc("GET /api/cluster-name/", clusterName)
	mux.HandleFunc("GET /api/", writeJSON([]any{}))

	server := httptest.Server{
		Listener: listener,
		Config:   &http.Server{Handler: mux, ErrorLog: stdlog.New(errorLogWriter{mock}, "", stdlog.LstdFlags)},
		TLS:      &tls.Config{Certificates: []tls.Certificate{cert}},
	}
	server.StartTLS()
	mock.http = &server
	log.Info().Str("url", server.URL).Msg("Started RabbitMQ management mock")
	return mock
}

func writeJSON(body any) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}
}
