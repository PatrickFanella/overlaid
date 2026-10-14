package newsroom

import (
	"context"
	"encoding/json"
)

// Only the one-time bot-registry import reads these retired fields. They are
// never exposed as current command configuration or used by dispatch.
type LegacyChatSettings struct {
	ChatEnabled     bool   `json:"chatEnabled"`
	SubmitCommand   string `json:"submitCommand"`
	VideoCommand    string `json:"videoCommand"`
	QuestionCommand string `json:"questionCommand"`
	QuestionsOpen   bool   `json:"questionsOpen"`
	AllowAnyone     bool   `json:"allowAnyone"`
	RaffleCommand   string
}

func (s *Store) LegacyChatSettings(ctx context.Context) (LegacyChatSettings, error) {
	legacy := LegacyChatSettings{SubmitCommand: "!news", VideoCommand: "!video", QuestionCommand: "!question", RaffleCommand: "!join", AllowAnyone: true}
	var raw []byte
	if s.db != nil {
		var exists bool
		err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM newsroom_documents WHERE /*tenant:newsroom_documents*/ AND id='default'),COALESCE((SELECT document FROM newsroom_documents WHERE /*tenant:newsroom_documents*/ AND id='default'),'{}'::jsonb)`).Scan(&exists, &raw)
		if err != nil {
			return legacy, err
		}
	} else {
		s.mu.Lock()
		raw = append([]byte(nil), s.legacy...)
		s.mu.Unlock()
	}
	if len(raw) > 0 {
		var doc struct {
			Settings json.RawMessage
			Raffle   struct{ Command string }
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			return legacy, err
		}
		if len(doc.Settings) > 0 {
			if err := json.Unmarshal(doc.Settings, &legacy); err != nil {
				return legacy, err
			}
		}
		if doc.Raffle.Command != "" {
			legacy.RaffleCommand = doc.Raffle.Command
		}
	}
	return legacy, nil
}
