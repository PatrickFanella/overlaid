package tenancy

import (
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/engine"
)

func TestOperatorTraceFixedMetadataValidation(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	hash := strings.Repeat("a", 64)
	trace := OperatorTrace{FormatVersion: 1, TenantID: id, ID: hash, RunID: hash, AutomationID: id, VersionID: id, Mode: engine.ModeReproduce, CaptureVersion: 1, OriginalGraphDigest: hash, GraphDigest: hash, CaptureDigest: hash, Clock: time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC), Status: "proposed", Reason: "supported_flow", Steps: []engine.FlowStep{{Sequence: 1, AutomationID: id, VersionID: id, NodeID: "root", Kind: "action", Outcome: "proposed", Reason: "opaque_arguments"}}}
	if !validOperatorTrace(trace) {
		t.Fatal("valid fixed metadata refused")
	}
	for _, change := range []func(*OperatorTrace){func(x *OperatorTrace) { x.FormatVersion = 2 }, func(x *OperatorTrace) { x.CaptureVersion = 2 }, func(x *OperatorTrace) { x.Seed = 1 }, func(x *OperatorTrace) { x.Steps = nil }, func(x *OperatorTrace) { x.DroppedSteps = -1 }, func(x *OperatorTrace) { x.DroppedSteps = 1 }, func(x *OperatorTrace) { x.Mode = engine.ModeExecute }, func(x *OperatorTrace) { x.GraphDigest = strings.Repeat("b", 64) }, func(x *OperatorTrace) { x.Reason = "PRIVATE_VALUE" }, func(x *OperatorTrace) { x.Steps[0].Reason = "PRIVATE_VALUE" }, func(x *OperatorTrace) { x.Steps[0].Parent = 1 }, func(x *OperatorTrace) { x.Steps[0].Sequence = 2 }, func(x *OperatorTrace) { x.Steps[0].NodeID = "PRIVATE TEXT" }, func(x *OperatorTrace) { x.Clock = time.Time{} }} {
		changed := trace
		changed.Steps = append([]engine.FlowStep(nil), trace.Steps...)
		change(&changed)
		if validOperatorTrace(changed) {
			t.Fatal("invalid trace accepted", changed)
		}
	}
	if operatorTraceDigest([]byte("a")) == operatorTraceDigest([]byte("b")) {
		t.Fatal("unbound body digest")
	}
}
