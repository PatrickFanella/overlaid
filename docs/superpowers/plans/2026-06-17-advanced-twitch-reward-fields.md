# Advanced Twitch Reward Fields Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Recommended path:
> dispatch a fresh subagent per task, review each result with `review-quality`,
> then continue. For complex multi-agent splits, use
> `parallel-feature-development`, `team-composition-patterns`, and
> `team-communication-protocols`. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Add guarded Twitch custom reward limit fields so operators can create and update common Channel Point reward constraints without exposing unsafe Helix options.

**Architecture:** Keep reward management inside the existing operator-gated reward queue surface. Backend HTTP remains the Adapter to Twitch Helix; `twitch_rewards.go` owns request validation and paired Helix payload construction. The frontend reward queue stays a single live-operations Module and sends a diff of changed operator edits for selected-reward updates.

**Tech Stack:** Go HTTP handlers/tests, Twitch Helix `channel_points/custom_rewards`, React + TypeScript dashboard, Vitest/SSR route tests, Playwright smoke, Makefile verification.

---

## Scope

Add these advanced-but-safe fields:

- `globalCooldownSeconds` (`0` disables global cooldown; positive values enable it)
- `maxPerStream` (`0` disables max-per-stream; positive values enable it)
- `maxPerUserPerStream` (`0` disables max-per-user-per-stream; positive values enable it)
- update existing `prompt` and `userInputRequired` on selected rewards

Do **not** add:

- `should_redemptions_skip_request_queue`
- image/background color editing
- automatic fulfillment behavior
- delete behavior changes

Safety boundaries:

- For updates, send Twitch paired settings together: `is_global_cooldown_enabled` with `global_cooldown_seconds` when enabled, `is_max_per_stream_enabled` with `max_per_stream` when enabled, and `is_max_per_user_per_stream_enabled` with `max_per_user_per_stream` when enabled. For create, omit disabled optional limit pairs and rely on Twitch defaults.
- Use `0` as the dashboard's explicit “disabled” value for limits.
- Keep pause/disable guidance before delete guidance.

Twitch documented bounds used by this sprint:

- `title`: max 45 characters, unique per broadcaster.
- `cost`: minimum 1.
- `prompt`: max 200 characters; include a prompt when viewer input is required.
- `global_cooldown_seconds`: Twitch's raw minimum is 1 when enabled, but this app enforces `0` or `>=60` because Twitch requires at least 60 seconds for the cooldown to appear in its UX.
- `max_per_stream`: minimum 1 when enabled.
- `max_per_user_per_stream`: minimum 1 when enabled.
- Deleting a reward marks any `UNFULFILLED` redemptions as `FULFILLED` in Twitch, so delete remains last resort.

---

## Files

- Modify `backend/internal/http/twitch_rewards.go`
  - Extend create/update request structs.
  - Decode advanced reward fields returned by Twitch.
  - Validate field limits.
  - Build paired Helix payload keys.
- Modify `backend/internal/http/twitch_rewards_test.go`
  - Add focused tests for create/update advanced field payloads and validation.
- Modify `web/src/lib/api.ts`
  - Extend `TwitchReward`, `TwitchRewardCreatePayload`, and `TwitchRewardUpdatePayload`.
- Modify `web/src/routes/dashboard/components/twitch-interactions/TwitchRewardQueueManager.tsx`
  - Add create-form limit fields.
  - Add selected-reward edit fields for prompt, user-input, cooldown, and max constraints.
  - Submit only explicit update payloads through existing `updateTwitchReward`.
- Modify `web/src/App.test.tsx`
  - Lock SSR safety copy for the advanced fields without requiring loaded Twitch data.
- Modify `docs/runbooks/twitch-interaction-rules.md`
  - Replace “advanced fields stay in Twitch” with the new supported-field boundary.
- Modify `docs/superpowers/plans/2026-06-16-fossabot-parity-phase-2.md`
  - Mark Task 3 shipped/deferred boundary.
- Modify `docs/superpowers/plans/2026-06-15-fossabot-parity-roadmap.md`
  - Update carried-forward advanced reward fields language.

