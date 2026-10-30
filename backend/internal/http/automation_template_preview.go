package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/templatepreview"
	"git.subcult.tv/PatrickFanella/over-laid/internal/variables"
)

// Supplied fixture values only. The map bounds match the 64 captured
// definitions a snapshot can hold; names stay within the legacy token grammar
// once converted, so oversized keys are refused before preview.
const templatePreviewMaxEntries = 64
const templatePreviewMaxName = 128

type templatePreviewValues struct {
	User             string            `json:"user"`
	DisplayName      string            `json:"displayName"`
	Channel          string            `json:"channel"`
	Args             string            `json:"args"`
	Command          string            `json:"command"`
	NowPlayingTitle  string            `json:"nowPlayingTitle"`
	NowPlayingArtist string            `json:"nowPlayingArtist"`
	QuoteRandom      string            `json:"quoteRandom"`
	Counts           map[string]int    `json:"counts"`
	APIVariables     map[string]string `json:"apiVariables"`
}

// automationTemplatePreview compares a legacy template with its proposed
// syntax-2 conversion using only the request's supplied values. It reads and
// writes no tenant state, saves or selects no version and reaches no provider,
// counter, cooldown or effect path.
func automationTemplatePreview(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source *string                `json:"source"`
		Values *templatePreviewValues `json:"values"`
	}
	if !decodeControl(w, r, &body) {
		return
	}
	if body.Source == nil || body.Values == nil || len(body.Values.Counts) > templatePreviewMaxEntries || len(body.Values.APIVariables) > templatePreviewMaxEntries {
		http.Error(w, "invalid request body", 400)
		return
	}
	for name := range body.Values.Counts {
		if len(name) > templatePreviewMaxName {
			http.Error(w, "invalid request body", 400)
			return
		}
	}
	for name := range body.Values.APIVariables {
		if len(name) > templatePreviewMaxName {
			http.Error(w, "invalid request body", 400)
			return
		}
	}
	v := body.Values
	result, err := templatepreview.Preview(ctx, *body.Source, bot.TemplateContext{User: v.User, DisplayName: v.DisplayName, Channel: v.Channel, Args: v.Args, Command: v.Command, NowPlayingTitle: v.NowPlayingTitle, NowPlayingArtist: v.NowPlayingArtist, QuoteRandom: v.QuoteRandom, Counts: v.Counts, APIVariables: v.APIVariables})
	if err != nil {
		templatePreviewError(w, err)
		return
	}
	c := result.Conversion
	warnings := make([]map[string]any, 0, len(c.Warnings))
	for _, item := range c.Warnings {
		warnings = append(warnings, map[string]any{"code": item.Code, "start": item.Span.Start, "end": item.Span.End})
	}
	diagnostics := make([]map[string]any, 0, len(result.Diagnostics))
	for _, item := range result.Diagnostics {
		diagnostics = append(diagnostics, map[string]any{"code": item.Code, "start": item.Span.Start, "end": item.Span.End})
	}
	writeJSON(w, map[string]any{"state": "preview_only", "originalSyntax": c.OriginalSyntax, "proposedSyntax": c.ProposedSyntax, "originalSource": c.OriginalSource, "proposedSource": c.ProposedSource, "warnings": warnings, "legacyOutput": result.LegacyOutput, "proposedOutput": result.ProposedOutput, "different": result.Different, "diagnostics": diagnostics})
}

func templatePreviewError(w http.ResponseWriter, err error) {
	status, code := 500, "template_preview_failed"
	switch {
	case errors.Is(err, templatepreview.ErrInput):
		status, code = 400, "template_preview_input"
	case errors.Is(err, templatepreview.ErrLimit), errors.Is(err, variables.ErrRender), errors.Is(err, variables.ErrTemplate):
		status, code = 422, "template_preview_limit"
	case errors.Is(err, templatepreview.ErrSnapshot), errors.Is(err, variables.ErrSnapshot):
		status, code = 422, "template_preview_snapshot"
	case errors.Is(err, context.DeadlineExceeded):
		status, code = 504, "automation_operation_timeout"
	case errors.Is(err, context.Canceled):
		status, code = 408, "automation_request_canceled"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}
