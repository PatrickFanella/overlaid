package config

import (
	"os"
	"strings"
	"testing"
)

func TestTwitchScopeDefaultsMatchDeployment(t *testing.T) {
	t.Setenv("TWITCH_SCOPES", "")
	defaults := strings.Join(Load().TwitchScopes, " ")
	for _, file := range []string{"../../../docker-compose.yml", "../../../.env.example", "../../../README.md"} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), defaults) {
			t.Fatalf("%s differs from config defaults", file)
		}
	}
}