---

## Historical implementation recipe

The unchecked steps below are the original implementation recipe, including pre-implementation failure expectations. They are not an outstanding-work checklist. The implemented closeout and current verification evidence appear at the end of this document.

## Task 1: Backend request/response contracts and tests

**Files:**

- Modify: `backend/internal/http/twitch_rewards.go`
- Modify: `backend/internal/http/twitch_rewards_test.go`

- [ ] **Step 1: Extend backend reward structs**

In `backend/internal/http/twitch_rewards.go`, update the reward/request structs to this shape:

```go
type twitchReward struct {
	ID                          string `json:"id"`
	Title                       string `json:"title"`
	Prompt                      string `json:"prompt"`
	Cost                        int    `json:"cost"`
	Enabled                     bool   `json:"is_enabled"`
	Paused                      bool   `json:"is_paused"`
	InStock                     bool   `json:"is_in_stock"`
	UserInputRequired           bool   `json:"is_user_input_required"`
	GlobalCooldownEnabled       bool   `json:"is_global_cooldown_enabled"`
	GlobalCooldownSeconds       int    `json:"global_cooldown_seconds"`
	MaxPerStreamEnabled         bool   `json:"is_max_per_stream_enabled"`
	MaxPerStream                int    `json:"max_per_stream"`
	MaxPerUserPerStreamEnabled  bool   `json:"is_max_per_user_per_stream_enabled"`
	MaxPerUserPerStream         int    `json:"max_per_user_per_stream"`
	Manageable                  bool   `json:"manageable,omitempty"`
}

type twitchRewardCreateRequest struct {
	Title                   string `json:"title"`
	Prompt                  string `json:"prompt,omitempty"`
	Cost                    int    `json:"cost"`
	Enabled                 bool   `json:"enabled"`
	UserInputRequired       bool   `json:"userInputRequired"`
	GlobalCooldownSeconds   int    `json:"globalCooldownSeconds,omitempty"`
	MaxPerStream            int    `json:"maxPerStream,omitempty"`
	MaxPerUserPerStream     int    `json:"maxPerUserPerStream,omitempty"`
}

type twitchRewardUpdateRequest struct {
	Enabled                 *bool   `json:"enabled,omitempty"`
	Paused                  *bool   `json:"paused,omitempty"`
	Prompt                  *string `json:"prompt,omitempty"`
	UserInputRequired       *bool   `json:"userInputRequired,omitempty"`
	GlobalCooldownSeconds   *int    `json:"globalCooldownSeconds,omitempty"`
	MaxPerStream            *int    `json:"maxPerStream,omitempty"`
	MaxPerUserPerStream     *int    `json:"maxPerUserPerStream,omitempty"`
}
```

- [ ] **Step 2: Add backend tests for advanced create payloads**

Add this test to `backend/internal/http/twitch_rewards_test.go` near the existing create tests:

