package bot

import "testing"

func TestModerationPolicyDefaultsAndValidation(t *testing.T) {
	base := ModerationRule{Name: "spam", Enabled: true, Pattern: "buy", Action: "timeout"}
	rule, err := NormalizeModerationRule(base)
	if err != nil || rule.ExemptRole != "moderator" || rule.TimeoutSeconds != 60 {
		t.Fatalf("defaults=%#v %v", rule, err)
	}
	for _, seconds := range []int{-1, 1209601} {
		base.TimeoutSeconds = seconds
		if _, err := NormalizeModerationRule(base); err == nil {
			t.Fatalf("accepted %d", seconds)
		}
	}
	base.TimeoutSeconds = 1209600
	base.ExemptRole = "none"
	if _, err := NormalizeModerationRule(base); err != nil {
		t.Fatal(err)
	}
}
func TestModerationRoleDryRunNotes(t *testing.T) {
	rules := []ModerationRule{{Name: "spam", Enabled: true, Pattern: "buy", Action: "timeout", TimeoutSeconds: 300}}
	for _, role := range []Role{RoleViewer, RoleModerator, RoleBroadcaster} {
		matches, err := MatchModerationRules(rules, "buy now", "viewer")
		if err != nil {
			t.Fatal(err)
		}
		matches = ApplyModerationExemptions(matches, role)
		if len(matches) != 1 || matches[0].TimeoutSeconds != 300 || matches[0].Skipped != (role != RoleViewer) {
			t.Fatalf("role=%s matches=%#v", role, matches)
		}
		if role != RoleViewer && matches[0].Note == "" {
			t.Fatal("exemption not explained")
		}
	}
}
