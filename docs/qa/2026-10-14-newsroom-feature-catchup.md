# Newsroom final feature catch-up

The user confirmed full feature catch-up for PRs #8–#17. The previous #1–#7 heads are unchanged. This pass uses the integrated #17 head as its fixed cutoff; it does not claim coverage of later source arrivals. Over-Laid base is `033e288`, main after merged PR #48. The owner's `/home/onnwee/Work/Forks/newsroom` checkout remains clean and unchanged; fetched source refs live in the disposable comparison clone.

| Source PR | Capability | Reviewed head |
| --- | --- | --- |
| [#8](https://github.com/PatrickFanella/newsroom/pull/8) | Reusable preparation templates | `4a477a83d1e992f812930762b1f318937cd4cf0c` |
| [#9](https://github.com/PatrickFanella/newsroom/pull/9) | Owner usage and estimated monthly allowances | `f15d4854ac25bcd32f3445131827285324fbe3cb` |
| [#10](https://github.com/PatrickFanella/newsroom/pull/10) | Segment timing targets | `4be284df8a22069073f3cae924fb74850925404a` |
| [#11](https://github.com/PatrickFanella/newsroom/pull/11) | Transactional moderator activity history | `cf24c9e254bf444c71d639b9e7c2acf017195fe3` |
| [#12](https://github.com/PatrickFanella/newsroom/pull/12) | Archived broadcasts and covered-story search | `83d12a7c7969fe8bd63fdbe9647ff1144fd28d7a` |
| [#13](https://github.com/PatrickFanella/newsroom/pull/13) | Previewed per-show Discord exports with chunk receipts | `565fd64a76bb0cf71267d00ace725b1a0358b1b5` |
| [#14](https://github.com/PatrickFanella/newsroom/pull/14) | Carry unused stories to a working shelf | `ef660f77c0f08044bdfbbf769669cc598e5e0b16` |
| [#15](https://github.com/PatrickFanella/newsroom/pull/15) | Explicit public episode snapshots | `fb3771c5c2c9a2324eaa0dc88c34b467b89c10f9` |
| [#16](https://github.com/PatrickFanella/newsroom/pull/16) | Twitch show boundaries and manual correction | `e736c50a462a8a4ce9e6b15afc4cb44bcc15fb51` |
| [#17](https://github.com/PatrickFanella/newsroom/pull/17) | Complete feature integration and combined contracts | `d7f66d201d5bf51c246c341f15d62bc7d37dafe7` |

## Adaptation

- Preparation shelves own independent segment definitions, optional template prompts, and whole-minute targets. Fresh working copies retain empty segments, source notes/metadata and ungrouped position. Copying to the deck skips approved duplicate URLs and empty segments without provider refresh. Shared/exported shelves and narrow quick-add reads exclude private prompts/template status.
- Owner monthly estimates reserve AI/search attempts atomically with policy checks in the retained tenant document. Pauses, allowance, warning threshold, UTC rollover, failure retention, and fail-closed dispatch use existing plan HTTP reservations as well. Two preferred-source searches reserve together. Usage starts with this feature because older Over-Laid HTTP counters cannot reliably identify paid operation types. Blocked enrichment remains skipped until explicit retry.
- Moderator activity saves trusted actor IDs, title/URL/status snapshots, and changed field names with the successful write. Private text/tokens and provider-only changes are excluded. Archive/activity searches use literal matching, fifty-row pages, nanosecond timestamps, and resource/filter-bound cursors.
- Per-show Discord previews retain exact chunks and independent receipts. Known failures resume; completed chunks are not resent. Uncertain outcomes block automatic resend. Unlike the source's at-least-once policy, Over-Laid preserves its existing fail-closed uncertain-delivery contract. Provider credentials and Discord redirects are refused.
- Carry-forward uses the historical show's own play records, preserves saved segment/source positions and available current prep/metadata, falls back after queue cleanup, and retains an idempotent destination receipt. It does not change the live deck.
- Public episodes save an allowlisted projection with summaries/takeaways excluded by default. Fingerprints fence publication; stable republish URLs, revocation, and newly generated tokens use hashed capabilities. A new URL is shown once and kept only in that browser's session storage. Revoke/publish rotates it. PostgreSQL publication/index updates share one transaction. Viewer deletion revokes derived episodes and removes saved exports/activity.
- Twitch automation uses the existing authenticated EventSub WebSocket and broadcaster token, rather than adding the source's separate HTTPS webhook transport. It registers/checks/repairs only the current session's lifecycle pair, recreates it on reconnect, and persists event/stream fencing and watermark state with show writes. Manual correction is owner-only, revision checked, overlap/play constrained, and offset recalculating. Public snapshots and saved Discord chunks remain frozen.
- Combined contracts retain asynchronous approval, expected-status checks, editorial/provider isolation, idempotent Played revisions, atomic play/Undo notes, and independent bundle snapshots.

## Verification

`bash scripts/ci-postgres.sh` passes the required `make verify`, disposable PostgreSQL integration fixtures, and full uncached Go race suite. Frontend: 133 tests across five files. TypeScript, ESLint, Go vet, builds, Compose validation, and Open Pilot template checks pass. Regression coverage includes concurrent PostgreSQL allowance reservations, independent copies, public capability/RLS/revocation, viewer deletion of derived projections, immutable saved Discord versions, precise archive/activity pagination, status fencing, idempotent playback, provider/Discord redirect refusal, lifecycle pair repair scope, refreshed repair tokens, grace-transfer subscription preservation, broadcaster fencing, and stale/manual boundary behavior.

Browser qualification used fresh synthetic records under `/tmp/overlaid-newsroom-catchup-qa/`, API port 41884, and web port 41882:

1. Enabled a template, saved private prompts, added a timed and an empty segment, assigned a source, created an independent working shelf, and copied its populated rundown to the deck.
2. Started a named show, edited a takeaway, played one source, ended the show, and carried the unused source into one retained working shelf.
3. Previewed exact per-show Discord messages without sending them.
4. Previewed/published an episode with private text excluded, rendered it through the public route, revoked it, observed rejection of the old URL, and published a different URL.
5. Corrected UTC boundaries, produced a competing-update conflict, reloaded, observed the retained stale draft, and explicitly loaded the latest boundaries.
6. Searched retained takeaways, inspected activity without private note text, saved a zero allowance, and observed the warning threshold. Enabling Twitch automation while disconnected returned a visible error.
7. Tested 375px usage/show/public-episode views without horizontal overflow. The untouched boundary form now refreshes after End while edited drafts stay intact.

All provider credentials were blank in the browser fixture. Paid calls, Twitch registration/events and Discord delivery were mocked in automated tests. No real messages, paid provider calls, production migration or deployment were performed. Live provider authorization/delivery and production rollout remain separate qualification steps.

## Rollout

Apply `0005_newsroom_episodes.sql` after the existing Newsroom migration before the matching runtime. Runtime startup requires the updated capability lookup. Other feature state uses the retained tenant document and single-writer file fallback. Preserve a matching document checkpoint for rollback; older binaries can drop added fields on rewrite. Retain additive SQL schema. Boundaries: 200 shows, 2,000 cards/bundles, fifty shelves, fifty segments per shelf/deck, twelve bundle sources, 10,000 play records, 20,000 activity records, and 1,000 show-export versions.
