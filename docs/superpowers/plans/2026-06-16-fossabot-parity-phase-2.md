# FossaBot Parity Phase 2 Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Recommended path:
> dispatch a fresh subagent per task, review each result with `review-quality`,
> then continue. For complex multi-agent splits, use
> `parallel-feature-development`, `team-composition-patterns`, and
> `team-communication-protocols`. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Finish the remaining high-value FossaBot parity gaps after the first roadmap: safer Twitch reward lifecycle controls, richer custom-reward fields, and an explicit uptime-parity plan.

**Architecture:** Keep Twitch reward management in the existing operator-gated Helix integration under `backend/internal/http/twitch_rewards.go` and the existing `/dashboard/config` reward queue UI. Prefer reversible reward state changes (`pause`, `resume`, `disable`, `enable`) over destructive delete, and keep advanced reward fields opt-in with paired Twitch settings sent together. Treat cloud-hosted uptime parity as an operations track first: document the deployment/monitoring target before changing deployment code.

**Tech Stack:** Go backend HTTP handlers/tests, Twitch Helix custom reward APIs, React/Vite TypeScript dashboard, existing operator-secret auth, existing docs/runbooks, `make verify`, `make smoke`, `make smoke-browser`.

---

## Current baseline

Phase 2 status after committed cleanup:

- Shipped: backend `PATCH /api/twitch/rewards/{rewardId}` lifecycle updates for `enabled` and `paused`.
- Shipped: dashboard pause/resume and enable/disable controls before delete.
- Shipped: uptime parity options document at `docs/superpowers/plans/2026-06-16-fossabot-uptime-parity-options.md`.
- Shipped: guarded advanced reward fields for global cooldown seconds, max per stream, max per user per stream, prompt edits, and user-input-required edits.
- Deferred: high-risk/nonessential reward fields such as `should_redemptions_skip_request_queue`, image/background styling, reward image upload, and any automatic fulfillment behavior.

Already implemented and committed before this Phase 2 plan:

- Bot audit/status dashboard, timers, custom commands, template variables, chat replies, API variables, counters, and quotes.
- Channel-point rules with cooldown seconds, templates, and fulfillment notes.
- Reward queue for manageable rewards, unfulfilled redemptions, fulfillment/cancel actions, and minimal create/delete.
- Moderation rules, manual Twitch moderation actions, guarded nuke dry-run/execute, temporary radiation windows, and rollback.

Remaining roadmap items from `docs/superpowers/plans/2026-06-15-fossabot-parity-roadmap.md`:

- High-risk/nonessential Twitch reward fields such as skip-request-queue, image/background styling, reward image upload, and automatic fulfillment behavior.
- Cloud-hosted uptime parity with FossaBot.

### Advanced reward fields closeout

- Shipped guarded advanced Twitch reward fields: global cooldown seconds, max per stream, max per user per stream, prompt edits, and user-input-required edits.
- Still deferred: `should_redemptions_skip_request_queue`, image/background styling, reward image upload, and any automatic fulfillment behavior.

Out of scope unless explicitly requested:

- Viewer/chat-supplied arbitrary API URLs.
- Deployment, DNS, or production infrastructure changes.
- Automatically fulfilling, canceling, refunding, creating, or deleting rewards from chat.

---

## File structure

### Backend reward lifecycle

- Modify `backend/internal/http/twitch_rewards.go` — add PATCH/update request types, update handler, validation, and Helix update helper.
- Modify `backend/internal/http/routes.go` — add `PATCH /api/twitch/rewards/{rewardId}` behind `requireOperator`.
- Modify `backend/internal/http/twitch_rewards_test.go` — add scope, validation, and Helix payload tests for reward updates.

### Frontend reward lifecycle

- Modify `web/src/lib/api.ts` — add `TwitchRewardUpdatePayload` and `updateTwitchReward(...)` client.
- Modify `web/src/routes/dashboard/components/TwitchInteractionsManager.tsx` — show selected reward lifecycle state and add pause/resume/enable/disable controls before delete.
- Modify `web/src/App.test.tsx` only if helper-level tests are added for payload construction or UI-independent reward helpers.

