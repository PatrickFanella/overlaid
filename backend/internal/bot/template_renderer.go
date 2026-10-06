package bot

// TemplateCommunity supplies only the read operations needed by templates.
type TemplateCommunity interface {
	CounterValue(string) (Counter, bool, error)
	RandomQuote() (Quote, bool, error)
}

type TemplateAPIVariables interface {
	ResolveAPIVariables([]string) map[string]APIVariableResult
}

// TemplateRenderer assembles referenced dependencies and applies the shared
// substitution, missing-value, diagnostic, and response-length policy.
// Callers supply actor/channel context; dependencies remain read-only.
type TemplateRenderer struct {
	Community    TemplateCommunity
	APIVariables TemplateAPIVariables
	NowPlaying   func() NowPlayingMetadata
}

func (r TemplateRenderer) Render(template string, ctx TemplateContext) TemplateResult {
	if r.NowPlaying != nil && usesNowPlaying(template) {
		metadata := r.NowPlaying()
		ctx.NowPlayingTitle, ctx.NowPlayingArtist = metadata.Title, metadata.Artist
	}
	if names := normalizeAPIVariableNames(APIVariableNames(template)); r.APIVariables != nil && len(names) > 0 {
		values := r.APIVariables.ResolveAPIVariables(names)
		ctx.APIVariables = copyTemplateValues(ctx.APIVariables)
		for _, name := range names {
			ctx.APIVariables[name] = values[name].Value
		}
	}
	if r.Community != nil {
		if names := CountVariableNames(template); len(names) > 0 {
			ctx.Counts = copyTemplateValues(ctx.Counts)
			for _, name := range names {
				ctx.Counts[name] = 0
				if counter, ok, err := r.Community.CounterValue(name); err == nil && ok {
					ctx.Counts[name] = counter.Value
				}
			}
		}
		if UsesRandomQuote(template) {
			ctx.QuoteRandom = ""
			if quote, ok, err := r.Community.RandomQuote(); err == nil && ok {
				ctx.QuoteRandom = quote.Text
				if quote.Author != "" {
					ctx.QuoteRandom += " — " + quote.Author
				}
			}
		}
	}
	return RenderTemplate(template, ctx)
}

func usesNowPlaying(template string) bool {
	for _, name := range TemplateVariableNames(template) {
		if name == "nowplaying.title" || name == "nowplaying.artist" {
			return true
		}
	}
	return false
}

func copyTemplateValues[T any](source map[string]T) map[string]T {
	result := make(map[string]T, len(source))
	for name, value := range source {
		result[name] = value
	}
	return result
}