```go
func TestTwitchRewardCreatePostsAdvancedLimits(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", TwitchClientID: "client-id"}, events.NewBus(4))
	server.twitch.token = &twitchTokenInfo{AccessToken: "access", Scope: []string{"channel:manage:redemptions"}, ExpiresAt: time.Now().Add(time.Hour), UserID: "123", Login: "broadcaster"}

	var body string
	oldClient := twitchHTTPClient
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		data, _ := io.ReadAll(req.Body)
		body = string(data)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"reward-1","title":"Hydrate","cost":100,"is_global_cooldown_enabled":true,"global_cooldown_seconds":60,"is_max_per_stream_enabled":true,"max_per_stream":3,"is_max_per_user_per_stream_enabled":true,"max_per_user_per_stream":1}]}`)), Header: make(http.Header)}, nil
	})}
	t.Cleanup(func() { twitchHTTPClient = oldClient })

	reward, err := server.createTwitchReward(t.Context(), twitchRewardCreateRequest{Title: "Hydrate", Cost: 100, GlobalCooldownSeconds: 60, MaxPerStream: 3, MaxPerUserPerStream: 1})
	if err != nil {
		t.Fatalf("create reward: %v", err)
	}
	if reward.GlobalCooldownSeconds != 60 || reward.MaxPerStream != 3 || reward.MaxPerUserPerStream != 1 {
		t.Fatalf("decoded advanced limits = %#v", reward)
	}
	for _, want := range []string{`"is_global_cooldown_enabled":true`, `"global_cooldown_seconds":60`, `"is_max_per_stream_enabled":true`, `"max_per_stream":3`, `"is_max_per_user_per_stream_enabled":true`, `"max_per_user_per_stream":1`} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %s in body: %s", want, body)
		}
	}
}
```

- [ ] **Step 3: Add backend tests for update payloads and validation**

Add these tests to `backend/internal/http/twitch_rewards_test.go` near the existing update tests:

```go
func TestTwitchRewardUpdatePatchesAdvancedLimits(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", TwitchClientID: "client-id"}, events.NewBus(4))
	server.twitch.token = &twitchTokenInfo{AccessToken: "access", Scope: []string{"channel:manage:redemptions"}, ExpiresAt: time.Now().Add(time.Hour), UserID: "123", Login: "broadcaster"}

	var body string
	oldClient := twitchHTTPClient
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		data, _ := io.ReadAll(req.Body)
		body = string(data)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"reward-1","title":"Hydrate","cost":100,"prompt":"Bring water","is_user_input_required":true,"is_global_cooldown_enabled":false,"is_max_per_stream_enabled":true,"max_per_stream":5,"is_max_per_user_per_stream_enabled":false}]}`)), Header: make(http.Header)}, nil
	})}
	t.Cleanup(func() { twitchHTTPClient = oldClient })

	prompt := "Bring water"
	userInputRequired := true
	globalCooldownSeconds := 0
	maxPerStream := 5
	maxPerUserPerStream := 0
	reward, err := server.updateTwitchReward(t.Context(), "reward-1", twitchRewardUpdateRequest{Prompt: &prompt, UserInputRequired: &userInputRequired, GlobalCooldownSeconds: &globalCooldownSeconds, MaxPerStream: &maxPerStream, MaxPerUserPerStream: &maxPerUserPerStream})
	if err != nil {
		t.Fatalf("update reward: %v", err)
	}
	if reward.Prompt != prompt || !reward.UserInputRequired || reward.MaxPerStream != 5 {
		t.Fatalf("decoded updated reward = %#v", reward)
	}
	for _, want := range []string{`"prompt":"Bring water"`, `"is_user_input_required":true`, `"is_global_cooldown_enabled":false`, `"is_max_per_stream_enabled":true`, `"max_per_stream":5`, `"is_max_per_user_per_stream_enabled":false`} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %s in body: %s", want, body)
		}
	}
	if strings.Contains(body, "global_cooldown_seconds") || strings.Contains(body, "max_per_user_per_stream") {
		t.Fatalf("disabled limits should not send value fields: %s", body)
	}
}

func TestTwitchRewardCreateRejectsInvalidAdvancedLimits(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", TwitchClientID: "client-id"}, events.NewBus(4))
	server.twitch.token = &twitchTokenInfo{AccessToken: "access", Scope: []string{"channel:manage:redemptions"}, ExpiresAt: time.Now().Add(time.Hour), UserID: "123", Login: "broadcaster"}

	if _, err := server.createTwitchReward(t.Context(), twitchRewardCreateRequest{Title: "Hydrate", Cost: 100, GlobalCooldownSeconds: -1}); err == nil || !strings.Contains(err.Error(), "global cooldown seconds must be zero or greater") {
		t.Fatalf("expected global cooldown validation error, got %v", err)
	}
	if _, err := server.createTwitchReward(t.Context(), twitchRewardCreateRequest{Title: "Hydrate", Cost: 100, MaxPerStream: -1}); err == nil || !strings.Contains(err.Error(), "max per stream must be zero or greater") {
		t.Fatalf("expected max per stream validation error, got %v", err)
	}
	if _, err := server.createTwitchReward(t.Context(), twitchRewardCreateRequest{Title: "Hydrate", Cost: 100, MaxPerUserPerStream: -1}); err == nil || !strings.Contains(err.Error(), "max per user per stream must be zero or greater") {
		t.Fatalf("expected max per user validation error, got %v", err)
	}
}
```