### Docs and roadmap

- Modify `docs/runbooks/twitch-interaction-rules.md` — document pause/disable-first operation and remaining advanced-field boundaries.
- Modify `docs/superpowers/plans/2026-06-15-fossabot-parity-roadmap.md` — update current status after each completed Phase 2 slice.
- Keep this file as the execution handoff for Phase 2.

### Uptime parity planning

- Modify or create a later runbook only after Task 4 decides the target, likely under `docs/runbooks/`.
- Do not edit Docker, Caddy, DNS, deploy, or CI files in this plan without a separate user approval.

---

## Task 1: Add safer reward pause/enable update API

**Goal:** Make Twitch reward lifecycle reversible by adding a guarded update endpoint for `is_enabled` and `is_paused`, so operators can pause/disable rewards instead of deleting them.

**Files:**

- Modify: `backend/internal/http/twitch_rewards.go`
- Modify: `backend/internal/http/routes.go`
- Test: `backend/internal/http/twitch_rewards_test.go`

- [ ] **Step 1: Write failing backend tests for update scope, validation, and Helix PATCH**

Add tests to `backend/internal/http/twitch_rewards_test.go`:

```go
func TestTwitchRewardUpdateRequiresManageScope(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", TwitchClientID: "client"}, events.NewBus(4))
	server.twitch.token = &twitchTokenInfo{AccessToken: "access", Scope: []string{"channel:read:redemptions"}, ExpiresAt: time.Now().Add(time.Hour), UserID: "123", Login: "broadcaster"}

	_, err := server.updateTwitchReward(t.Context(), "reward-1", twitchRewardUpdateRequest{Paused: boolPtr(true)})
	if err == nil || !strings.Contains(err.Error(), "channel:manage:redemptions") {
		t.Fatalf("expected missing manage scope, got %v", err)
	}
}

func TestTwitchRewardUpdateValidatesPatch(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", TwitchClientID: "client"}, events.NewBus(4))
	server.twitch.token = &twitchTokenInfo{AccessToken: "access", Scope: []string{"channel:manage:redemptions"}, ExpiresAt: time.Now().Add(time.Hour), UserID: "123", Login: "broadcaster"}

	_, err := server.updateTwitchReward(t.Context(), "", twitchRewardUpdateRequest{Paused: boolPtr(true)})
	if err == nil || !strings.Contains(err.Error(), "reward id is required") {
		t.Fatalf("expected reward id validation error, got %v", err)
	}

	_, err = server.updateTwitchReward(t.Context(), "reward-1", twitchRewardUpdateRequest{})
	if err == nil || !strings.Contains(err.Error(), "at least one reward field") {
		t.Fatalf("expected empty patch validation error, got %v", err)
	}
}

func TestTwitchRewardUpdatePatchesLifecycle(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", TwitchClientID: "client-id"}, events.NewBus(4))
	server.twitch.token = &twitchTokenInfo{AccessToken: "access", Scope: []string{"channel:manage:redemptions"}, ExpiresAt: time.Now().Add(time.Hour), UserID: "123", Login: "broadcaster"}

	var body string
	oldClient := twitchHTTPClient
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPatch {
			t.Fatalf("expected PATCH, got %s", req.Method)
		}
		if got := req.URL.Query().Get("broadcaster_id"); got != "123" {
			t.Fatalf("expected broadcaster_id=123, got %q", got)
		}
		if got := req.URL.Query().Get("id"); got != "reward-1" {
			t.Fatalf("expected reward id, got %q", got)
		}
		data, _ := io.ReadAll(req.Body)
		body = string(data)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"reward-1","title":"Hydrate","cost":100,"is_enabled":false,"is_paused":true}]}`)), Header: make(http.Header)}, nil
	})}
	t.Cleanup(func() { twitchHTTPClient = oldClient })

	reward, err := server.updateTwitchReward(t.Context(), "reward-1", twitchRewardUpdateRequest{Enabled: boolPtr(false), Paused: boolPtr(true)})
	if err != nil {
		t.Fatalf("update reward: %v", err)
	}
	if reward.ID != "reward-1" || reward.Enabled || !reward.Paused || !reward.Manageable {
		t.Fatalf("unexpected reward: %#v", reward)
	}
	if !strings.Contains(body, `"is_enabled":false`) || !strings.Contains(body, `"is_paused":true`) {
		t.Fatalf("unexpected update body: %s", body)
	}
}

