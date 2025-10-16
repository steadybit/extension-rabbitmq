package extrabbitmq

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/steadybit/extension-rabbitmq/config"
)

// --- helpers ---

type vhost struct {
	Name    string `json:"name"`
	Tracing bool   `json:"tracing"`
}

func fakeMgmtVhostServer(t *testing.T, vhosts []vhost) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/vhosts", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(vhosts)
	})
	return httptest.NewServer(mux)
}

func resetCfg() func() {
	old := config.Config
	return func() { config.Config = old }
}

// --- tests ---

func TestGetAllVhosts_SingleEndpoint(t *testing.T) {
	defer resetCfg()()

	srv := fakeMgmtVhostServer(t, []vhost{{Name: "/", Tracing: false}, {Name: "dev", Tracing: true}})
	defer srv.Close()

	setEndpointsJSON([]config.ManagementEndpoint{{URL: srv.URL}})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	targets, err := getAllVhosts(ctx)
	if err != nil {
		t.Fatalf("getAllVhosts error: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(targets))
	}

	got := findTargetByLabel(targets, "dev")
	if got == nil {
		t.Fatalf("target 'dev' not found")
	}
	assertAttr(t, *got, "rabbitmq.vhost.name", "dev")
	assertAttr(t, *got, "rabbitmq.vhost.tracing", "true")
}

func TestGetAllVhosts_MultipleEndpoints(t *testing.T) {
	defer resetCfg()()

	s1 := fakeMgmtVhostServer(t, []vhost{{Name: "alpha", Tracing: false}})
	defer s1.Close()
	s2 := fakeMgmtVhostServer(t, []vhost{{Name: "beta", Tracing: true}})
	defer s2.Close()

	setEndpointsJSON([]config.ManagementEndpoint{{URL: s1.URL}, {URL: s2.URL}})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	targets, err := getAllVhosts(ctx)
	if err != nil {
		t.Fatalf("getAllVhosts error: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(targets))
	}

	if findTargetByLabel(targets, "alpha") == nil || findTargetByLabel(targets, "beta") == nil {
		t.Fatalf("expected targets 'alpha' and 'beta'")
	}
}

func TestGetAllVhosts_AttributeExcludes(t *testing.T) {
	defer resetCfg()()

	srv := fakeMgmtVhostServer(t, []vhost{{Name: "x", Tracing: true}})
	defer srv.Close()

	setEndpointsJSON([]config.ManagementEndpoint{{URL: srv.URL}})
	config.Config.DiscoveryAttributesExcludesVhosts = []string{"rabbitmq.vhost.tracing"}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	targets, err := getAllVhosts(ctx)
	if err != nil {
		t.Fatalf("getAllVhosts error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}

	if _, ok := targets[0].Attributes["rabbitmq.vhost.tracing"]; ok {
		t.Fatalf("expected attribute 'rabbitmq.vhost.tracing' to be excluded")
	}
}
