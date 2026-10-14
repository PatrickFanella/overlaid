package newsroom

import (
	"context"
	"errors"
	"strings"
)

func (e Enricher) QueueBriefing(ctx context.Context, d Document) (string, error) {
	if e.Keys.Anthropic == "" {
		return "", errors.New("Anthropic key is not configured")
	}
	var content strings.Builder
	content.WriteString("Prepare a neutral overview of the approved running order, connecting stories only where the supplied content supports it. Mark gaps rather than inferring facts.\n")
	byID := map[string]Item{}
	for _, item := range d.Items {
		byID[item.ID] = item
	}
	count := 0
	for _, id := range d.Order {
		item, ok := byID[id]
		if !ok || item.Status != "approved" {
			continue
		}
		count++
		content.WriteString("\nStory: " + item.Title + "\nSource: " + item.URL + "\n")
		if item.Enrichment != nil {
			content.WriteString("Available source text: " + bounded(item.Enrichment.Body, 2000) + "\nPrep summary (may contain errors): " + bounded(item.Enrichment.Summary, 1000) + "\n")
		} else {
			content.WriteString("Only a title is available; do not infer the story content.\n")
		}
		if content.Len() > 14000 {
			break
		}
	}
	if count == 0 {
		return "", errors.New("approve links before generating a briefing")
	}
	client := e.Client
	if client == nil {
		client = PublicClient(nil)
	}
	result := Enrichment{Body: content.String()}
	if err := e.analyze(ctx, client, Item{}, &result); err != nil {
		return "", err
	}
	return result.Summary, nil
}