- [ ] **Step 4: Run tests and confirm failure before implementation**

Run from `backend/`:

```bash
go test ./internal/http -run 'TestTwitchReward'
```

Expected before Task 2 implementation: compile/test failures for missing struct fields or missing payload behavior.

---

## Task 2: Backend implementation for paired Twitch reward fields

**Files:**

- Modify: `backend/internal/http/twitch_rewards.go`
- Modify: `backend/internal/http/twitch_rewards_test.go`

- [ ] **Step 1: Add create payload helper**

Add this helper in `backend/internal/http/twitch_rewards.go` below `rewardReadToken`:

```go
func addRewardLimitPayload(payload map[string]any, globalCooldownSeconds, maxPerStream, maxPerUserPerStream int) error {
	if globalCooldownSeconds < 0 {
		return fmt.Errorf("global cooldown seconds must be zero or greater")
	}
	if maxPerStream < 0 {
		return fmt.Errorf("max per stream must be zero or greater")
	}
	if maxPerUserPerStream < 0 {
		return fmt.Errorf("max per user per stream must be zero or greater")
	}
	if globalCooldownSeconds > 0 {
		if globalCooldownSeconds < 60 {
			return fmt.Errorf("global cooldown seconds must be 0 or at least 60")
		}
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

- [ ] **Step 2: Add update payload helpers**

Add this helper below `addRewardLimitPayload`:

```go
func addRewardLimitPatch(payload map[string]any, field string, enabledKey string, valueKey string, value *int) error {
	if value == nil {
		return nil
	}
	if *value < 0 {
		return fmt.Errorf("%s must be zero or greater", field)
	}
	if field == "global cooldown seconds" && *value > 0 && *value < 60 {
		return fmt.Errorf("global cooldown seconds must be 0 or at least 60")
	}
	payload[enabledKey] = *value > 0
	if *value > 0 {
		payload[valueKey] = *value
	}
	return nil
}
```

- [ ] **Step 3: Wire advanced fields into create**

In `createTwitchReward`, after adding the optional prompt, call:

```go
if err := addRewardLimitPayload(payload, request.GlobalCooldownSeconds, request.MaxPerStream, request.MaxPerUserPerStream); err != nil {
	return nil, err
}
```

Keep existing title/cost/prompt validation exactly as-is.

- [ ] **Step 4: Wire advanced fields into update**

In `updateTwitchReward`, after existing `Enabled`/`Paused` handling, add:

```go
if request.Prompt != nil {
	prompt := strings.TrimSpace(*request.Prompt)
	if len(prompt) > 200 {
		return nil, fmt.Errorf("reward prompt must be 200 characters or fewer")
	}
	payload["prompt"] = prompt
}
if request.UserInputRequired != nil {
	payload["is_user_input_required"] = *request.UserInputRequired
}
if err := addRewardLimitPatch(payload, "global cooldown seconds", "is_global_cooldown_enabled", "global_cooldown_seconds", request.GlobalCooldownSeconds); err != nil {
	return nil, err
}
if err := addRewardLimitPatch(payload, "max per stream", "is_max_per_stream_enabled", "max_per_stream", request.MaxPerStream); err != nil {
	return nil, err
}
if err := addRewardLimitPatch(payload, "max per user per stream", "is_max_per_user_per_stream_enabled", "max_per_user_per_stream", request.MaxPerUserPerStream); err != nil {
	return nil, err
}
```

Keep the existing empty-patch guard after all fields are considered:

```go
if len(payload) == 0 {
	return nil, fmt.Errorf("at least one reward field is required")
}
```

- [ ] **Step 5: Run focused backend checks**

Run:

```bash
gofmt -w backend/internal/http/twitch_rewards.go backend/internal/http/twitch_rewards_test.go
cd backend && go test ./internal/http -run 'TestTwitchReward'
```

Expected: PASS.

- [ ] **Step 6: Commit backend slice**

```bash
git add backend/internal/http/twitch_rewards.go backend/internal/http/twitch_rewards_test.go
git commit -m "feat: add advanced twitch reward backend fields"
```

---

## Task 3: Frontend API contracts and SSR tests

**Files:**

- Modify: `web/src/lib/api.ts`
- Modify: `web/src/App.test.tsx`

- [ ] **Step 1: Extend frontend reward API types**

In `web/src/lib/api.ts`, replace the reward types with this shape:

```ts
export type TwitchReward = {
  id: string;
  title: string;
  prompt?: string;
  cost: number;
  is_enabled?: boolean;
  is_paused?: boolean;
  is_in_stock?: boolean;
  is_user_input_required?: boolean;
  is_global_cooldown_enabled?: boolean;
  global_cooldown_seconds?: number;
  is_max_per_stream_enabled?: boolean;
  max_per_stream?: number;
  is_max_per_user_per_stream_enabled?: boolean;
  max_per_user_per_stream?: number;
  manageable?: boolean;
};
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

