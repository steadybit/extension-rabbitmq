// common_test.go
package extrabbitmq

import (
	"context"
	"encoding/base64"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

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
			h := r.Header.Get("Authorization")
			if h == "" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
		}
		// minimal valid JSON for Overview()
		io.WriteString(w, `{}`)
	})
	return httptest.NewServer(mux)
}

func fakeMgmtTLSServer(t *testing.T, requireAuth bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/overview", func(w http.ResponseWriter, r *http.Request) {
		if requireAuth {
			h := r.Header.Get("Authorization")
			if h == "" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
		}
		io.WriteString(w, `{}`)
	})
	s := httptest.NewTLSServer(mux)
	return s
}

func withTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

// reset config between tests
func resetConfig() func() {
	old := config.Config
	return func() { config.Config = old }
}

// --- tests ---

func TestCreateNewClient_HTTP_NoAuth_DefaultScheme(t *testing.T) {
	defer resetConfig()()
	config.Config.Username = ""
	config.Config.Password = ""

	s := fakeMgmtServer(t, false)
	defer s.Close()

	// strip scheme to verify we default to http://
	host := strings.TrimPrefix(s.URL, "http://")

	c, err := createNewClient(host)
	if err != nil {
		t.Fatalf("createNewClient error: %v", err)
	}

	// call a real API method to verify connectivity
	_, err = c.Overview()
	if err != nil {
		t.Fatalf("Overview failed: %v", err)
	}
}

func TestCreateNewClient_HTTP_BasicAuth_FromConfig(t *testing.T) {
	defer resetConfig()()
	config.Config.Username = "u"
	config.Config.Password = "p"

	// require any Authorization header
	s := fakeMgmtServer(t, true)
	defer s.Close()

	// add a check that header is present and matches
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

	c, err := createNewClient(s.URL)
	if err != nil {
		t.Fatalf("createNewClient error: %v", err)
	}
	if _, err := c.Overview(); err != nil {
		t.Fatalf("Overview failed: %v", err)
	}
}

func TestCreateNewClient_HTTP_BasicAuth_FromURL(t *testing.T) {
	defer resetConfig()()
	config.Config.Username = ""
	config.Config.Password = ""

	s := fakeMgmtServer(t, true)
	defer s.Close()

	// inject our own handler to validate the exact basic header for url creds
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

	// put creds into the URL
	urlWithCreds := strings.Replace(s.URL, "http://", "http://urluser:urlpass@", 1)

	c, err := createNewClient(urlWithCreds)
	if err != nil {
		t.Fatalf("createNewClient error: %v", err)
	}
	if _, err := c.Overview(); err != nil {
		t.Fatalf("Overview failed: %v", err)
	}
}

func TestCreateNewClient_HTTPS_InsecureSkipVerify(t *testing.T) {
	defer resetConfig()()
	config.Config.InsecureSkipVerify = true
	config.Config.RabbitClusterCertChainFile = ""

	s := fakeMgmtTLSServer(t, false)
	defer s.Close()

	c, err := createNewClient(s.URL)
	if err != nil {
		t.Fatalf("createNewClient error: %v", err)
	}
	if _, err := c.Overview(); err != nil {
		t.Fatalf("Overview failed: %v", err)
	}
}

func TestCreateNewClient_HTTPS_CustomCA(t *testing.T) {
	defer resetConfig()()
	config.Config.InsecureSkipVerify = false

	s := fakeMgmtTLSServer(t, false)
	defer s.Close()

	// write server cert to a temp PEM file and point the config at it
	tmp := t.TempDir()
	pemPath := tmp + "/ca.pem"

	// extract leaf cert from httptest server and write as PEM
	certDER := s.TLS.Certificates[0].Certificate[0]
	block := &pem.Block{Type: "CERTIFICATE", Bytes: certDER}
	if err := writePEM(pemPath, block); err != nil {
		t.Fatalf("writePEM: %v", err)
	}
	config.Config.RabbitClusterCertChainFile = pemPath

	c, err := createNewClient(s.URL)
	if err != nil {
		t.Fatalf("createNewClient error: %v", err)
	}
	if _, err := c.Overview(); err != nil {
		t.Fatalf("Overview failed: %v", err)
	}
}

// --- tiny util to write a PEM file ---
func writePEM(path string, block *pem.Block) error {
	return osWriteFile(path, pem.EncodeToMemory(block))
}

// indirection to keep imports minimal in tests
var osWriteFile = func(name string, data []byte) error {
	return os.WriteFile(name, data, 0o600)
}
