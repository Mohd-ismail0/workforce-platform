package api

import "testing"

// The integration catalogue must expose what each operation has been EVIDENCED
// to do, because the UI labels every preview with it. If this surface went
// missing, previews would silently lose the distinction between "this is a
// simulation" and "this will really happen".
func TestIntegrationsExposePerOperationMaturity(t *testing.T) {
	f := newFixture(t)
	code, body := f.do("req", "GET", "/api/v1/integrations", nil)
	if code != 200 {
		t.Fatalf("integrations: %d %v", code, body)
	}
	items, _ := body["items"].([]any)
	if len(items) == 0 {
		t.Fatal("no integrations returned")
	}

	sawMaturity := false
	for _, raw := range items {
		m, _ := raw.(map[string]any)
		id, _ := m["id"].(string)
		simulation, _ := m["simulation"].(bool)
		maturity, _ := m["maturity"].([]any)
		if len(maturity) == 0 {
			t.Errorf("%s exposes no per-operation maturity", id)
			continue
		}
		sawMaturity = true
		actions, _ := m["actions"].([]any)
		certified := map[string]bool{}
		for _, entry := range maturity {
			om, _ := entry.(map[string]any)
			action, _ := om["action"].(string)
			level, _ := om["maturity"].(string)
			evidence, _ := om["evidence"].(string)
			certified[action] = true
			if evidence == "" {
				t.Errorf("%s.%s states a level with no evidence", id, action)
			}
			switch level {
			case "read_only", "prepare_preview", "governed_apply", "verified_apply":
			default:
				t.Errorf("%s.%s has unexpected level %q", id, action, level)
			}
			// The rule that matters most: a simulator has not reached an external
			// system and has not read anything back, so it must not claim to have.
			if simulation && (level == "governed_apply" || level == "verified_apply") {
				t.Errorf("%s is a simulator but exposes %s as %q", id, action, level)
			}
		}
		// Every action offered must be accounted for in the surfaced list.
		for _, a := range actions {
			if !certified[a.(string)] {
				t.Errorf("%s offers %q with no maturity entry, so a client cannot tell what it does", id, a)
			}
		}
	}
	if !sawMaturity {
		t.Fatal("the catalogue exposed no maturity information at all")
	}
}