- [ ] **Step 2: Add SSR copy contract test**

In `web/src/App.test.tsx`, add or extend a reward queue test so it asserts always-rendered advanced-field copy:

```ts
it('renders reward queue advanced field guidance', () => {
  const html = renderToString(<TwitchInteractionsManager apiBaseUrl="http://localhost:8080" operatorSecret="secret" />);

  expect(html).toContain('Reward queue');
  expect(html).toContain('Global cooldown seconds');
  expect(html).toContain('Max per stream');
  expect(html).toContain('Max per user/stream');
  expect(html).toContain('zero to disable optional limits');
});
```

If `TwitchInteractionsManager` is already rendered in an existing test, merge these assertions into that test instead of adding duplicate setup.

- [ ] **Step 3: Run frontend tests and confirm failure before UI implementation**

Run:

```bash
pnpm --dir web run test
```

Expected before Task 4 implementation: SSR test fails because the new advanced-field labels/copy do not render yet.

---

## Task 4: Frontend reward queue advanced field UI

**Files:**

- Modify: `web/src/routes/dashboard/components/twitch-interactions/TwitchRewardQueueManager.tsx`

- [ ] **Step 1: Extend draft state**

In `TwitchRewardQueueManager.tsx`, update the local draft type and default:

```ts
type RewardDraft = {
  title: string;
  prompt: string;
  cost: number;
  enabled: boolean;
  userInputRequired: boolean;
  globalCooldownSeconds: number;
  maxPerStream: number;
  maxPerUserPerStream: number;
};

type RewardEditDraft = {
  prompt: string;
  userInputRequired: boolean;
  globalCooldownSeconds: number;
  maxPerStream: number;
  maxPerUserPerStream: number;
};

const emptyRewardDraft: RewardDraft = {
  title: '',
  prompt: '',
  cost: 100,
  enabled: false,
  userInputRequired: false,
  globalCooldownSeconds: 0,
  maxPerStream: 0,
  maxPerUserPerStream: 0,
};
```

- [ ] **Step 2: Add reward edit draft for selected reward updates**

Add state and helpers after the existing `rewardDraft` state:

```ts
const [rewardEditDraft, setRewardEditDraft] = useState({
  prompt: '',
  userInputRequired: false,
  globalCooldownSeconds: 0,
  maxPerStream: 0,
  maxPerUserPerStream: 0,
});

function rewardLimitValue(enabled?: boolean, value?: number) {
  return enabled ? value ?? 0 : 0;
}

function rewardEditDraftFromReward(reward?: TwitchReward): RewardEditDraft {
  return {
    prompt: reward?.prompt ?? '',
    userInputRequired: reward?.is_user_input_required ?? false,
    globalCooldownSeconds: rewardLimitValue(reward?.is_global_cooldown_enabled, reward?.global_cooldown_seconds),
    maxPerStream: rewardLimitValue(reward?.is_max_per_stream_enabled, reward?.max_per_stream),
    maxPerUserPerStream: rewardLimitValue(reward?.is_max_per_user_per_stream_enabled, reward?.max_per_user_per_stream),
  };
}
```

