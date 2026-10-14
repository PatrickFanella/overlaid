# Newsroom fork comparison and fixes

Compared the clean Kvant checkout `/home/onnwee/Work/Forks/newsroom` at `b435e064a48f23462e48d441e39f345d3505b6c3`, its recent fix history, the two open reliability PRs, and the active Newsroom broadcast-session worktree. The fork and its worktrees were read only. Over-Laid's earlier uncommitted integration and unified Chat command manager were preserved.

## Comparison

| Source work | Over-Laid finding and action |
| --- | --- |
| [PR 1](https://github.com/PatrickFanella/newsroom/pull/1), `d48de90502df38728bafe6a1856527fa9a4ca509`: durable asynchronous enrichment | Approval already committed without waiting for providers. Remaining jobs were held in memory and automatic AI could run on pending submissions. Persisted job state, approval-only automatic AI, lease claims, fenced completion, interrupted-work recovery, explicit retry, and preservation of editorial/provider metadata were added. |
| [PR 2](https://github.com/PatrickFanella/newsroom/pull/2), `6f39e473710585ed69b222a342a9ab67852e9a5a`: play history and exact export acknowledgement | Playback was already an atomic document write, but deleting an archived card erased its notes. Added retained note snapshots, idempotent Played, Undo cancellation, replay snapshots, detached history, serialized desk writes, and chunk receipts under a delivery lease. Markdown downloads were already repeatable full snapshots with no timestamp cursor or Supabase row cap. They remain independent from Discord. |
| `eb780ac`: autosave on-air notes | Unsaved component state was lost on switching. Added debounce/blur/unmount saving, browser-session draft retention, conditional editorial-field checks, visible failure, and explicit conflict recovery. |
| `8f6e5da`, `bbdef53`, `e3d7136`: approver and direct segment approval | Added approval actor/time retention, visible attribution, and atomic approval into a segment. |
| `5c55af6`: independent audience title | The rebuilt app had only a source-title edit. Added a separate resettable overlay/chat title; history keeps the source title. |
| Audience trigger-warning propagation | Manual audience warnings now appear in the overlay payload/view and at the beginning of current-source chat announcements. AI warning signals remain internal. |
| `b435e06`: segment-preserving shared shelves/imports | Sharing/imports flattened segment assignments and lost imported titles. Added segment metadata and assignments to the public/export format and fresh segment IDs on import. Private notes, provider data, unused segments, and capabilities stay excluded. |
| `98a51b2`, `e0ddf35`: stale moderator status | Added twelve-hour status expiry and ninety-six-hour roster hiding. |
| `d9e88af`, `cb15dbd`, `f9e02ca`, `218c76c`, `1d03b0d`, `328ed4a` | Already handled by explicit playlist import, URL normalization, active-link dedup, immediate atomic writes, explicit on-air selection, and exact/suffix host risk rules. No duplicate implementation added. |
| Account/Twitch identity separation and no-channel operation | Over-Laid already uses independent accounts/tenants and provider connections. The desk works without Twitch. No Supabase identity/session implementation copied. |
| Active `feat/broadcast-sessions-vod-chapters` worktree | Observed unfinished broadcast APIs/migration/helpers and concurrent dirty work. Over-Laid already stores stream-session association. VOD URL/offset/chapter export is a separate unfinished feature, not a fix ported in this pass. The worktree was untouched. |
| Supabase fan-out, Vercel lifecycle, service-worker HTML and Next.js refactors | Those implementation-specific defects do not apply to the Go/Vite rebuild. |

## Verification

- `bash scripts/ci-postgres.sh` passed: required `make verify`, PostgreSQL fixtures, and the full uncached Go race suite. Frontend: 124 tests across five files.
- New regressions cover queued work across file restart, competing claims, expired-lease recovery and stale completion refusal, nested snapshot isolation, approval-only AI scheduling, failure preservation, play retry/Undo/replay/removal, legacy played-card backfill in both file and PostgreSQL stores, partial Discord retry, late notes, uncertain and legacy whole-export delivery refusal, delivery lease competition, and segment import copies.
- PostgreSQL regressions use two independent Store instances and verify that concurrent enrichment and delivery claims admit exactly one owner.
- Browser checks verified immediate note editing followed by a tab switch, persisted autosave, independent broadcast title/warning in the overlay API, source title retained in show notes, and Undo/replay leaving one active note. Desktop and 390-pixel layouts had no horizontal overflow.
- All browser writes used `/tmp/overlaid-newsroom-qa/` and the isolated loopback API/web preview. Provider and Twitch credentials were disabled. Discord delivery used HTTP transport fixtures only.

No hosted CI, live Twitch/Discord/provider acceptance, commit, push, merge, or deployment is claimed. The two source PRs remain separate upstream work; inspecting them did not modify or merge them.
