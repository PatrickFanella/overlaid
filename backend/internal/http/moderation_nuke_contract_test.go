package httpapi

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platformmoderation"
)

// The browser consumes this same fixture. Marshal the actual HTTP response
// types to catch wire-name, omission and empty-array contract drift.
func TestRetainedNukeBrowserReceiptContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/retained-nuke-receipts.json")
	if err != nil {
		t.Fatal(err)
	}
	var expected map[string]any
	if err := json.Unmarshal(raw, &expected); err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	for name, states := range map[string][]string{"mixed": {"delivered", "rejected", "uncertain", "pending"}, "radiation": {}} {
		t.Run(name, func(t *testing.T) {
			status := platformmoderation.NukeStatus{ManualStatus: platformmoderation.ManualStatus{ManualAdmission: platformmoderation.ManualAdmission{SourceID: strings.Repeat("a", 64), ActionIDs: []string{}, CreatedAt: created, Deadline: created.Add(2 * time.Minute)}, Receipts: []platformmoderation.Receipt{}}}
			for i, state := range states {
				id := strings.Repeat(string(rune('1'+i)), 64)
				r := platformmoderation.Receipt{ManualSourceID: status.SourceID, ActionID: id, ConnectionID: "twitch:123", Capability: "user.timeout", State: state, Code: state, CreatedAt: created}
				if state == "rejected" {
					r.Failure = "unauthorized"
				}
				status.ActionIDs = append(status.ActionIDs, id)
				status.Receipts = append(status.Receipts, r)
			}
			if name == "radiation" {
				status.RadiationRuleID = strings.Repeat("d", 64)
			}
			actualRaw, err := json.Marshal(retainedNukeReceipt{Mode: "retained", Queued: name == "mixed", RadiationOnly: name == "radiation", Receipt: status})
			if err != nil {
				t.Fatal(err)
			}
			var actual any
			if err := json.Unmarshal(actualRaw, &actual); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, expected[name]) {
				t.Fatalf("HTTP receipt %s differs from browser fixture: %s", name, actualRaw)
			}
		})
	}
}
