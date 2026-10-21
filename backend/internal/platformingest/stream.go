package platformingest

import (
	"context"
	"encoding/json"
	"errors"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

var ErrConflict = errors.New("retained platform event conflicts with original input")

// StreamTarget binds one native delivery to an existing scoped store.
type StreamTarget struct {
	Store  *Store
	Event  platform.StreamEvent
	Native json.RawMessage
}

// IngestStreamFanout retains one delivery across one to sixteen unique scopes
// sharing one product pool. The caller must hold all owner leases through return.
// Every event/native/job commits together; errors expose no partial result.
func IngestStreamFanout(ctx context.Context, targets []StreamTarget) ([]bool, error) {
	if len(targets) == 0 || len(targets) > tenantdb.MaxStreamFanout {
		return nil, ErrInvalid
	}
	inputs := make([]tenantdb.StreamAdmission, len(targets))
	for i, target := range targets {
		if target.Store == nil || target.Store.db == nil {
			return nil, ErrInvalid
		}
		inputs[i] = tenantdb.StreamAdmission{Scope: target.Store.db, Event: target.Event, Native: target.Native}
	}
	result, err := tenantdb.AdmitStreamFanout(ctx, inputs)
	if errors.Is(err, tenantdb.ErrStreamInvalid) {
		return nil, ErrInvalid
	}
	if errors.Is(err, tenantdb.ErrStreamConflict) {
		return nil, ErrConflict
	}
	return result, err
}

// IngestStream preserves single-source admission, duplicate noops and original
// deadlines using the same atomic path as explicit multi-workspace fanout.
func (s *Store) IngestStream(ctx context.Context, event platform.StreamEvent, native json.RawMessage) (bool, error) {
	results, err := IngestStreamFanout(ctx, []StreamTarget{{Store: s, Event: event, Native: native}})
	if err != nil {
		return false, err
	}
	return results[0], nil
}

// Both kinds serialize on the tenant row and charge this combined admission
// count. Existing display history retains its independent trimming convention.
const retainedEventCount = `SELECT (SELECT count(*) FROM normalized_chat_events WHERE /*tenant:normalized_chat_events*/)+(SELECT count(*) FROM normalized_stream_events WHERE /*tenant:normalized_stream_events*/)`
