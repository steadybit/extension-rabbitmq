package extrabbitmq

import (
	"encoding/base64"
	"encoding/pem"
	rabbithole "github.com/michaelklishin/rabbit-hole/v3"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/steadybit/discovery-kit/go/discovery_kit_api"
	"github.com/steadybit/extension-rabbitmq/config"
)

// --- helpers ---

func basicAuth(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

func fakeMgmtServer(t *testing.T, requireAuth bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/overview", func(w http.ResponseWriter, r *http.Request) {
		if requireAuth {
			if r.Header.Get("Authorization") == "" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
		}
		io.WriteString(w, `{}`)
	})
	return httptest.NewServer(mux)
}

func fakeMgmtTLSServer(t *testing.T, requireAuth bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/overview", func(w http.ResponseWriter, r *http.Request) {
		if requireAuth {
			if r.Header.Get("Authorization") == "" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
		}
		io.WriteString(w, `{}`)
	})
	return httptest.NewTLSServer(mux)
}

func resetConfig() func() {
	old := config.Config
	return func() { config.Config = old }
}

func writePEM(path string, block *pem.Block) error {
	return os.WriteFile(path, pem.EncodeToMemory(block), 0o600)
}

// --- tests ---

func TestCreateNewClient_HTTP_NoAuth_DefaultScheme(t *testing.T) {
	defer resetConfig()()

	s := fakeMgmtServer(t, false)
	defer s.Close()

	// Pass host without scheme to verify defaulting to http://
	host := strings.TrimPrefix(s.URL, "http://")
	setEndpointsJSON([]config.ManagementEndpoint{{URL: s.URL}})

	c, err := createNewClient(host, false, "")
	if err != nil {
		t.Fatalf("createNewClient error: %v", err)
	}
	if _, err := c.Overview(); err != nil {
		t.Fatalf("Overview failed: %v", err)
	}
}

func TestCreateNewClient_HTTP_BasicAuth_FromEndpoint(t *testing.T) {
	defer resetConfig()()

	s := fakeMgmtServer(t, true)
	defer s.Close()

	// Ensure the exact auth header is sent
	orig := s.Config.Handler
	s.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/overview" {
			if got := r.Header.Get("Authorization"); got != basicAuth("u", "p") {
				http.Error(w, "bad auth", http.StatusUnauthorized)
				return
			}
		}
		orig.ServeHTTP(w, r)
	})

	setEndpointsJSON([]config.ManagementEndpoint{{URL: s.URL, Username: "u", Password: "p"}})

	c, err := createNewClient(s.URL, false, "")
	if err != nil {
		t.Fatalf("createNewClient error: %v", err)
	}
	if _, err := c.Overview(); err != nil {
		t.Fatalf("Overview failed: %v", err)
	}
}

func TestCreateNewClient_HTTP_BasicAuth_FromURL(t *testing.T) {
	defer resetConfig()()
	setEndpointsJSON(nil)

	s := fakeMgmtServer(t, true)
	defer s.Close()

	orig := s.Config.Handler
	s.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/overview" {
			if got := r.Header.Get("Authorization"); got != basicAuth("urluser", "urlpass") {
				http.Error(w, "bad auth", http.StatusUnauthorized)
				return
			}
		}
		orig.ServeHTTP(w, r)
	})

	urlWithCreds := strings.Replace(s.URL, "http://", "http://urluser:urlpass@", 1)

	c, err := createNewClient(urlWithCreds, false, "")
	if err != nil {
		t.Fatalf("createNewClient error: %v", err)
	}
	if _, err := c.Overview(); err != nil {
		t.Fatalf("Overview failed: %v", err)
	}
}

func TestCreateNewClient_HTTPS_InsecureSkipVerify(t *testing.T) {
	defer resetConfig()()

	s := fakeMgmtTLSServer(t, false)
	defer s.Close()

	setEndpointsJSON([]config.ManagementEndpoint{{URL: s.URL, InsecureSkipVerify: true}})

	c, err := createNewClient(s.URL, true, "")
	if err != nil {
		t.Fatalf("createNewClient error: %v", err)
	}
	if _, err := c.Overview(); err != nil {
		t.Fatalf("Overview failed: %v", err)
	}
}

func TestCreateNewClient_HTTPS_CustomCA(t *testing.T) {
	defer resetConfig()()

	s := fakeMgmtTLSServer(t, false)
	defer s.Close()

	tmp := t.TempDir()
	pemPath := tmp + "/ca.pem"

	certDER := s.TLS.Certificates[0].Certificate[0]
	if err := writePEM(pemPath, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		t.Fatalf("writePEM: %v", err)
	}

	setEndpointsJSON([]config.ManagementEndpoint{{URL: s.URL, CAFile: pemPath}})

	c, err := createNewClient(s.URL, false, pemPath)
	if err != nil {
		t.Fatalf("createNewClient error: %v", err)
	}
	if _, err := c.Overview(); err != nil {
		t.Fatalf("Overview failed: %v", err)
	}
}

func TestFetchTargetPerClient_AggregatesFromAllEndpoints(t *testing.T) {
	defer resetConfig()()

	s1 := fakeMgmtServer(t, false)
	defer s1.Close()
	s2 := fakeMgmtServer(t, false)
	defer s2.Close()

	setEndpointsJSON([]config.ManagementEndpoint{
		{URL: s1.URL},
		{URL: s2.URL},
	})

	handler := func(client *rabbithole.Client) ([]discovery_kit_api.Target, error) {
		// Sanity check connectivity
		if _, err := client.Overview(); err != nil {
			return nil, err
		}
		// Return one target per endpoint using the client endpoint as label
		return []discovery_kit_api.Target{{
			Id:         client.Endpoint + "::probe",
			Label:      client.Endpoint,
			TargetType: "test",
			Attributes: map[string][]string{"rabbitmq.mgmt.url": {client.Endpoint}},
		}}, nil
	}

	targets, err := FetchTargetPerClient(handler)
	if err != nil {
		t.Fatalf("FetchTargetPerClient error: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(targets))
	}

	// Validate both endpoints are present
	m := map[string]bool{}
	for _, tgt := range targets {
		m[tgt.Label] = true
	}
	if !m[s1.URL] || !m[s2.URL] {
		t.Fatalf("expected targets for %q and %q, got labels: %+v", s1.URL, s2.URL, m)
	}
}
