package tenancy

import (
	"encoding/base64"
	"encoding/json"
	"sync"
	"testing"
)

func TestVaultRefreshSerializationKeyRotationAndRestart(t *testing.T) {
	s, a, _, owner, _, f := tenantPair(t)
	ctx := t.Context()
	v1 := base64.StdEncoding.EncodeToString(make([]byte, 32))
	next := make([]byte, 32)
	next[0] = 1
	v2 := base64.StdEncoding.EncodeToString(next)
	old, err := ParseKeyring("v1", "v1:"+v1)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveConnection(ctx, old, a.ID, owner, "twitch", "one", "One", nil, json.RawMessage(`{"generation":0,"refresh_token":"private"}`)); err != nil {
		t.Fatal(err)
	}
	rotated, err := ParseKeyring("v2", "v1:"+v1+",v2:"+v2)
	if err != nil {
		t.Fatal(err)
	}
	var work sync.WaitGroup
	for i := 0; i < 8; i++ {
		work.Add(1)
		go func() {
			defer work.Done()
			_, err := s.ResolveConnection(ctx, rotated, a.ID, "twitch", func(raw json.RawMessage) (json.RawMessage, error) {
				var data map[string]any
				if err := json.Unmarshal(raw, &data); err != nil {
					return nil, err
				}
				data["generation"] = data["generation"].(float64) + 1
				return json.Marshal(data)
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	work.Wait()
	var version string
	var ciphertext []byte
	if err = f.Admin.QueryRow(`SELECT key_version,ciphertext FROM platform_connections WHERE tenant_id=$1`, a.ID).Scan(&version, &ciphertext); err != nil || version != "v2" {
		t.Fatal("key rotation not persisted", err)
	}
	if json.Valid(ciphertext) {
		t.Fatal("credential was stored as plaintext JSON")
	}
	// A fresh process can decrypt after the old key is removed, because access
	// re-encrypted the stored document under the active version.
	restarted, err := Open(f.RuntimeURL)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	onlyNew, _ := ParseKeyring("v2", "v2:"+v2)
	raw, err := restarted.ResolveConnection(ctx, onlyNew, a.ID, "twitch", func(raw json.RawMessage) (json.RawMessage, error) { return raw, nil })
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	json.Unmarshal(raw, &data)
	if data["generation"] != float64(8) {
		t.Fatal("concurrent refresh lost update", data)
	}
	if _, err = s.ResolveConnection(ctx, old, a.ID, "twitch", func(raw json.RawMessage) (json.RawMessage, error) { return raw, nil }); err == nil {
		t.Fatal("missing key was accepted")
	}
}