func boolPtr(value bool) *bool { return &value }
```

- [ ] **Step 2: Run focused tests and confirm they fail**

Run from `backend/`:

```bash
go test ./internal/http -run 'TestTwitchRewardUpdate'
```

Expected: FAIL because `twitchRewardUpdateRequest`, `updateTwitchReward`, and `boolPtr` support do not exist yet.

- [ ] **Step 3: Add update route**

In `backend/internal/http/routes.go`, register the route beside the existing reward routes:

```go
router.HandleFunc("PATCH /api/twitch/rewards/{rewardId}", s.requireOperator(s.twitchRewardUpdate))
```

- [ ] **Step 4: Add update request/handler/helper**

In `backend/internal/http/twitch_rewards.go`, add this request type near the existing create/delete types:

```go
type twitchRewardUpdateRequest struct {
	Enabled *bool `json:"enabled,omitempty"`
	Paused  *bool `json:"paused,omitempty"`
}
```

Add this handler near `twitchRewardCreate` and `twitchRewardDelete`:

```go
func (s *Server) twitchRewardUpdate(w http.ResponseWriter, r *http.Request) {
	var request twitchRewardUpdateRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	reward, err := s.updateTwitchReward(r.Context(), r.PathValue("rewardId"), request)
	if err != nil {
		http.Error(w, err.Error(), twitchRewardsHTTPStatus(err))
		return
	}
	writeJSON(w, twitchRewardMutationResponse{Reward: reward, Result: "updated"})
}
```

Add this helper near `createTwitchReward` and `deleteTwitchReward`:

```go
func (s *Server) updateTwitchReward(ctx context.Context, rewardID string, request twitchRewardUpdateRequest) (*twitchReward, error) {
	token, err := s.rewardToken(ctx, "channel:manage:redemptions")
	if err != nil {
		return nil, err
	}
	rewardID = strings.TrimSpace(rewardID)
	if rewardID == "" {
		return nil, fmt.Errorf("reward id is required")
	}
	payload := map[string]any{}
	if request.Enabled != nil {
		payload["is_enabled"] = *request.Enabled
	}
	if request.Paused != nil {
		payload["is_paused"] = *request.Paused
	}
	if len(payload) == 0 {
		return nil, fmt.Errorf("at least one reward field is required")
	}
	query := url.Values{"broadcaster_id": {token.UserID}, "id": {rewardID}}
	var decoded struct {
		Data []twitchReward `json:"data"`
	}
	if err := s.twitchRewardsRequest(ctx, http.MethodPatch, twitchRewardsEndpoint+"?"+query.Encode(), token.AccessToken, payload, http.StatusOK, &decoded); err != nil {
		return nil, err
	}
	if len(decoded.Data) == 0 {
		return nil, fmt.Errorf("twitch rewards failed: update returned no reward")
	}
	decoded.Data[0].Manageable = true
	return &decoded.Data[0], nil
}
```

- [ ] **Step 5: Run focused backend tests**

Run from `backend/`:

```bash
gofmt -w internal/http/twitch_rewards.go internal/http/routes.go internal/http/twitch_rewards_test.go
go test ./internal/http -run 'TestTwitchReward|TestTwitchRewardsRequireRedemptionScope|TestTwitchRedemptionStatusRequiresManageScope'
```

Expected: PASS.

- [ ] **Step 6: Commit Task 1**

```bash
git add backend/internal/http/twitch_rewards.go backend/internal/http/routes.go backend/internal/http/twitch_rewards_test.go
git commit -m "feat: update twitch reward lifecycle"
```

---

## Task 2: Add pause/resume/enable/disable controls to reward queue UI

**Goal:** Make the dashboard guide operators toward reversible reward lifecycle actions before delete.

**Files:**

- Modify: `web/src/lib/api.ts`
- Modify: `web/src/routes/dashboard/components/TwitchInteractionsManager.tsx`

- [ ] **Step 1: Add frontend API types and client**

In `web/src/lib/api.ts`, add the payload type beside reward create/delete types:

```ts
export type TwitchRewardUpdatePayload = { enabled?: boolean; paused?: boolean };
```

Add this client beside `createTwitchReward` and `deleteTwitchReward`:

```ts
export async function updateTwitchReward(rewardId: string, payload: TwitchRewardUpdatePayload, explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await sendJSON('PATCH', `/api/twitch/rewards/${encodeURIComponent(rewardId)}`, payload, explicitBaseUrl, { operatorSecret });
  return (await response.json()) as TwitchRewardMutationResponse;
}
```

- [ ] **Step 2: Import update client in UI**

In `web/src/routes/dashboard/components/TwitchInteractionsManager.tsx`, add `updateTwitchReward` to the API import list:

```ts
import {
  createTwitchReward,
  deleteTwitchReward,
  fetchTwitchInteractions,
  fetchTwitchRewardRedemptions,
  fetchTwitchRewards,
  saveTwitchInteractions,
  updateTwitchRedemptionStatus,
  updateTwitchReward,
  type ChatAliasRule,
  type ChannelPointRule,
  type TwitchRedemption,
  type TwitchReward,
  type TwitchInteractionRules,
} from '../../../lib/api';
```

- [ ] **Step 3: Add lifecycle action function**

Inside `TwitchInteractionsManager`, add this function near `deleteReward()`:

```ts
  async function updateSelectedRewardLifecycle(patch: { enabled?: boolean; paused?: boolean }) {
    if (!selectedRewardId) return;
    try {
      await updateTwitchReward(selectedRewardId, patch, apiBaseUrl, operatorSecret);
      await loadRewards();
    } catch (error) {
      setRewardError(error instanceof Error ? error.message : 'Failed to update reward lifecycle.');
    }
  }
