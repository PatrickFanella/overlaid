package platform

// ChatDispatchArgs deliberately contains no chat, raw payload or credentials.
// Deadline is derived from the retained event, never renewed by a retry.
type ChatDispatchArgs struct {
	TenantID     string `json:"tenant_id"`
	ConnectionID string `json:"connection_id"`
	EventID      string `json:"event_id"`
	Deadline     int64  `json:"deadline"`
}

func (ChatDispatchArgs) Kind() string { return "overlaid_chat_dispatch" }

// ChatSendArgs references the private ledger rather than copying message text,
// credentials or a claim token. Deadline is never renewed by job insertion.
type ChatSendArgs struct {
	TenantID string `json:"tenant_id"`
	ActionID string `json:"action_id"`
	Deadline int64  `json:"deadline"`
}

func (ChatSendArgs) Kind() string { return "overlaid_chat_send" }

// ModerationArgs references a private ledger action. Targets, reasons,
// credentials and claim tokens never enter jobs. Deadline is source-derived.
type ModerationArgs struct {
	TenantID string `json:"tenant_id"`
	ActionID string `json:"action_id"`
	Deadline int64  `json:"deadline"`
}

func (ModerationArgs) Kind() string { return "overlaid_moderation" }
