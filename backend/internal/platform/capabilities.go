package platform

type Capability string

const CapabilityVersion = 1

type CapabilityDefinition struct {
	Name Capability
	Read bool
}

// Registry returns a copy so adapters cannot alter the service's vocabulary.
func Registry() []CapabilityDefinition {
	return []CapabilityDefinition{
		{"chat.read", true}, {"chat.send", false}, {"chat.reply", false}, {"chat.announce", false}, {"chat.whisper", false}, {"chat.clear", false},
		{"message.delete", false}, {"user.timeout", false}, {"user.ban", false}, {"user.unban", false}, {"user.warn", false},
		{"role.moderator.manage", false}, {"role.vip.manage", false}, {"chat.modes.manage", false}, {"shield.manage", false},
		{"stream.read", true}, {"stream.update", false}, {"stream.marker.create", false}, {"clip.create", false}, {"raid.start", false}, {"shoutout.send", false},
		{"reward.read", true}, {"reward.manage", false}, {"redemption.read", true}, {"redemption.resolve", false},
		{"poll.read", true}, {"poll.create", false}, {"prediction.read", true}, {"prediction.create", false},
		{"membership.read", true}, {"follow.read", true}, {"tip.read", true}, {"discord.interaction.respond", false}, {"discord.role.manage", false},
	}
}

// Gates are explicit approvals for this capability. A gate not required by a
// provider must still be approved by its adapter; the zero value denies access.
type CapabilityGates struct {
	Supported, Granted, RoleAllowed, StateAllowed, Entitled bool
}

type Availability struct {
	Available bool
	Reason    string
}

type CapabilityReport struct {
	Version int
	entries map[Capability]Availability
}

func EvaluateCapabilities(gates map[Capability]CapabilityGates) CapabilityReport {
	report := CapabilityReport{Version: CapabilityVersion, entries: make(map[Capability]Availability)}
	for _, definition := range Registry() {
		g := gates[definition.Name]
		result := Availability{Available: true}
		switch {
		case !g.Supported:
			result = Availability{Reason: "provider_unsupported"}
		case !g.Granted:
			result = Availability{Reason: "grant_missing"}
		case !g.RoleAllowed:
			result = Availability{Reason: "role_required"}
		case !g.StateAllowed:
			result = Availability{Reason: "state_unavailable"}
		case !g.Entitled:
			result = Availability{Reason: "plan_required"}
		}
		report.entries[definition.Name] = result
	}
	return report
}

// Check also denies capabilities absent from a zero or older report. Callers
// must obtain a fresh report at dispatch, rather than retaining onboarding grants.
func (r CapabilityReport) Check(capability Capability) Availability {
	if result, ok := r.entries[capability]; ok && r.Version == CapabilityVersion {
		return result
	}
	return Availability{Reason: "unknown_capability"}
}
