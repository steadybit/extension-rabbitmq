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

type exch struct {
	Name       string `json:"name"`
	Vhost      string `json:"vhost"`
	Type       string `json:"type"`
	Durable    bool   `json:"durable"`
	AutoDelete bool   `json:"auto_delete"`
	Internal   bool   `json:"internal"`
}

func fakeMgmtExchangesServer(t *testing.T, exchanges []exch) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/exchanges", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(exchanges)
	})
	return httptest.NewServer(mux)
}

func resetCfgExchanges() func() {
	old := config.Config
	return func() { config.Config = old }
}

func setEndpointsJSONExchanges(eps []config.ManagementEndpoint) {
	b, _ := json.Marshal(eps)
	config.Config.ManagementEndpointsJSON = string(b)
	config.Config.ManagementEndpoints = eps
}

func findTargetByLabelExchanges(ts []discovery_kit_api.Target, label string) *discovery_kit_api.Target {
	for i := range ts {
		if ts[i].Label == label {
			return &ts[i]
		}
	}
	return nil
}

func assertAttrExchanges(t *testing.T, tgt discovery_kit_api.Target, key, want string) {
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

func TestGetAllExchanges_SingleEndpoint(t *testing.T) {
	defer resetCfgExchanges()()

	s := fakeMgmtExchangesServer(t, []exch{
		{Name: "amq.direct", Vhost: "/", Type: "direct", Durable: true, AutoDelete: false, Internal: false},
		{Name: "events", Vhost: "vh1", Type: "topic", Durable: true, AutoDelete: false, Internal: false},
	})
	defer s.Close()

	setEndpointsJSONExchanges([]config.ManagementEndpoint{{URL: s.URL}})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	targets, err := getAllExchanges(ctx)
	if err != nil {
		t.Fatalf("getAllExchanges error: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(targets))
	}

	ev := findTargetByLabelExchanges(targets, "vh1/events")
	if ev == nil {
		t.Fatalf("target vh1/events not found")
	}
	assertAttrExchanges(t, *ev, "rabbitmq.exchange.vhost", "vh1")
	assertAttrExchanges(t, *ev, "rabbitmq.exchange.name", "events")
	assertAttrExchanges(t, *ev, "rabbitmq.exchange.type", "topic")
	assertAttrExchanges(t, *ev, "rabbitmq.exchange.durable", "true")
	assertAttrExchanges(t, *ev, "rabbitmq.exchange.auto_delete", "false")
	assertAttrExchanges(t, *ev, "rabbitmq.exchange.internal", "false")
}

func TestGetAllExchanges_MultipleEndpoints(t *testing.T) {
	defer resetCfgExchanges()()

	s1 := fakeMgmtExchangesServer(t, []exch{
		{Name: "ex1", Vhost: "a", Type: "fanout", Durable: false, AutoDelete: true, Internal: false},
	})
	defer s1.Close()
	s2 := fakeMgmtExchangesServer(t, []exch{
		{Name: "ex2", Vhost: "b", Type: "headers", Durable: true, AutoDelete: false, Internal: true},
	})
	defer s2.Close()

	setEndpointsJSONExchanges([]config.ManagementEndpoint{{URL: s1.URL}, {URL: s2.URL}})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	targets, err := getAllExchanges(ctx)
	if err != nil {
		t.Fatalf("getAllExchanges error: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(targets))
	}

	if findTargetByLabelExchanges(targets, "a/ex1") == nil {
		t.Fatalf("expected target a/ex1")
	}
	if findTargetByLabelExchanges(targets, "b/ex2") == nil {
		t.Fatalf("expected target b/ex2")
	}
}
