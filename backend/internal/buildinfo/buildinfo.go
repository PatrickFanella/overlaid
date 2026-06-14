package buildinfo

import "runtime"

var (
	Version = "dev"
	Commit  = "unknown"
	BuiltAt = "unknown"
)

type Info struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuiltAt   string `json:"builtAt"`
	GoVersion string `json:"goVersion"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

func Report() Info {
	return Info{
		Name:      "over-laid",
		Version:   valueOrDefault(Version, "dev"),
		Commit:    valueOrDefault(Commit, "unknown"),
		BuiltAt:   valueOrDefault(BuiltAt, "unknown"),
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}
}

func valueOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
