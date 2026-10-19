package platformmoderation

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
)

func planKeys() tenancy.Keyring {
	return tenancy.Keyring{Active: "v1", Keys: map[string][]byte{"v1": []byte(strings.Repeat("a", 32))}}
}
func planScope() NukePlanScope {
	return NukePlanScope{TenantID: "00000000-0000-4000-8000-000000000001", ActorID: owner, ConnectionID: "twitch:123", Binding: strings.Repeat("b", 64)}
}
func testNukePlan(t *testing.T, action string, empty bool) NukePlan {
	t.Helper()
	s := planScope()
	o, err := normalizeNukeOptions(NukeOptions{Pattern: "PRIVATE_PATTERN", Action: action, DurationSeconds: 90, RadiationMinutes: 7})
	if err != nil {
		t.Fatal(err)
	}
	// Old input must be restorable for lookup; opening never renews this time.
	observed := time.Date(2026, 1, 1, 12, 0, 0, 123456000, time.UTC)
	p := NukePlan{tenant: s.TenantID, request: ManualRequest{ActorAccountID: s.ActorID, RequestKey: nukePlanRequestKey, ConnectionID: s.ConnectionID, Kind: "radiation", nuke: &nukeBinding{options: o, observedAt: observed}}, targets: []NukeTarget{}}
	if empty {
		return p
	}
	e, err := platform.NewChatEvent(platform.Connection{TenantID: s.TenantID, ID: s.ConnectionID, Platform: "twitch"}, "PRIVATE_MESSAGE", observed.Add(-time.Minute), observed, platform.ChatMessage{MessageID: "PRIVATE_MESSAGE", Text: "PRIVATE_PATTERN", Actor: platform.Identity{ID: "777", Login: "PRIVATE_LOGIN"}, Channel: platform.Identity{ID: "123"}})
	if err != nil {
		t.Fatal(err)
	}
	p.request.Kind = "batch"
	p.request.Actions = []ManualAction{{Key: "target:" + e.ID, Action: nukeAction(e, o), targetEventID: e.ID, targetFingerprint: nukeEventHash(e)}}
	p.targets = []NukeTarget{{EventID: e.ID, MessageID: e.Chat.MessageID, UserID: e.Chat.Actor.ID, Login: e.Chat.Actor.Login, Text: e.Chat.Text, OccurredAt: e.OccurredAt}}
	return p
}

func TestNukePlanSealedRoundTripAndRotation(t *testing.T) {
	for _, tc := range []struct {
		action string
		empty  bool
	}{{"delete", false}, {"timeout", false}, {"timeout", true}} {
		t.Run(tc.action+map[bool]string{true: "-empty"}[tc.empty], func(t *testing.T) {
			p, keys, scope := testNukePlan(t, tc.action, tc.empty), planKeys(), planScope()
			token, err := SealNukePlan(keys, scope, p)
			if err != nil {
				t.Fatal(err)
			}
			wrapped, _ := base64.RawURLEncoding.DecodeString(token)
			for _, private := range []string{"PRIVATE_PATTERN", "PRIVATE_MESSAGE", "PRIVATE_LOGIN", p.Fingerprint(), scope.ActorID, scope.Binding} {
				if strings.Contains(string(wrapped), private) {
					t.Fatal("private input in token wrapper", private)
				}
			}
			keys.Active = "v2"
			keys.Keys["v2"] = []byte(strings.Repeat("c", 32))
			restored, err := OpenNukePlan(keys, scope, token)
			if err != nil || restored.Fingerprint() != p.Fingerprint() || !reflect.DeepEqual(restored.Options(), p.Options()) || !reflect.DeepEqual(restored.Targets(), p.Targets()) || !restored.ObservedAt().Equal(p.ObservedAt()) {
				t.Fatal("round trip changed input", err)
			}
			newToken, err := SealNukePlan(keys, scope, restored)
			if err != nil || newToken == token {
				t.Fatal("new ciphertext/rotation", err)
			}
			delete(keys.Keys, "v1")
			if _, err := OpenNukePlan(keys, scope, token); !errors.Is(err, ErrNukePlanUnavailable) {
				t.Fatal("retired key accepted", err)
			}
			if _, err := OpenNukePlan(keys, scope, newToken); err != nil {
				t.Fatal("active rotated key refused", err)
			}
		})
	}
}