When `selectedRewardId` changes in the `<select>`, reset the edit draft from the newly selected reward:

```ts
const nextReward = rewards.find((reward) => reward.id === event.target.value);
setSelectedRewardId(event.target.value);
setRewardEditDraft(rewardEditDraftFromReward(nextReward));
setDeleteRewardConfirmed(false);
```

After `loadRewards` chooses `nextSelectedRewardId`, also set `rewardEditDraft` from that reward so refresh preserves the visible values:

```ts
const nextSelectedReward = loadedRewards.find((reward) => reward.id === nextSelectedRewardId);
setRewardEditDraft(rewardEditDraftFromReward(nextSelectedReward));
```

- [ ] **Step 3: Validate advanced draft values before create/update**

Add this helper:

```ts
function validateRewardLimits(values: { globalCooldownSeconds: number; maxPerStream: number; maxPerUserPerStream: number }) {
  if (values.globalCooldownSeconds < 0) return 'Global cooldown seconds must be zero or greater.';
  if (values.globalCooldownSeconds > 0 && values.globalCooldownSeconds < 60) return 'Global cooldown seconds must be 0 or at least 60 so Twitch can display it.';
  if (values.maxPerStream < 0) return 'Max per stream must be zero or greater.';
  if (values.maxPerUserPerStream < 0) return 'Max per user/stream must be zero or greater.';
  return '';
}
```

Call it in `createReward()` after the cost check and before `createTwitchReward(...)`:

```ts
const limitError = validateRewardLimits(rewardDraft);
if (limitError) {
  setRewardError(limitError);
  return;
}
```

Add a new update function:

```ts
async function updateSelectedRewardAdvancedFields() {
  if (!selectedReward) return;
  const limitError = validateRewardLimits(rewardEditDraft);
  if (limitError) {
    setRewardError(limitError);
    return;
  }
  try {
    const currentDraft = rewardEditDraftFromReward(selectedReward);
    const patch = {
      ...(rewardEditDraft.prompt !== currentDraft.prompt ? { prompt: rewardEditDraft.prompt } : {}),
      ...(rewardEditDraft.userInputRequired !== currentDraft.userInputRequired ? { userInputRequired: rewardEditDraft.userInputRequired } : {}),
      ...(rewardEditDraft.globalCooldownSeconds !== currentDraft.globalCooldownSeconds ? { globalCooldownSeconds: rewardEditDraft.globalCooldownSeconds } : {}),
      ...(rewardEditDraft.maxPerStream !== currentDraft.maxPerStream ? { maxPerStream: rewardEditDraft.maxPerStream } : {}),
      ...(rewardEditDraft.maxPerUserPerStream !== currentDraft.maxPerUserPerStream ? { maxPerUserPerStream: rewardEditDraft.maxPerUserPerStream } : {}),
    };
    if (Object.keys(patch).length === 0) {
      setRewardError('No reward field changes to save.');
      return;
    }
    await updateTwitchReward(selectedReward.id, patch, apiBaseUrl, operatorSecret);
    await loadRewards(selectedReward.id);
  } catch (error) {
    setRewardError(error instanceof Error ? error.message : 'Failed to update reward fields.');
  }
}
```

- [ ] **Step 4: Render create-form advanced fields**

Add these fields below the prompt input in the create form:

```tsx
<p className="text-xs text-zinc-400 md:col-span-4">Use zero to disable optional limits; global cooldown must be 0 or at least 60 seconds for Twitch display.</p>
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

- [ ] **Step 5: Render selected-reward edit fields**

Inside the selected reward context, after the lifecycle buttons and before delete controls, add:

```tsx
<div className="mt-3 grid gap-2 border-t border-cyan-400/20 pt-3 md:grid-cols-3">
  <label className="grid gap-1 text-xs text-zinc-300 md:col-span-3">
    <span className="hud-kicker">Prompt</span>
    <input className="hud-input" type="text" maxLength={200} value={rewardEditDraft.prompt} onChange={(event) => setRewardEditDraft((current) => ({ ...current, prompt: event.target.value }))} />
  </label>
  <label className="flex items-center gap-2 text-xs text-zinc-300 md:col-span-3">
    <input type="checkbox" checked={rewardEditDraft.userInputRequired} onChange={(event) => setRewardEditDraft((current) => ({ ...current, userInputRequired: event.target.checked }))} />
    Require viewer input
  </label>
  <label className="grid gap-1 text-xs text-zinc-300">
    <span className="hud-kicker">Global cooldown seconds</span>
    <input className="hud-input" type="number" min={0} value={rewardEditDraft.globalCooldownSeconds} onChange={(event) => setRewardEditDraft((current) => ({ ...current, globalCooldownSeconds: Number(event.target.value) }))} />
  </label>
  <label className="grid gap-1 text-xs text-zinc-300">
    <span className="hud-kicker">Max per stream</span>
    <input className="hud-input" type="number" min={0} value={rewardEditDraft.maxPerStream} onChange={(event) => setRewardEditDraft((current) => ({ ...current, maxPerStream: Number(event.target.value) }))} />
  </label>
  <label className="grid gap-1 text-xs text-zinc-300">
    <span className="hud-kicker">Max per user/stream</span>
    <input className="hud-input" type="number" min={0} value={rewardEditDraft.maxPerUserPerStream} onChange={(event) => setRewardEditDraft((current) => ({ ...current, maxPerUserPerStream: Number(event.target.value) }))} />
  </label>
  <p className="text-xs text-zinc-400 md:col-span-2">Use zero to disable optional limits; global cooldown must be 0 or at least 60 seconds for Twitch display.</p>
  <button className="hud-chip self-end" type="button" onClick={() => void updateSelectedRewardAdvancedFields()}>
    Save reward fields
  </button>
</div>
```

- [ ] **Step 6: Run frontend checks**

Run:

```bash
pnpm --dir web run format
pnpm --dir web run test
```

Expected: PASS.

- [ ] **Step 7: Commit frontend slice**

```bash
git add web/src/lib/api.ts web/src/App.test.tsx web/src/routes/dashboard/components/twitch-interactions/TwitchRewardQueueManager.tsx
git commit -m "feat: add advanced reward field controls"
```

---

## Task 5: Documentation and roadmap closeout

**Files:**

- Modify: `docs/runbooks/twitch-interaction-rules.md`
- Modify: `docs/superpowers/plans/2026-06-16-fossabot-parity-phase-2.md`
- Modify: `docs/superpowers/plans/2026-06-15-fossabot-parity-roadmap.md`
- Modify: `docs/superpowers/plans/2026-06-17-advanced-twitch-reward-fields.md`

- [ ] **Step 1: Update reward runbook boundaries**

In `docs/runbooks/twitch-interaction-rules.md`, replace the first safety-boundary bullet with:

```markdown
- Reward creation and updates support title/cost/prompt/enabled/user-input-required plus guarded cooldown and max redemption limits: global cooldown seconds, max per stream, and max per user per stream. Use `0` to disable optional limit fields. High-risk fields such as `should_redemptions_skip_request_queue`, image uploads, and color/image styling stay in Twitch for now.
```

Add one QA evidence bullet:

```markdown
- [ ] Advanced reward fields use `0` as disabled in the dashboard, backend update calls send disabled paired flags, and pause/disable remains the preferred reversible control before delete.
```

- [ ] **Step 2: Update Phase 2 status**

In `docs/superpowers/plans/2026-06-16-fossabot-parity-phase-2.md`, add a closeout note near the Phase 2 status section:

```markdown
### Advanced reward fields closeout