```

- [ ] **Step 4: Show lifecycle status and reversible controls before delete**

Replace the selected reward context block in `TwitchInteractionsManager` with this expanded version:

```tsx
        {selectedReward ? (
          <div className="mt-3 rounded-2xl border border-cyan-400/20 bg-cyan-400/5 p-3 text-xs text-zinc-200">
            <p className="hud-kicker">Selected reward context</p>
            <div className="mt-1 flex flex-wrap items-center gap-2">
              <p className="font-semibold text-cyan-100">
                {selectedReward.title} · {selectedReward.cost}
              </p>
              <span className="hud-chip hud-chip--square">{selectedReward.is_enabled === false ? 'disabled' : 'enabled'}</span>
              <span className="hud-chip hud-chip--square">{selectedReward.is_paused ? 'paused' : 'not paused'}</span>
              {selectedReward.is_in_stock === false ? <span className="hud-chip hud-chip--square">out of stock</span> : null}
            </div>
            {selectedRewardRule?.fulfillmentNote ? <p className="mt-1 text-zinc-200">Fulfillment note: {selectedRewardRule.fulfillmentNote}</p> : <p className="mt-1 text-zinc-400">No saved channel-point rule match.</p>}
            <div className="mt-3 flex flex-wrap gap-2">
              <button className="hud-chip" type="button" onClick={() => updateSelectedRewardLifecycle({ paused: !selectedReward.is_paused })}>
                {selectedReward.is_paused ? 'Resume reward' : 'Pause reward'}
              </button>
              <button className="hud-chip" type="button" onClick={() => updateSelectedRewardLifecycle({ enabled: selectedReward.is_enabled === false })}>
                {selectedReward.is_enabled === false ? 'Enable reward' : 'Disable reward'}
              </button>
            </div>
            <p className="mt-2 text-zinc-400">Pause or disable first when you need a reversible change. Delete remains destructive and may fulfill pending redemptions on Twitch.</p>
          </div>
        ) : null}
