package newsroom

import (
	"strings"
	"time"
	"unicode/utf8"
)

// Evidence retains classifications/counts, never prompts or provider payloads.
type InputEvidence struct {
	Version         int       `json:"version"`
	CheckedAt       time.Time `json:"checkedAt"`
	Source          string    `json:"source"`
	InputCharacters int       `json:"inputCharacters"`
	Truncated       bool      `json:"truncated"`
	Outcome         string    `json:"outcome"`
	Model           string    `json:"model,omitempty"`
	Retrieval       string    `json:"retrieval"`
}

func extractionEvidence(item Item, result Enrichment, pageObserved bool) *InputEvidence {
	source := "unavailable"
	if strings.TrimSpace(result.Body) != "" {
		switch {
		case strings.HasPrefix(result.Body, "Video transcript:\n"):
			source = "transcript"
		case strings.HasPrefix(result.Body, "Transcript unavailable."):
			source = "description"
		case item.Kind == "social":
			source = "post_text"
		case pageObserved:
			source = "page_text"
		default:
			source = "description"
		}
	} else if result.Title != "" || result.Description != "" {
		source = "page_metadata"
	}
	retrieval := "not_observed"
	if pageObserved {
		retrieval = "backend_observed"
	}
	if result.Error != "" && !pageObserved {
		retrieval = "failed"
	}
	return &InputEvidence{Version: 1, CheckedAt: time.Now().UTC(), Source: source, InputCharacters: utf8.RuneCountInString(bounded(result.Body, 16000)), Truncated: utf8.RuneCountInString(result.Body) > 16000, Outcome: "metadata_only", Retrieval: retrieval}
}
func aiEvidence(source *InputEvidence, model string) *InputEvidence {
	copy := *source
	copy.Model = model
	copy.CheckedAt = time.Now().UTC()
	copy.Outcome = "failed"
	// The model receives supplied text; no provider-side retrieval tool is used.
	copy.Retrieval = "supplied_text"
	return &copy
}
