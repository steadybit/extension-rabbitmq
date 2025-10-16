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

type nodeResp struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Running bool   `json:"running"`
}

func fakeMgmtNodesServer(t *testing.T, nodes []nodeResp) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/nodes", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(nodes)
	})
	return httptest.NewServer(mux)
}

func resetCfgNodes() func() {
	old := config.Config
	return func() { config.Config = old }
}

// --- tests ---

func TestGetAllNodes_SingleEndpoint(t *testing.T) {
	defer resetCfgNodes()()

	srv := fakeMgmtNodesServer(t, []nodeResp{{Name: "rabbit@node-0", Type: "disc", Running: true}})
	defer srv.Close()

	config.Config.ManagementEndpoints = []config.ManagementEndpoint{{URL: srv.URL}}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	targets, err := getAllNodes(ctx)
	if err != nil {
		t.Fatalf("getAllNodes error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}

	got := targets[0]
	if got.Label != "rabbit@node-0" {
		t.Fatalf("unexpected label: %s", got.Label)
	}
	assertAttr(t, got, "rabbitmq.node.name", "rabbit@node-0")
	assertAttr(t, got, "rabbitmq.node.type", "disc")
	assertAttr(t, got, "rabbitmq.node.running", "true")
}

func TestGetAllNodes_MultipleEndpoints(t *testing.T) {
	defer resetCfgNodes()()

	s1 := fakeMgmtNodesServer(t, []nodeResp{{Name: "rabbit@a", Type: "disc", Running: true}})
	defer s1.Close()
	s2 := fakeMgmtNodesServer(t, []nodeResp{{Name: "rabbit@b", Type: "ram", Running: false}})
	defer s2.Close()

	config.Config.ManagementEndpoints = []config.ManagementEndpoint{{URL: s1.URL}, {URL: s2.URL}}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	targets, err := getAllNodes(ctx)
	if err != nil {
		t.Fatalf("getAllNodes error: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(targets))
	}

	if findTargetByLabel(targets, "rabbit@a") == nil || findTargetByLabel(targets, "rabbit@b") == nil {
		t.Fatalf("expected nodes 'rabbit@a' and 'rabbit@b'")
	}
}