```

- [ ] **Step 5: Run frontend checks**

Run:

```bash
pnpm --dir web run format
pnpm --dir web run test
```

Expected: PASS.

- [ ] **Step 6: Commit Task 2**

```bash
git add web/src/lib/api.ts web/src/routes/dashboard/components/TwitchInteractionsManager.tsx
git commit -m "feat: add safer reward lifecycle controls"
```

---

## Task 3: Add advanced reward fields in a guarded second slice

**Goal:** Let operators create or update common Twitch reward limits without exposing every Helix field at once.

**Scope:** Add global cooldown, per-stream max, per-user-per-stream max, and optional prompt/user-input edits. Do not add `should_redemptions_skip_request_queue` in this task.

**Files:**

- Modify: `backend/internal/http/twitch_rewards.go`
- Modify: `backend/internal/http/twitch_rewards_test.go`
- Modify: `web/src/lib/api.ts`
- Modify: `web/src/routes/dashboard/components/TwitchInteractionsManager.tsx`
- Modify: `docs/runbooks/twitch-interaction-rules.md`

- [ ] **Step 1: Extend backend request types**

In `backend/internal/http/twitch_rewards.go`, extend create/update request types:

```go
type twitchRewardCreateRequest struct {
	Title                    string `json:"title"`
	Prompt                   string `json:"prompt,omitempty"`
	Cost                     int    `json:"cost"`
	Enabled                  bool   `json:"enabled"`
	UserInputRequired        bool   `json:"userInputRequired"`
	GlobalCooldownSeconds    int    `json:"globalCooldownSeconds,omitempty"`
	MaxPerStream             int    `json:"maxPerStream,omitempty"`
	MaxPerUserPerStream      int    `json:"maxPerUserPerStream,omitempty"`
}