- Shipped guarded advanced Twitch reward fields: global cooldown seconds, max per stream, max per user per stream, prompt edits, and user-input-required edits.
- Still deferred: `should_redemptions_skip_request_queue`, image/background styling, reward image upload, and any automatic fulfillment behavior.
```

- [ ] **Step 3: Update main roadmap carried-forward item**

In `docs/superpowers/plans/2026-06-15-fossabot-parity-roadmap.md`, change the carried-forward advanced reward field language to:

```markdown
- Twitch reward fields still intentionally deferred: skip-request-queue, background color/image styling, reward image upload, and any automatic fulfillment behavior. Common guarded fields now live in the reward queue runbook.
```

- [ ] **Step 4: Close this sprint plan**

Append this closeout section to this file after implementation:

```markdown
## Sprint closeout

- Backend create/update payloads support paired Twitch limit fields and prompt/user-input updates.
- Frontend reward queue can create and update common advanced reward fields while keeping pause/disable before delete.
- Docs now distinguish supported guarded fields from intentionally deferred high-risk fields.
- Verification passed with the commands actually run for this sprint, expected to include focused Go reward tests, frontend format/test, `make verify`, `make smoke`, and `make smoke-browser`.
```

- [ ] **Step 5: Run docs verification and commit**

Run:

```bash
git diff --check
```

Expected: PASS.

Commit:

```bash
git add docs/runbooks/twitch-interaction-rules.md docs/superpowers/plans/2026-06-16-fossabot-parity-phase-2.md docs/superpowers/plans/2026-06-15-fossabot-parity-roadmap.md docs/superpowers/plans/2026-06-17-advanced-twitch-reward-fields.md
git commit -m "docs: close advanced reward fields sprint"
```

---

## Task 6: Final verification

**Files:** No source files expected unless verification exposes defects.

- [ ] **Step 1: Run full verification**

Run from repo root:

```bash
make verify
```

Expected: format/lint, Go tests, frontend tests, backend build, and web build pass.

- [ ] **Step 2: Run smoke checks**

Start or refresh the stack first:

```bash
docker compose up -d --build
```

Then run:

```bash
make smoke
make smoke-browser
```

Expected: API/web smoke pass; OBS readiness may remain degraded in local environments where OBS WebSocket is not running, but the smoke target itself should exit successfully.

- [ ] **Step 3: Remove generated browser artifacts if present**

```bash
rm -rf web/test-results
```

- [ ] **Step 4: Inspect final status**

```bash
git status --short
git log --oneline -10
```

Expected: clean working tree after commits; latest commits include backend, frontend, docs closeout, and any verification fixes.

---

## Self-review

- Spec coverage: covers the explicit deferred advanced reward field sprint from Phase 2 and keeps high-risk Helix fields out of scope.
- Placeholder scan: no TBD/TODO/fill-in instructions; every code-changing step includes exact code or exact target behavior.
- Type consistency: backend JSON names are camelCase request fields mapped to Twitch snake_case Helix payload keys; frontend types use existing API naming conventions.

## Sprint closeout

- Backend create/update payloads support paired Twitch limit fields and prompt/user-input updates.
- Frontend reward queue can create and update common advanced reward fields while keeping pause/disable before delete.
- Docs now distinguish supported guarded fields from intentionally deferred high-risk fields.
- Verification passed: focused Go reward tests, frontend format/test, backend/frontend/docs slice reviews, `git diff --check`, `make verify`, `make smoke`, and `make smoke-browser`.
- Local smoke note: `/readyz` remained degraded for local EventSub/OBS reachability/auth in this environment, but the smoke targets passed and reward-management routes rendered.


### 2026-10-06 contract reconciliation

Create/update payload and decoding contracts remain covered by `twitch_rewards_test.go`; current browser workflows cover edit retry, lifecycle writes, deletion, duplicate submission prevention, stale reads, and receipts after failed reloads. The backend and both dashboard drafts now also reject cooldowns above 604800 seconds. Transport fixtures accept exactly 604800 and prove 604801 does not dispatch. Scope text in bot-ops and the parent roadmap now reflects shipped advanced fields and reversible lifecycle controls. Live Twitch execution remains unverified without credentials.
