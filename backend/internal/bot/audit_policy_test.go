package bot

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestAuditDataRejectsOverwrittenSecretsAndTrailingJSON(t *testing.T) {
	for _, payload := range []string{
		`{"details":{"accessToken":"private-fixture"},"details":{}}`,
		`{"details":[{"api_key":"private-fixture"}],"details":null}`,
		`{"details":{"pa\u0073sword":"private-fixture"},"details":{"publicKey":"public"}}`,
		`{"action":"safe"} {"token":"private-fixture"}`,
		`{"action":"safe"} trailing`,
	} {
		if err := ValidateAuditData(json.RawMessage(payload)); err == nil {
			t.Errorf("unsafe payload accepted: %s", payload)
		} else if strings.Contains(err.Error(), "private-fixture") {
			t.Errorf("error leaked payload content: %v", err)
		}
	}
}

func TestAuditPayloadPolicyPostgres(t *testing.T) {
	store, _ := isolatedPostgresBotStore(t)
	ctx := context.Background()
	for _, payload := range []string{
		`{"details":{"accessToken":"private-fixture"},"details":{}}`,
		`{"action":"safe"} {"token":"private-fixture"}`,
	} {
		if _, err := store.RecordAudit(ctx, AuditEvent{Action: "policy_fixture", Data: json.RawMessage(payload)}); err == nil {
			t.Fatal("unsafe audit write accepted")
		}
	}
	rows, err := store.RecentAudit(ctx, 50)
	if err != nil || len(rows) != 0 {
		t.Fatalf("rejected writes persisted: %v err=%v", rows, err)
	}
	if _, err := store.RecordAudit(ctx, AuditEvent{Action: "policy_fixture", Data: json.RawMessage(`{"details":{"publicKey":"one"},"details":{"publicKey":"two"}}`)}); err != nil {
		t.Fatal(err)
	}
	rows, err = store.RecentAudit(ctx, 50)
	if err != nil || len(rows) != 1 || !strings.Contains(string(rows[0].Data), "two") || strings.Contains(string(rows[0].Data), "one") {
		t.Fatalf("safe JSONB normalization: rows=%v err=%v", rows, err)
	}
}

func TestAuditDataAllowsSafeDuplicateMembersAndLiteralSecretWords(t *testing.T) {
	for _, payload := range []string{
		`{"details":{"publicKey":"one"},"details":{"publicKey":"two"}}`,
		`{"message":"token password secret api_key are literal words"}`,
		`{"details":[{"publicKey":"value"},null,42]}`,
	} {
		if err := ValidateAuditData(json.RawMessage(payload)); err != nil {
			t.Errorf("safe payload rejected: %v", err)
		}
	}
}
