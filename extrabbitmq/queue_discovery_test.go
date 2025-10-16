package extrabbitmq

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/steadybit/discovery-kit/go/discovery_kit_api"
	"github.com/steadybit/extension-rabbitmq/config"
)

// --- helpers ---

func fakeMgmtQueuesServer(t *testing.T, queues []map[string]interface{}) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/queues", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(queues)
	})
	return httptest.NewServer(mux)
}

func resetCfgQueues() func() {
	old := config.Config
	return func() { config.Config = old }
}

func setEndpointsJSONQueues(eps []config.ManagementEndpoint) {
	b, _ := json.Marshal(eps)
	config.Config.ManagementEndpointsJSON = string(b)
	config.Config.ManagementEndpoints = eps
}

func findTargetByLabelQueues(ts []discovery_kit_api.Target, label string) *discovery_kit_api.Target {
	for i := range ts {
		if ts[i].Label == label {
			return &ts[i]
		}
	}
	return nil
}

func assertAttrQueues(t *testing.T, tgt discovery_kit_api.Target, key, want string) {
	t.Helper()
	vals, ok := tgt.Attributes[key]
	if !ok {
		t.Fatalf("attribute %q missing", key)
	}
	if len(vals) == 0 || vals[0] != want {
		t.Fatalf("attribute %q = %v, want %q", key, vals, want)
	}
}

// --- tests ---

func TestGetAllQueues_SingleEndpoint(t *testing.T) {
	defer resetCfgQueues()()

	// two queues on default vhost
	payload := []map[string]interface{}{
		{
			"name":                    "orders",
			"vhost":                   "/",
			"durable":                 true,
			"auto_delete":             false,
			"consumers":               3,
			"messages":                10,
			"messages_ready":          7,
			"messages_unacknowledged": 3,
			"state":                   "running",
			"status":                  "running",
		},
		{
			"name":                    "payments",
			"vhost":                   "/",
			"durable":                 false,
			"auto_delete":             true,
			"consumers":               0,
			"messages":                0,
			"messages_ready":          0,
			"messages_unacknowledged": 0,
			"state":                   "idle",
			"status":                  "idle",
		},
	}
	s := fakeMgmtQueuesServer(t, payload)
	defer s.Close()

	setEndpointsJSONQueues([]config.ManagementEndpoint{{URL: s.URL}})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	targets, err := getAllQueues(ctx)
	if err != nil {
		t.Fatalf("getAllQueues error: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(targets))
	}

	q := findTargetByLabelQueues(targets, "/"+"/"+"orders") // label is vhost/name, here '///orders'
	if q == nil {
		// fallback to "/" + "orders"
		q = findTargetByLabelQueues(targets, "//orders")
	}
	if q == nil {
		q = findTargetByLabelQueues(targets, "/orders")
	}
	if q == nil {
		t.Fatalf("orders queue target not found")
	}
	assertAttrQueues(t, *q, "rabbitmq.queue.vhost", "/")
	assertAttrQueues(t, *q, "rabbitmq.queue.name", "orders")
	assertAttrQueues(t, *q, "rabbitmq.queue.durable", "true")
	assertAttrQueues(t, *q, "rabbitmq.queue.auto_delete", "false")
	assertAttrQueues(t, *q, "rabbitmq.queue.messages_ready", "7")
}

func TestGetAllQueues_MultipleEndpoints(t *testing.T) {
	defer resetCfgQueues()()

	s1 := fakeMgmtQueuesServer(t, []map[string]interface{}{
		{"name": "alpha", "vhost": "vh1", "durable": true, "auto_delete": false, "messages": 1, "messages_ready": 1, "messages_unacknowledged": 0, "state": "running", "status": "running"},
	})
	defer s1.Close()
	s2 := fakeMgmtQueuesServer(t, []map[string]interface{}{
		{"name": "beta", "vhost": "vh2", "durable": false, "auto_delete": true, "messages": 0, "messages_ready": 0, "messages_unacknowledged": 0, "state": "idle", "status": "idle"},
	})
	defer s2.Close()

	setEndpointsJSONQueues([]config.ManagementEndpoint{{URL: s1.URL}, {URL: s2.URL}})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	targets, err := getAllQueues(ctx)
	if err != nil {
		t.Fatalf("getAllQueues error: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(targets))
	}

	// labels are vhost/name
	if findTargetByLabelQueues(targets, "vh1/alpha") == nil {
		t.Fatalf("expected target vh1/alpha")
	}
	if findTargetByLabelQueues(targets, "vh2/beta") == nil {
		t.Fatalf("expected target vh2/beta")
	}
}

func TestGetAllQueues_AttributeExcludes(t *testing.T) {
	defer resetCfgQueues()()

	s := fakeMgmtQueuesServer(t, []map[string]interface{}{
		{"name": "x", "vhost": "vh", "durable": true, "auto_delete": false, "messages": 0, "messages_ready": 0, "messages_unacknowledged": 0, "state": "idle", "status": "idle"},
	})
	defer s.Close()

	setEndpointsJSONQueues([]config.ManagementEndpoint{{URL: s.URL}})
	config.Config.DiscoveryAttributesExcludesQueues = []string{"rabbitmq.queue.auto_delete"}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	targets, err := getAllQueues(ctx)
	if err != nil {
		t.Fatalf("getAllQueues error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	if _, ok := targets[0].Attributes["rabbitmq.queue.auto_delete"]; ok {
		t.Fatalf("expected attribute rabbitmq.queue.auto_delete to be excluded")
	}
}
