package buildinfo

import "testing"

func TestReportUsesSafeDefaults(t *testing.T) {
	originalVersion, originalCommit, originalBuiltAt := Version, Commit, BuiltAt
	t.Cleanup(func() { Version, Commit, BuiltAt = originalVersion, originalCommit, originalBuiltAt })

	Version, Commit, BuiltAt = "", "", ""
	info := Report()

	if info.Name != "over-laid" {
		t.Fatalf("expected over-laid, got %q", info.Name)
	}
	if info.Version != "dev" || info.Commit != "unknown" || info.BuiltAt != "unknown" {
		t.Fatalf("unexpected defaults: %#v", info)
	}
	if info.GoVersion == "" || info.OS == "" || info.Arch == "" {
		t.Fatalf("runtime metadata missing: %#v", info)
	}
}

func TestReportUsesInjectedValues(t *testing.T) {
	originalVersion, originalCommit, originalBuiltAt := Version, Commit, BuiltAt
	t.Cleanup(func() { Version, Commit, BuiltAt = originalVersion, originalCommit, originalBuiltAt })

	Version, Commit, BuiltAt = "v1", "abc123", "2026-06-13T00:00:00Z"
	info := Report()

	if info.Version != "v1" || info.Commit != "abc123" || info.BuiltAt != "2026-06-13T00:00:00Z" {
		t.Fatalf("injected values not used: %#v", info)
	}
}