func TestNukePlanScopeAndTokenRefusal(t *testing.T) {
	p, keys, scope := testNukePlan(t, "timeout", false), planKeys(), planScope()
	token, err := SealNukePlan(keys, scope, p)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"tenant", "actor", "channel", "binding", "binding-invalid", "channel-invalid"} {
		t.Run(mode, func(t *testing.T) {
			wrong := scope
			switch mode {
			case "tenant":
				wrong.TenantID = "00000000-0000-4000-8000-000000000002"
			case "actor":
				wrong.ActorID = "00000000-0000-4000-8000-000000000099"
			case "channel":
				wrong.ConnectionID = "twitch:456"
			case "binding":
				wrong.Binding = strings.Repeat("d", 64)
			case "binding-invalid":
				wrong.Binding = "browser-selected"
			case "channel-invalid":
				wrong.ConnectionID = "twitch:abc"
			}
			if _, err := OpenNukePlan(keys, wrong, token); !errors.Is(err, ErrNukePlanUnavailable) {
				t.Fatal("wrong scope accepted", err)
			}
			if mode != "binding" {
				if _, err := SealNukePlan(keys, wrong, p); !errors.Is(err, ErrNukePlanUnavailable) {
					t.Fatal("wrong plan scope sealed", err)
				}
			}
		})
	}
	for _, invalid := range []string{"", "raw-browser-json", token[:len(token)-1], token[:10] + "\n" + token[10:], strings.Repeat("a", MaxNukePlanTokenBytes+1)} {
		if _, err := OpenNukePlan(keys, scope, invalid); !errors.Is(err, ErrNukePlanUnavailable) {
			t.Fatal("malformed token accepted", err)
		}
	}
	raw, _ := base64.RawURLEncoding.DecodeString(token)
	var wrapper sealedNukePlan
	_ = json.Unmarshal(raw, &wrapper)
	for _, mode := range []string{"version", "key", "ciphertext", "unknown", "trailing"} {
		w := wrapper
		w.Ciphertext = append([]byte(nil), wrapper.Ciphertext...)
		switch mode {
		case "version":
			w.Version++
		case "key":
			w.KeyVersion = "missing"
		case "ciphertext":
			w.Ciphertext[len(w.Ciphertext)-1] ^= 1
		}
		raw, _ := json.Marshal(w)
		if mode == "unknown" {
			raw = append(raw[:len(raw)-1], []byte(`,"unknown":true}`)...)
		}
		if mode == "trailing" {
			raw = append(raw, []byte(` {}`)...)
		}
		if _, err := OpenNukePlan(keys, scope, base64.RawURLEncoding.EncodeToString(raw)); !errors.Is(err, ErrNukePlanUnavailable) {
			t.Fatal("edited wrapper accepted", mode, err)
		}
	}
	if _, err := SealNukePlan(tenancy.Keyring{}, scope, p); !errors.Is(err, ErrNukePlanUnavailable) {
		t.Fatal("missing custody accepted", err)
	}
}

func TestNukePlanAuthenticatedSchemaRefusal(t *testing.T) {
	p, keys, scope := testNukePlan(t, "timeout", false), planKeys(), planScope()
	token, _ := SealNukePlan(keys, scope, p)
	raw, _ := base64.RawURLEncoding.DecodeString(token)
	var wrapped sealedNukePlan
	_ = json.Unmarshal(raw, &wrapped)
	purpose, _ := scope.purpose()
	plain, _ := keys.Unseal(scope.TenantID, purpose, wrapped.KeyVersion, wrapped.Ciphertext)
	for _, mode := range []string{"fingerprint", "options", "target", "action", "count", "unknown", "trailing", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			var saved savedNukePlan
			_ = json.Unmarshal(plain, &saved)
			switch mode {
			case "fingerprint":
				saved.Fingerprint = strings.Repeat("0", 64)
			case "options":
				saved.Options.LookbackMinutes = 0
			case "target":
				saved.Targets[0].EventID = strings.Repeat("e", 64)
			case "action":
				saved.Actions[0].Action.UserID = "888"
			case "count":
				saved.Targets = nil
			case "oversize":
				saved.Targets[0].Login = strings.Repeat("x", maxNukePlanPlainBytes)
			}
			raw, _ := json.Marshal(saved)
			if mode == "unknown" {
				raw = append(raw[:len(raw)-1], []byte(`,"unknown":true}`)...)
			}
			if mode == "trailing" {
				raw = append(raw, []byte(` {}`)...)
			}
			ciphertext, _ := keys.Seal(scope.TenantID, purpose, raw)
			raw, _ = json.Marshal(sealedNukePlan{Version: 1, KeyVersion: keys.Active, Ciphertext: ciphertext})
			if _, err := OpenNukePlan(keys, scope, base64.RawURLEncoding.EncodeToString(raw)); !errors.Is(err, ErrNukePlanUnavailable) {
				t.Fatal("invalid authenticated payload accepted", err)
			}
		})
	}
	p.targets[0].Login = strings.Repeat("x", maxNukePlanPlainBytes)
	if _, err := SealNukePlan(keys, scope, p); !errors.Is(err, ErrNukePlanUnavailable) {
		t.Fatal("oversized plan sealed", err)
	}
}

func TestNukePlanConcurrentNonceAndImmutableRestoration(t *testing.T) {
	p, keys, scope := testNukePlan(t, "delete", false), planKeys(), planScope()
	var wg sync.WaitGroup
	var mu sync.Mutex
	seen := map[string]bool{}
	for range 32 {
		wg.Go(func() {
			token, err := SealNukePlan(keys, scope, p)
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			if seen[token] {
				t.Error("reused ciphertext nonce")
			}
			seen[token] = true
			mu.Unlock()
			restored, err := OpenNukePlan(keys, scope, token)
			if err != nil {
				t.Error(err)
				return
			}
			targets := restored.Targets()
			targets[0].Text = "mutated"
			if restored.Targets()[0].Text != "PRIVATE_PATTERN" {
				t.Error("mutable target escaped")
			}
		})
	}
	wg.Wait()
}
