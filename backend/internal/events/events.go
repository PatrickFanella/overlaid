package events

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

type EventType string

const (
	EventTypeAlert       EventType = "alert"
	EventTypeClear       EventType = "clear"
	EventTypeService     EventType = "service_status"
	EventTypeChatMessage EventType = "chat_message"
	EventTypeCommand     EventType = "custom_command"
	EventTypeNowPlaying  EventType = "now_playing"
	EventTypeBRB         EventType = "brb"
)

type Severity string

const (
	SeverityInfo    Severity = "info"
	SeveritySuccess Severity = "success"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

type Event struct {
	ID        string         `json:"id"`
	Type      EventType      `json:"type"`
	Source    string         `json:"source"`
	Title     string         `json:"title,omitempty"`
	Message   string         `json:"message,omitempty"`
	Severity  Severity       `json:"severity,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
}

func NewAlert(source, title, message string, severity Severity) Event {
	return Event{
		ID:        newID(),
		Type:      EventTypeAlert,
		Source:    source,
		Title:     title,
		Message:   message,
		Severity:  severity,
		CreatedAt: time.Now().UTC(),
	}
}

func NewCommand(source, title, message string, severity Severity, data map[string]any) Event {
	return Event{
		ID:        newID(),
		Type:      EventTypeCommand,
		Source:    source,
		Title:     title,
		Message:   message,
		Severity:  severity,
		Data:      data,
		CreatedAt: time.Now().UTC(),
	}
}

func NewClear(source string) Event {
	return Event{
		ID:        newID(),
		Type:      EventTypeClear,
		Source:    source,
		CreatedAt: time.Now().UTC(),
	}
}

func NewNowPlaying(source, title, message string, data map[string]any) Event {
	return Event{
		ID:        newID(),
		Type:      EventTypeNowPlaying,
		Source:    source,
		Title:     title,
		Message:   message,
		Severity:  SeverityInfo,
		Data:      data,
		CreatedAt: time.Now().UTC(),
	}
}

func NewBRB(source, title, message string, data map[string]any) Event {
	return Event{
		ID:        newID(),
		Type:      EventTypeBRB,
		Source:    source,
		Title:     title,
		Message:   message,
		Severity:  SeverityInfo,
		Data:      data,
		CreatedAt: time.Now().UTC(),
	}
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().UTC().Format("20060102T150405.000000000Z")
	}
	return hex.EncodeToString(b[:])
}
