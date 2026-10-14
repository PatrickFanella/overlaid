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
