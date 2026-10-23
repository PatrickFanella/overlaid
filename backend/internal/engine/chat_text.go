package engine

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"git.subcult.tv/PatrickFanella/over-laid/internal/variables"
)

const MaxChatTextRunes = 500
const ChatTextTimeout = time.Second

var (
	ErrChatText            = errors.New("engine_chat_text_invalid")
	ErrChatTextUnsupported = errors.New("engine_chat_text_unsupported")
	ErrChatTextUnavailable = errors.New("engine_chat_text_unavailable")
	ErrChatTextLimit       = errors.New("engine_chat_text_limit")
)

// ChatTextInput is supplied evidence only. Explicit syntax selection is not an
// automatic migration of saved templates; no trusted source producer is added.
type ChatTextInput struct {
	Flow          RehearsalInput
	Variables     variables.Snapshot
	SyntaxVersion int
}

// ChatTextProposal is private rendered rehearsal text, not a transport request,
// durable effect key, authorization, delivery receipt or deduplication promise.
type ChatTextProposal struct {
	ID                                    string
	Sequence                              int
	AutomationID, VersionID, NodeID, Text string
}
type ChatTextRehearsal struct {
	ID, FlowID, GraphDigest, CaptureDigest, VariableFingerprint string
	Mode                                                        Mode
	Proposals                                                   []ChatTextProposal
}

// RehearseChatText regenerates supported flow from the pinned graph, then
// renders chat.send text from immutable captured values. It has no persistence,
// provider, live resolver, queue or effect callback. All errors discard output.
func RehearseChatText(ctx context.Context, g PinnedGraph, in ChatTextInput) (out ChatTextRehearsal, failure error) {
	if ctx == nil {
		return out, ErrChatText
	}
	callerCtx := ctx
	defer func() {
		if callerCtx.Err() != nil {
			failure = callerCtx.Err()
		}
		if failure != nil {
			out = ChatTextRehearsal{}
		}
	}()
	if in.SyntaxVersion != variables.SyntaxVersion || in.Variables.Fingerprint() == "" || !in.Variables.MatchesClockSeed(in.Flow.Clock, in.Flow.Seed) {
		return out, ErrChatText
	}
	ctx, cancel := context.WithTimeout(ctx, ChatTextTimeout)
	defer cancel()
	flow, err := RehearseFlow(ctx, g, in.Flow)
	if err != nil {
		return out, err
	}
	if flow.Status != "proposed" && flow.Status != "stopped" {
		return out, ErrChatTextUnsupported
	}
	out = ChatTextRehearsal{FlowID: flow.ID, GraphDigest: flow.GraphDigest, CaptureDigest: flow.CaptureDigest, VariableFingerprint: in.Variables.Fingerprint(), Mode: flow.Mode, Proposals: []ChatTextProposal{}}
	out.ID = rehearsalHash("overlaid.engine.chat-text.v1", flow.ID, out.VariableFingerprint, strconv.Itoa(in.SyntaxVersion))
	resolutions, total := 0, 0
	for _, p := range flow.Proposals {
		if err = ctx.Err(); err != nil {
			return out, err
		}
		// Threaded replies and every other capability need separate source/effect
		// contracts. Unknown arguments cannot be silently discarded into a send.
		if p.Capability != "chat.send" || len(p.Arguments) != 1 || p.Arguments[0].Name != "text" {
			return out, ErrChatTextUnsupported
		}
		template, e := variables.Parse(p.Arguments[0].Value, in.SyntaxVersion)
		if e != nil {
			return out, ErrChatText
		}
		rendered, e := variables.Render(ctx, template, in.Variables)
		if e != nil {
			if ctx.Err() != nil {
				return out, ctx.Err()
			}
			if errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) {
				return out, e
			}
			return out, ErrChatTextLimit
		}
		resolutions += rendered.Resolutions
		if resolutions > variables.MaxResolutions {
			return out, ErrChatTextLimit
		}
		if len(rendered.Diagnostics) > 0 {
			return out, ErrChatTextUnavailable
		}
		text := rendered.Text
		if !utf8.ValidString(text) || strings.ContainsRune(text, 0) || strings.TrimSpace(text) == "" {
			return out, ErrChatText
		}
		if utf8.RuneCountInString(text) > MaxChatTextRunes || len(text) > MaxProposalBytes-total {
			return out, ErrChatTextLimit
		}
		total += len(text)
		id := rehearsalHash("overlaid.engine.chat-text-proposal.v1", out.ID, strconv.Itoa(p.Sequence), p.AutomationID, p.VersionID, p.NodeID)
		out.Proposals = append(out.Proposals, ChatTextProposal{ID: id, Sequence: p.Sequence, AutomationID: p.AutomationID, VersionID: p.VersionID, NodeID: p.NodeID, Text: text})
	}
	if err = ctx.Err(); err != nil {
		return out, err
	}
	return out, nil
}
