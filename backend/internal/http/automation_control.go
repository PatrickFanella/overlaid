package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/engine"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

var errAutomationWriterUnavailable = errors.New("automation_writer_unavailable")

func automationControlPath(path string) bool {
	p := strings.Split(strings.Trim(path, "/"), "/")
	return len(p) >= 4 && p[0] == "api" && p[1] == "tenants" && p[3] == "automations"
}

// This control boundary deliberately authenticates the browser session directly:
// Application.actor also accepts bootstrap operator authority, which cannot
// authorize immutable workspace automation/run identities.
func (a *Application) automationControl(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 5 || !tenantdb.ValidID(parts[2]) {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	session, token, err := a.session(r)
	if err != nil {
		http.Error(w, "account authentication required", http.StatusUnauthorized)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	stop := context.AfterFunc(a.ctx, cancel)
	defer stop()
	unbind, err := a.bindSessionRequest(ctx, token, cancel)
	if err != nil {
		http.Error(w, "session expired or revoked", http.StatusUnauthorized)
		return
	}
	defer unbind()
	r = r.WithContext(ctx)
	if !a.mutation(r, token) {
		http.Error(w, "CSRF validation required", http.StatusForbidden)
		return
	}
	if parts[2] == tenantdb.BootstrapID {
		automationControlError(w, tenancy.ErrDenied)
		return
	}
	tenant, err := a.store.Resolve(ctx, session.AccountID, parts[2])
	if err != nil {
		if errors.Is(err, tenancy.ErrDenied) || errors.Is(err, sql.ErrNoRows) {
			automationControlError(w, tenancy.ErrDenied)
		} else {
			automationControlError(w, err)
		}
		return
	}
	if !tenancy.Allowed(tenant.Role, "configure") {
		automationControlError(w, tenancy.ErrDenied)
		return
	}
	// Metadata writers participate in the existing managed owner; this check
	// opens no execution pool and grants no provider/effect permission.
	switch parts[4] {
	case "publish", "select", "admit", "save-trace":
		if e := a.checkChatController(ctx); e != nil {
			if ctx.Err() != nil && a.ctx.Err() == nil {
				automationControlError(w, ctx.Err())
			} else {
				automationControlError(w, errAutomationWriterUnavailable)
			}
			return
		}
	}
	scope, err := a.store.Product.Scoped(tenant.ID)
	if err != nil {
		automationControlError(w, err)
		return
	}
	store, err := tenancy.NewAutomationStore(ctx, scope)
	if err != nil {
		automationControlError(w, err)
		return
	}
	actor := session.AccountID
	switch parts[4] {
	case "list":
		var body struct{}
		if !decodeControl(w, r, &body) {
			return
		}
		result, e := store.List(ctx, actor)
		if e != nil {
			automationControlError(w, e)
			return
		}
		items := make([]map[string]any, 0, len(result))
		for _, item := range result {
			items = append(items, map[string]any{"automationId": item.AutomationID, "currentVersionId": item.CurrentVersionID, "digest": item.Digest, "versionCount": item.VersionCount, "updatedAt": item.UpdatedAt, "versionCreatedAt": item.VersionCreatedAt})
		}
		writeJSON(w, map[string]any{"automations": items})
	case "list-versions":
		var body struct {
			AutomationID string `json:"automationId"`
		}
		if !decodeControl(w, r, &body) {
			return
		}
		result, e := store.ListVersions(ctx, actor, body.AutomationID)
		if e != nil {
			automationControlError(w, e)
			return
		}
		items := make([]map[string]any, 0, len(result.Versions))
		for _, item := range result.Versions {
			items = append(items, map[string]any{"versionId": item.VersionID, "digest": item.Digest, "createdAt": item.CreatedAt})
		}
		writeJSON(w, map[string]any{"automationId": result.AutomationID, "currentVersionId": result.CurrentVersionID, "versions": items})
	case "validate", "publish":
		var body struct {
			Document                 json.RawMessage `json:"document"`
			ExpectedCurrentVersionID string          `json:"expectedCurrentVersionId"`
		}
		if !decodeControl(w, r, &body) {
			return
		}
		v, e := engine.Parse(body.Document)
		if e != nil {
			automationControlError(w, e)
			return
		}
		d := v.Document()
		if d.TenantID != tenant.ID {
			automationControlError(w, tenancy.ErrAutomationInvalid)
			return
		}
		if parts[4] == "validate" {
			if body.ExpectedCurrentVersionID != "" {
				automationControlError(w, tenancy.ErrAutomationInvalid)
				return
			}
			writeJSON(w, map[string]any{"validation": "structure_only", "automationId": d.AutomationID, "versionId": d.VersionID, "digest": v.Digest(), "declaredCapabilities": d.Capabilities})
			return
		}
		result, e := store.Publish(ctx, actor, v, body.ExpectedCurrentVersionID)
		if e != nil {
			automationControlError(w, e)
			return
		}
		writeJSON(w, automationPublicationJSON(result))
	case "select":
		var body struct {
			AutomationID             string `json:"automationId"`
			VersionID                string `json:"versionId"`
			ExpectedCurrentVersionID string `json:"expectedCurrentVersionId"`
		}
		if !decodeControl(w, r, &body) {
			return
		}
		result, e := store.SelectCurrent(ctx, actor, body.AutomationID, body.VersionID, body.ExpectedCurrentVersionID)
		if e != nil {
			automationControlError(w, e)
			return
		}
		writeJSON(w, automationPublicationJSON(result))
	case "load":
		var body struct {
			AutomationID string `json:"automationId"`
			VersionID    string `json:"versionId"`
		}
		if !decodeControl(w, r, &body) {
			return
		}
		g, e := store.Load(ctx, actor, body.AutomationID, body.VersionID)
		if e != nil {
			automationControlError(w, e)
			return
		}
		versions := []json.RawMessage{}
		for _, v := range g.Versions() {
			versions = append(versions, json.RawMessage(v.JSON()))
		}
		root := g.Root().Document()
		writeJSON(w, map[string]any{"automationId": root.AutomationID, "versionId": root.VersionID, "graphDigest": g.Digest(), "versions": versions})
	case "admit":
		var body struct {
			RequestID         string `json:"requestId"`
			AutomationID      string `json:"automationId"`
			ExpectedVersionID string `json:"expectedVersionId"`
		}
		if !decodeControl(w, r, &body) {
			return
		}
		result, e := store.AdmitOperator(ctx, actor, body.RequestID, body.AutomationID, body.ExpectedVersionID)
		if e != nil {
			automationControlError(w, e)
			return
		}
		writeJSON(w, automationRunJSON(result))
	case "lookup":
		var body struct {
			RequestID string `json:"requestId"`
		}
		if !decodeControl(w, r, &body) {
			return
		}
		result, e := store.LookupOperator(ctx, actor, body.RequestID)
		if e != nil {
			automationControlError(w, e)
			return
		}
		writeJSON(w, automationRunJSON(result))
	case "rehearse":
		var body struct {
			RequestID string      `json:"requestId"`
			Mode      engine.Mode `json:"mode"`
		}
		if !decodeControl(w, r, &body) {
			return
		}
		result, e := store.RehearseOperator(ctx, actor, body.RequestID, body.Mode)
		if e != nil {
			automationControlError(w, e)
			return
		}
		f := result.Flow
		steps := make([]map[string]any, 0, len(f.Steps))
		for _, s := range f.Steps {
			steps = append(steps, map[string]any{"sequence": s.Sequence, "parent": s.Parent, "automationId": s.AutomationID, "versionId": s.VersionID, "nodeId": s.NodeID, "kind": s.Kind, "outcome": s.Outcome, "reason": s.Reason})
		}
		proposals := make([]map[string]any, 0, len(f.Proposals))
		for _, p := range f.Proposals {
			proposals = append(proposals, map[string]any{"sequence": p.Sequence, "automationId": p.AutomationID, "versionId": p.VersionID, "nodeId": p.NodeID, "declaredCapability": p.Capability, "opaqueArguments": p.Arguments})
		}
		writeJSON(w, map[string]any{"captureVersion": result.CaptureVersion, "selectedVersionId": result.SelectedVersionID, "run": automationRunJSON(result.Run), "rehearsal": map[string]any{"id": f.ID, "mode": f.Mode, "originalGraphDigest": f.OriginalGraphDigest, "graphDigest": f.GraphDigest, "captureDigest": f.CaptureDigest, "clock": f.Clock, "seed": f.Seed, "status": f.Status, "reason": f.Reason, "steps": steps, "droppedSteps": f.DroppedSteps, "proposals": proposals}})
	case "rehearse-chat-text":
		var body struct {
			RequestID         string      `json:"requestId"`
			Mode              engine.Mode `json:"mode"`
			ExpectedVersionID string      `json:"expectedVersionId"`
			SyntaxVersion     int         `json:"syntaxVersion"`
		}
		if !decodeControl(w, r, &body) {
			return
		}
		result, e := store.RehearseOperatorChatText(ctx, actor, body.RequestID, body.Mode, body.ExpectedVersionID, body.SyntaxVersion)
		if e != nil {
			automationControlError(w, e)
			return
		}
		text := result.Text
		proposals := make([]map[string]any, 0, len(text.Proposals))
		for _, p := range text.Proposals {
			proposals = append(proposals, map[string]any{"id": p.ID, "sequence": p.Sequence, "automationId": p.AutomationID, "versionId": p.VersionID, "nodeId": p.NodeID, "text": p.Text})
		}
		writeJSON(w, map[string]any{"captureVersion": result.CaptureVersion, "syntaxVersion": result.SyntaxVersion, "selectedVersionId": result.SelectedVersionID, "run": automationRunJSON(result.Run), "availableInputs": []string{"time"}, "chatText": map[string]any{"id": text.ID, "flowId": text.FlowID, "mode": text.Mode, "graphDigest": text.GraphDigest, "captureDigest": text.CaptureDigest, "variableFingerprint": text.VariableFingerprint, "proposals": proposals}})
	case "list-traces":
		var body struct {
			RequestID string `json:"requestId"`
		}
		if !decodeControl(w, r, &body) {
			return
		}
		result, e := store.ListOperatorTraces(ctx, actor, body.RequestID)
		if e != nil {
			automationControlError(w, e)
			return
		}
		items := make([]map[string]any, 0, len(result.Traces))
		for _, item := range result.Traces {
			items = append(items, map[string]any{"traceId": item.ID, "versionId": item.VersionID, "mode": item.Mode, "digest": item.Digest, "createdAt": item.CreatedAt})
		}
		writeJSON(w, map[string]any{"run": automationRunJSON(result.Run), "traces": items})
	case "save-trace":
		var body struct {
			RequestID         string      `json:"requestId"`
			Mode              engine.Mode `json:"mode"`
			ExpectedVersionID string      `json:"expectedVersionId"`
		}
		if !decodeControl(w, r, &body) {
			return
		}
		result, e := store.SaveOperatorTrace(ctx, actor, body.RequestID, body.Mode, body.ExpectedVersionID)
		if e != nil {
			automationControlError(w, e)
			return
		}
		writeJSON(w, automationTraceJSON(result))
	case "load-trace":
		var body struct {
			RequestID string `json:"requestId"`
			TraceID   string `json:"traceId"`
		}
		if !decodeControl(w, r, &body) {
			return
		}
		result, e := store.LoadOperatorTrace(ctx, actor, body.RequestID, body.TraceID)
		if e != nil {
			automationControlError(w, e)
			return
		}
		writeJSON(w, automationTraceJSON(result))
	default:
		http.NotFound(w, r)
	}
}
func automationPublicationJSON(p tenancy.AutomationPublication) map[string]any {
	return map[string]any{"automationId": p.AutomationID, "versionId": p.VersionID, "digest": p.Digest, "graphDigest": p.GraphDigest, "createdAt": p.CreatedAt, "unchanged": p.Unchanged}
}
func automationRunJSON(r tenancy.OperatorRun) map[string]any {
	return map[string]any{"runId": r.ID, "occurrenceId": r.OccurrenceID, "automationId": r.AutomationID, "versionId": r.VersionID, "graphDigest": r.GraphDigest, "createdAt": r.CreatedAt, "deadline": r.Deadline, "unchanged": r.Unchanged, "state": "admitted_metadata"}
}
func automationTraceJSON(r tenancy.OperatorTraceRecord) map[string]any {
	return map[string]any{"document": r.Trace, "digest": r.Digest, "createdAt": r.CreatedAt, "unchanged": r.Unchanged}
}
func automationControlError(w http.ResponseWriter, err error) {
	status, code := 500, "automation_operation_failed"
	switch {
	case errors.Is(err, tenancy.ErrDenied):
		status, code = 403, "automation_permission_denied"
	case errors.Is(err, sql.ErrNoRows):
		status, code = 404, "automation_not_found"
	case errors.Is(err, engine.ErrDocument):
		status, code = 400, "invalid_document"
	case errors.Is(err, tenancy.ErrAutomationInvalid):
		status, code = 400, "invalid_automation_request"
	case errors.Is(err, engine.ErrChatText):
		status, code = 400, "automation_chat_text_invalid"
	case errors.Is(err, engine.ErrChatTextUnsupported):
		status, code = 409, "automation_chat_text_unsupported"
	case errors.Is(err, engine.ErrChatTextUnavailable):
		status, code = 409, "automation_chat_text_unavailable"
	case errors.Is(err, engine.ErrChatTextLimit):
		status, code = 409, "automation_chat_text_limited"
	case errors.Is(err, tenancy.ErrAutomationConflict):
		status, code = 409, "automation_version_conflict"
	case errors.Is(err, tenancy.ErrAutomationPolicy):
		status, code = 409, "automation_policy_refused"
	case errors.Is(err, tenancy.ErrAutomationCooldown):
		status, code = 409, "automation_cooldown"
	case errors.Is(err, tenancy.ErrAutomationPurged):
		status, code = 410, "automation_run_purged"
	case errors.Is(err, tenancy.ErrLimit), errors.Is(err, tenantdb.ErrLimit):
		status, code = 409, "automation_capacity_limited"
	case errors.Is(err, errAutomationWriterUnavailable):
		status, code = 503, "automation_writer_unavailable"
	case errors.Is(err, tenancy.ErrAutomationSchema):
		status, code = 503, "automation_schema_unavailable"
	case errors.Is(err, context.DeadlineExceeded):
		status, code = 504, "automation_operation_timeout"
	case errors.Is(err, context.Canceled):
		status, code = 408, "automation_request_canceled"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}
