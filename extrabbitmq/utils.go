package extrabbitmq

import (
	"github.com/steadybit/discovery-kit/go/discovery_kit_api"
	"testing"
)

func findTargetByLabel(ts []discovery_kit_api.Target, label string) *discovery_kit_api.Target {
	for i := range ts {
		if ts[i].Label == label {
			return &ts[i]
		}
	}
	return nil
}

func assertAttr(t *testing.T, tgt discovery_kit_api.Target, key, want string) {
	t.Helper()
	vals, ok := tgt.Attributes[key]
	if !ok {
		t.Fatalf("attribute %q missing", key)
	}
	if len(vals) == 0 || vals[0] != want {
		t.Fatalf("attribute %q = %v, want %q", key, vals, want)
	}
}
