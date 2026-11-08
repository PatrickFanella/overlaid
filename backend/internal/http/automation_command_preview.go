package httpapi

import (
	"context"
	"net/http"

	"git.subcult.tv/PatrickFanella/over-laid/internal/templatepreview"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
)

func commandMigrationProjection(item tenancy.CommandMigrationSummary) map[string]any {
	return map[string]any{"id": item.ID, "name": item.Name, "enabled": item.Enabled, "action": item.Action, "responseMode": item.ResponseMode, "roleRequired": item.RoleRequired, "cooldownSeconds": item.CooldownSeconds, "snapshot": item.Snapshot, "kind": item.Kind}
}

func automationCommandMigration(ctx context.Context, w http.ResponseWriter, r *http.Request, store *tenancy.AutomationStore, actor, operation string) {
	if operation == "list-command-migration" {
		var body struct{}
		if !decodeControl(w, r, &body) {
			return
		}
		items, err := store.ListCommandMigration(ctx, actor)
		if err != nil {
			automationControlError(w, err)
			return
		}
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			out = append(out, commandMigrationProjection(item))
		}
		writeJSON(w, map[string]any{"state": "command_catalogue_only", "commands": out})
		return
	}
	var body struct {
		CommandID string                 `json:"commandId"`
		Snapshot  string                 `json:"snapshot"`
		Values    *templatePreviewValues `json:"values"`
	}
	if !decodeControl(w, r, &body) {
		return
	}
	if !validTemplatePreviewValues(body.Values) {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	source, err := store.ReadCommandMigration(ctx, actor, body.CommandID, body.Snapshot)
	if err != nil {
		automationControlError(w, err)
		return
	}
	out := map[string]any{"state": "command_preview_only", "command": commandMigrationProjection(source.CommandMigrationSummary), "status": "editorial_contract_required", "template": nil}
	if source.Kind == "template" {
		result, e := templatepreview.Preview(ctx, source.Response, suppliedTemplateContext(body.Values))
		if e != nil {
			templatePreviewError(w, e)
			return
		}
		out["status"], out["template"] = "template_review", templatePreviewProjection(result)
	}
	writeJSON(w, out)
}