type twitchRewardUpdateRequest struct {
	Enabled                  *bool   `json:"enabled,omitempty"`
	Paused                   *bool   `json:"paused,omitempty"`
	Prompt                   *string `json:"prompt,omitempty"`
	UserInputRequired        *bool   `json:"userInputRequired,omitempty"`
	GlobalCooldownSeconds    *int    `json:"globalCooldownSeconds,omitempty"`
	MaxPerStream             *int    `json:"maxPerStream,omitempty"`
	MaxPerUserPerStream      *int    `json:"maxPerUserPerStream,omitempty"`
}
```

- [ ] **Step 2: Add paired-setting validation helper**

Add this helper in `twitch_rewards.go`:

```go
func addRewardLimitPayload(payload map[string]any, globalCooldownSeconds, maxPerStream, maxPerUserPerStream int) error {
	if globalCooldownSeconds < 0 || maxPerStream < 0 || maxPerUserPerStream < 0 {
		return fmt.Errorf("reward limits must be zero or greater")
	}
	if globalCooldownSeconds > 0 {
		payload["is_global_cooldown_enabled"] = true
		payload["global_cooldown_seconds"] = globalCooldownSeconds
	}
	if maxPerStream > 0 {
		payload["is_max_per_stream_enabled"] = true
		payload["max_per_stream"] = maxPerStream
	}
	if maxPerUserPerStream > 0 {
		payload["is_max_per_user_per_stream_enabled"] = true
		payload["max_per_user_per_stream"] = maxPerUserPerStream
	}
	return nil
}
```

For update, send both paired values when a pointer is non-nil:

```go
if request.GlobalCooldownSeconds != nil {
	if *request.GlobalCooldownSeconds < 0 {
		return nil, fmt.Errorf("global cooldown seconds must be zero or greater")
	}
	payload["is_global_cooldown_enabled"] = *request.GlobalCooldownSeconds > 0
	if *request.GlobalCooldownSeconds > 0 {
		payload["global_cooldown_seconds"] = *request.GlobalCooldownSeconds
	}
}
if request.MaxPerStream != nil {
	if *request.MaxPerStream < 0 {
		return nil, fmt.Errorf("max per stream must be zero or greater")
	}
	payload["is_max_per_stream_enabled"] = *request.MaxPerStream > 0
	if *request.MaxPerStream > 0 {
		payload["max_per_stream"] = *request.MaxPerStream
	}
}
if request.MaxPerUserPerStream != nil {
	if *request.MaxPerUserPerStream < 0 {
		return nil, fmt.Errorf("max per user per stream must be zero or greater")
	}
	payload["is_max_per_user_per_stream_enabled"] = *request.MaxPerUserPerStream > 0
	if *request.MaxPerUserPerStream > 0 {
		payload["max_per_user_per_stream"] = *request.MaxPerUserPerStream
	}
}
```

- [ ] **Step 3: Add tests for advanced field payloads**

Add backend tests that assert create/update payloads include paired setting fields:

```go
func TestTwitchRewardCreatePostsAdvancedLimits(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", TwitchClientID: "client-id"}, events.NewBus(4))
	server.twitch.token = &twitchTokenInfo{AccessToken: "access", Scope: []string{"channel:manage:redemptions"}, ExpiresAt: time.Now().Add(time.Hour), UserID: "123", Login: "broadcaster"}

	var body string
	oldClient := twitchHTTPClient
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		data, _ := io.ReadAll(req.Body)
		body = string(data)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"reward-1","title":"Hydrate","cost":100}]}`)), Header: make(http.Header)}, nil
	})}
	t.Cleanup(func() { twitchHTTPClient = oldClient })

	_, err := server.createTwitchReward(t.Context(), twitchRewardCreateRequest{Title: "Hydrate", Cost: 100, GlobalCooldownSeconds: 60, MaxPerStream: 3, MaxPerUserPerStream: 1})
	if err != nil {
		t.Fatalf("create reward: %v", err)
	}
	for _, want := range []string{`"is_global_cooldown_enabled":true`, `"global_cooldown_seconds":60`, `"is_max_per_stream_enabled":true`, `"max_per_stream":3`, `"is_max_per_user_per_stream_enabled":true`, `"max_per_user_per_stream":1`} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %s in body: %s", want, body)
		}
	}
}
```

- [ ] **Step 4: Add frontend fields**

Extend `TwitchRewardCreatePayload` and `TwitchRewardUpdatePayload` in `web/src/lib/api.ts`:

```ts
export type TwitchRewardCreatePayload = {
  title: string;
  prompt?: string;
  cost: number;
  enabled: boolean;
  userInputRequired: boolean;
  globalCooldownSeconds?: number;
  maxPerStream?: number;
  maxPerUserPerStream?: number;
};

export type TwitchRewardUpdatePayload = {
  enabled?: boolean;
  paused?: boolean;
  prompt?: string;
  userInputRequired?: boolean;
  globalCooldownSeconds?: number;
  maxPerStream?: number;
  maxPerUserPerStream?: number;
};
```

Update `rewardDraft` in `TwitchInteractionsManager.tsx` so zero means “disabled” for optional limits:

```ts
const [rewardDraft, setRewardDraft] = useState({
  title: '',
  prompt: '',
  cost: 100,
  enabled: false,
  userInputRequired: false,
  globalCooldownSeconds: 0,
  maxPerStream: 0,
  maxPerUserPerStream: 0,
});
```

Reset to the same shape after a successful create:

```ts
setRewardDraft({
  title: '',
  prompt: '',
  cost: 100,
  enabled: false,
  userInputRequired: false,
  globalCooldownSeconds: 0,
  maxPerStream: 0,
  maxPerUserPerStream: 0,
});
```

Add these fields to the create form below the prompt field:

```tsx
<label className="grid gap-1 text-xs text-zinc-300">
  <span className="hud-kicker">Global cooldown seconds</span>
  <input className="hud-input" type="number" min={0} value={rewardDraft.globalCooldownSeconds} onChange={(event) => setRewardDraft((current) => ({ ...current, globalCooldownSeconds: Number(event.target.value) }))} />
</label>
<label className="grid gap-1 text-xs text-zinc-300">
  <span className="hud-kicker">Max per stream</span>
  <input className="hud-input" type="number" min={0} value={rewardDraft.maxPerStream} onChange={(event) => setRewardDraft((current) => ({ ...current, maxPerStream: Number(event.target.value) }))} />
</label>
<label className="grid gap-1 text-xs text-zinc-300">
  <span className="hud-kicker">Max per user/stream</span>
  <input className="hud-input" type="number" min={0} value={rewardDraft.maxPerUserPerStream} onChange={(event) => setRewardDraft((current) => ({ ...current, maxPerUserPerStream: Number(event.target.value) }))} />
</label>
```

- [ ] **Step 5: Run checks and commit**

Run:

```bash
gofmt -w backend/internal/http/twitch_rewards.go backend/internal/http/twitch_rewards_test.go
rtk go test ./internal/http -run 'TestTwitchReward'
pnpm --dir web run format
pnpm --dir web run test
```

Run the Go command from the repo root with `rtk`, or run `go test ./internal/http -run 'TestTwitchReward'` from `backend/`.

Commit:

```bash
git add backend/internal/http/twitch_rewards.go backend/internal/http/twitch_rewards_test.go web/src/lib/api.ts web/src/routes/dashboard/components/TwitchInteractionsManager.tsx docs/runbooks/twitch-interaction-rules.md
git commit -m "feat: add advanced twitch reward fields"
```

---

## Task 4: Regroup uptime parity into an operations decision

**Goal:** Turn “cloud-hosted uptime parity” into an explicit decision record before touching deployment infrastructure.

**Files:**

- Create: `docs/superpowers/plans/2026-06-16-fossabot-uptime-parity-options.md`
- Modify: `docs/superpowers/plans/2026-06-15-fossabot-parity-roadmap.md`

- [ ] **Step 1: Write uptime options document**

Create `docs/superpowers/plans/2026-06-16-fossabot-uptime-parity-options.md` with this content:

```markdown
# FossaBot Uptime Parity Options

**Question:** What does “cloud-hosted uptime parity” mean for over-laid?

## Option A: Keep self-hosted, add stronger readiness and alerting

- Keep current self-hosted Docker/Postgres model.
- Add documented health checks, external uptime probes, and ntfy/monitoring alerts.
- Best when OBS/local streaming integration matters more than SaaS-style availability.

## Option B: Split control plane from local stream worker

- Host dashboard/config/audit storage on a reliable server.
- Keep OBS/websocket/media actions on the local stream machine.
- Requires a secure worker connection model and careful secret boundaries.

## Option C: Full hosted bot service

- Host bot command/timer/moderation/reward workflows in the cloud.
- Local overlay/OBS features become optional clients.
- Highest uptime parity with FossaBot, but highest architecture and security cost.

## Recommendation

Start with Option A unless a real production requirement demands Option B or C. It improves uptime confidence without weakening over-laid's local OBS/operator safety model.
```

- [ ] **Step 2: Update main roadmap carry-forward**

In `docs/superpowers/plans/2026-06-15-fossabot-parity-roadmap.md`, change the carried-forward cloud item to reference the options document:

