package dashboardconfig

import (
	"strings"
	"testing"
)

func TestValidateAcceptsNormalDashboardConfigLikeObject(t *testing.T) {
	payload := []byte(`{
		"title": "Dashboard",
		"layout": {"columns": 3, "widgets": [{"id": "cpu", "type": "chart"}]},
		"enabled": true
	}`)

	if err := Validate(payload); err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}
}

func TestValidateRejectsNonObjectJSON(t *testing.T) {
	for _, payload := range [][]byte{
		[]byte(`[]`),
		[]byte(`"string"`),
		[]byte(`123`),
		[]byte(`null`),
	} {
		if err := Validate(payload); err == nil || !strings.Contains(err.Error(), "must be a JSON object") {
			t.Fatalf("expected non-object rejection for %s, got %v", payload, err)
		}
	}
}

func TestValidateRejectsSecretLookingKeysRecursively(t *testing.T) {
	payload := []byte(`{
		"safe": {
			"nested": [
				{"operatorSecret": "x"},
				{"meta": {"accessToken": "y"}},
				{"more": {"password": "z"}},
				{"auth": {"authorization": "a"}},
				{"cookies": [{"cookie": "b"}]}
			]
		}
	}`)

	if err := Validate(payload); err == nil || !strings.Contains(err.Error(), "must not contain secrets") {
		t.Fatalf("expected secret rejection, got %v", err)
	}
}
