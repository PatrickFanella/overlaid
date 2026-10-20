package platform

// StreamDispatchArgs references a retained typed stream event. Private channel
// text, raw payloads and credentials never enter the queue. The original source
// deadline is fixed by a composite database foreign key, not by retry time.
type StreamDispatchArgs struct {
	TenantID     string `json:"tenant_id"`
	ConnectionID string `json:"connection_id"`
	EventID      string `json:"event_id"`
	Deadline     int64  `json:"deadline"`
}

func (StreamDispatchArgs) Kind() string { return "overlaid_stream_dispatch" }