```markdown
- Cloud-hosted uptime parity with FossaBot; see `docs/superpowers/plans/2026-06-16-fossabot-uptime-parity-options.md` before changing deployment architecture.
```

- [ ] **Step 3: Commit Task 4**

```bash
git add docs/superpowers/plans/2026-06-16-fossabot-uptime-parity-options.md docs/superpowers/plans/2026-06-15-fossabot-parity-roadmap.md
git commit -m "docs: plan fossabot uptime parity options"
```

---

## Task 5: Final Phase 2 verification and roadmap cleanup

**Goal:** Keep docs aligned with shipped behavior and avoid stale roadmap checkboxes.

**Files:**

- Modify: `docs/superpowers/plans/2026-06-15-fossabot-parity-roadmap.md`
- Modify: `docs/runbooks/twitch-interaction-rules.md`
- Modify: `README.md` only if user-facing operation links change.

- [ ] **Step 1: Run full verification**

Run from repo root:

```bash
make verify
make smoke
make smoke-browser
```

Expected: all pass. If browser smoke writes `web/test-results/.last-run.json`, remove generated test artifacts before committing.

- [ ] **Step 2: Update status bullets**

In `docs/superpowers/plans/2026-06-15-fossabot-parity-roadmap.md`, ensure “Current status” mentions:

```markdown
- Twitch reward inventory, reversible pause/disable lifecycle controls, advanced bounded reward fields, minimal reward creation/deletion, and unfulfilled redemption queue controls for fulfill/cancel actions.
```

Ensure “Deliberately carried forward” no longer lists advanced reward fields if Task 3 shipped. Keep destructive/high-risk `should_redemptions_skip_request_queue` and cloud architecture as carry-forward if not shipped.

- [ ] **Step 3: Run doc consistency review manually**

Check these files for contradictions:

```bash
git diff -- docs/runbooks/twitch-interaction-rules.md docs/superpowers/plans/2026-06-15-fossabot-parity-roadmap.md docs/superpowers/plans/2026-06-16-fossabot-parity-phase-2.md
```

Look for contradictions around:

- whether delete is preferred or last resort;
- whether pause/disable exists;
- whether advanced fields are available;
- whether cloud hosting is implemented or only planned.

- [ ] **Step 4: Commit cleanup**

```bash
git add docs/superpowers/plans/2026-06-15-fossabot-parity-roadmap.md docs/runbooks/twitch-interaction-rules.md README.md
git commit -m "docs: update fossabot phase 2 status"
```

---

## Recommended execution order

Historical note: Tasks 1, 2, 3, 4, and 5 have now shipped or been closed out. This order is retained as implementation history rather than active guidance.

1. Task 1 — backend reversible reward update API.
2. Task 2 — dashboard pause/resume/enable/disable controls.
3. Stop and run `make verify && make smoke-browser`.
4. Task 4 — uptime options document.
5. Task 3 — advanced reward fields, now shipped in `docs/superpowers/plans/2026-06-17-advanced-twitch-reward-fields.md`.
6. Task 5 — cleanup after whichever Phase 2 slices actually ship.

## Risk notes

- Twitch only lets the app/client that created a reward manage it. Keep `only_manageable_rewards=true` as the default list behavior.
- Deleting a reward with unfulfilled redemptions can mark them fulfilled on Twitch. Keep delete behind confirmation and visually subordinate to pause/disable.
- Paired Twitch fields must be sent together for advanced settings. Never send only `max_per_stream` without `is_max_per_stream_enabled`.
- `should_redemptions_skip_request_queue` remains excluded because it auto-fulfills redemptions and can bypass operator review.
- Cloud-hosted uptime parity is not just a code feature. It changes trust boundaries, data location, and local OBS access.

## Verification policy

Every committed code slice must pass:

```bash
make verify
make smoke-browser
```

Runtime-affecting slices must also pass:

```bash
make smoke
curl -fsS http://localhost:8080/readyz | jq .
```

Do not push or deploy unless explicitly requested.
