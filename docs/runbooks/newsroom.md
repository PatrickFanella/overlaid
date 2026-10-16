# Newsroom

Newsroom lives at `/dashboard/newsroom` (Alt+8). It rebuilds the reaction-stream workflow from the [pinned upstream source](../vendor/newsroom/PROVENANCE.md) using Over-Laid’s Go API, React operator desk, workspace accounts, Twitch connection, and forced PostgreSQL RLS.

## Workflow

1. Submit a link manually, paste a batch of URLs, import a YouTube playlist, or enable a Newsroom action in Chat → Bot commands. Links enter moderator triage. The browser extension can add directly to an approved deck or shelf using a separate add token.
2. Inspect the source, extraction, summary, copyright signal, and any content warning. Approve or reject the link. Moderator members can submit and triage; editors, managers, and owners can run the deck and configure providers.
3. Arrange the running order with the arrow controls and show segments. Put a link on the desk, open its source or load its YouTube preview, and record a takeaway. Card edits autosave after a short pause, on blur, and when switching cards. Failed or conflicting saves retain the browser-session draft and offer explicit recovery. Mark played saves an immutable takeaway snapshot and the current stream session atomically. Undo cancels that note; replay creates a fresh one. Removing the archived queue card preserves its history.
4. Use shelves for future shows. Shared shelf URLs reveal only the shelf name, used segment names/order, and source titles, URLs, media kinds, and segment assignments. Downloading and importing a shelf preserves titles and creates fresh segment IDs. Generate or rotate a share URL explicitly; revoke it to stop access.
5. Download show notes as Markdown for all played links or a selected session. Discord delivery requires confirmation and suppresses mentions. A tenant-scoped lease serializes delivery, and receipts identify each acknowledged chunk of each retained note for that webhook. Retrying an explicit HTTP refusal skips previously acknowledged chunks. A transport failure, lost response, failed receipt write, or interrupted pending claim is uncertain and blocks automatic reposting. Inspect the channel before deciding what to send next. Legacy whole-export receipts do not contain member note IDs; their presence blocks delivery until the operator reconciles them after inspecting Discord. The app does not guess which notes they covered. Downloads are repeatable full-history snapshots and do not consume Discord receipts.

Quick sources keep recurring source URLs near the submission form. Queue briefing requests a separate AI overview of approved stories, using only available source text. Moderator availability is self-reported and always shows when it was set. Status expires to Away after twelve hours; rows older than ninety-six hours disappear from the roster. Shelf JSON exports/imports support preparing material outside the current desk.

Audience questions have their own pending, approved, answered, and rejected states. Approved questions can take over the OBS card. The raffle accepts one entry per Twitch login, closes when a winner is drawn, supports reroll excluding the previous winner, and can announce its state in connected Twitch chat. Entries use cryptographic random selection on the server.

Approvals record the actor and timestamp. A pending link can be approved directly into a show segment. The separate **Overlay / chat title** changes audience-facing copy while preserving the source title in history; clearing it restores the source title. The manually written content warning appears ahead of the source announcement in chat and on the overlay. AI warnings remain internal triage hints.

The OBS source is `/overlay/newsroom?key=<OVERLAY_TOKEN>`. It displays the current approved link title and publisher, or the selected approved question. It never exposes private card notes, pending submissions, provider credentials, or the operator secret. `/overlay/newsroom?demo=1` renders an offline sample. Add the source from the existing Overlays page.

`/dashboard/newsroom?demo=1` shows separate sample data and refuses saved changes.

## Twitch capture

Newsroom actions are entries in the general command manager at `/dashboard/chat`. They share the Twitch EventSub connection, command dispatcher, enabled state, role requirement, cooldown, plan admission, and bot audit path with other custom commands. The backend worker runs admitted requests without requiring the dashboard to stay open and rechecks saved command state before changing the desk. Pausing, deleting, renaming, or changing an action also blocks an already queued request that no longer matches.

The initial entries are `!news` (submit links), `!video` (reply with the current source), `!question` (submit a question), and `!join` (enter an open raffle). Fresh installs seed them paused. Enable and edit their names, roles, and cooldowns in Chat → Bot commands. The **Newsroom: capture chat links** action captures URLs from ordinary non-command messages; use it instead of a separate capture setting. Only one capture entry dispatches each message. A subscriber-or-VIP role also permits moderators and the broadcaster. Ignored Newsroom logins remain a submission filter. At most three URLs enter per message. Queues are bounded; a full queue produces a bot audit rejection without blocking overlay delivery.

Existing Newsroom command settings import once into the shared registry, preserving enabled states and permission intent. Conflicting custom-command or alias names remain intact; imported entries receive a `newsroom-` name with a suffix when needed. The persisted import marker prevents intentionally deleted entries from returning after restart. Retired command settings are no longer accepted by the Newsroom API. Raffle opening, drawing, and question approval remain desk controls; they do not define chat commands.

URLs must use HTTP(S), cannot contain embedded credentials, and are normalized for duplicates. YouTube short/share URLs normalize to a watch URL; tracking and single-video playlist context are removed. Duplicate protection covers pending, approved, and shelved links unless explicitly disabled.

## Shows, search, and review coordination

**Shows and recordings** provides named editorial show boundaries. The workspace owner can start and end a show, save a Twitch VOD or YouTube recording, and set the recording offset in seconds (0–604800). One show can be active at a time. Starting it retains the approved rundown in its current segment/order layout and links the current operational stream-session ID when one exists. These editorial boundaries do not start or stop Twitch, OBS, or the operational session logger.

Played notes acquire the active show ID and elapsed offset in the same retained-document write as playback. Existing notes are not assigned to inferred shows. Removing queue cards preserves the starting rundown and active show-note history; Undo excludes its cancelled note from exports, and replay receives the show active at replay time. Read-only members can inspect shows and export notes; show mutations require the workspace owner.

Show Markdown and plain-text chapter downloads are repeatable. Recording links add the saved recording offset to each story offset. Stories played in the same second share one chapter line. Downloads do not consume Discord receipts or general notes, and chapter publication is a separate step. Viewer deletion removes matching entries from retained rundown snapshots as well as cards and history. Retention removes expired ended shows. Up to 200 show records can be retained.

The moderator queue searches retained links by title, audience title, original extracted title, URL, publisher, topic, and submitter. Status and exact media-type filters are supported. Search is case-insensitive literal text, including `%` and `_`. **Load more links** retrieves fifty-item pages; timestamp-plus-ID cursors preserve sub-second precision and remain stable when newer links arrive. Matching/loaded counts are separate from the workspace pending count. Loaded rows survive routine desk polling; **Refresh search** restarts the search to include newly matching rows. Changing filters immediately makes the old result rows unavailable for actions.

Pending cards offer optional ten-minute review claims with **Start review**, **Extend review**, and **Release review**. Claims do not renew automatically. Approval or rejection clears the claim; expiry permits another reviewer. Active claims block another actor's single-card editorial writes, including older clients without editorial revision fields. Only the workspace owner can explicitly release another actor's claim, using the current token. Renewal changes the token so an old tab cannot clear a newer claim. Bulk maintenance remains separate from these coordination controls.

Editorial revisions track human card changes independently of provider, metadata, and lease changes. The desk sends the revision captured when a draft or action was read. Conflicts retain the browser-session draft, including across reload, and provide explicit reconciliation or discard controls. Reconciliation keeps only the fields the author actually changed and preserves concurrent changes to untouched fields. Undo rejection uses the committed rejection's revision and refuses to reverse newer work. Note autosaves share the serialized write queue without disabling an action click triggered by blur; Played waits for the saved revision.

## Providers

Source metadata is fetched by a bounded background worker. Automatic AI analysis is opt-in and starts only after approval; pending submissions never invoke Claude. A manual **Enrich / retry** requests analysis explicitly for an approved or played item. Jobs retain queued, running, complete, or failed state in the desk document. Claims use a two-minute lease and completion token; PostgreSQL row locks fence competing API processes. The worker recovers queued and expired claims every fifteen seconds and after restart. File mode requires a single writer. Failures require explicit retry; reapproval does not repeatedly charge for an existing summary. Missing keys, blocked pages, unavailable transcripts, refused AI output, and failed providers remain visible. The app does not invent summaries or treat an unknown copyright signal as safe to rebroadcast.

Provider keys can be saved in Newsroom settings when the vault keyring and persistent storage are configured. They are write-only, sealed with the workspace and purpose as AES-GCM associated data, and re-encrypted under the active key on a successful read. Keep old vault key versions for retained backups. Empty fields preserve saved keys; the provider API accepts an explicitly empty value to remove a stored override. Hosted workspaces use their own encrypted provider keys and do not inherit global environment keys.

Self-host installations may set these environment variables instead:

- `ANTHROPIC_API_KEY`: Claude summaries, source classification, topic tags, copyright risk, and visual-content warning estimates.
- `NEWSROOM_AI_MODEL`: model identifier, default `claude-haiku-4-5-20251001`.
- `YOUTUBE_API_KEY`: YouTube metadata, duration, and playlist expansion, capped at 200 videos per import. Without it, single-video metadata uses oEmbed and available watch-page data. Captions are best effort.
- `BRAVE_SEARCH_API_KEY`: related source coverage.
- `NEWSROOM_DISCORD_WEBHOOK`: an HTTPS Discord incoming webhook for confirmed show-notes delivery.

Article extraction reads page text and metadata without executing scripts. X/Twitter and TikTok can use their oEmbed responses. The AI receives extracted text; a video description is labeled as incomplete when a transcript is unavailable. Its output is schema-checked before retention. Copyright and content-warning signals require human review.

Outbound fetches reject private, loopback, link-local, shared-address, and reserved destinations; DNS results are validated and pinned at connection time. Redirects are checked again, HTTPS downgrades are refused, and provider credentials cannot follow a cross-host redirect. Responses, deadlines, imports, and worker queues are bounded. Managed workspaces reserve calls through their existing hourly plan limit.

Provider interfaces follow the [Claude Messages API](https://platform.claude.com/docs/en/api/messages/create), [YouTube videos API](https://developers.google.com/youtube/v3/docs/videos/list), and [Brave Search API](https://api-dashboard.search.brave.com/api-reference/web/search/get). A fixture pass is separate from real provider acceptance.

## Storage and migration

Managed installations must apply `0004_newsroom.sql` with administrator credentials before starting the updated API. The runtime requires this table with forced RLS, explicit tenant predicates, and a non-owner role. The retained document uses row locks plus revisions to serialize changes across processes. Stale dashboard commands return 409 and refresh the desk rather than overwriting concurrent edits.

The migration also installs a narrow security-definer capability lookup, following the existing credential-lookup role. Add and share tokens are stored as hashes and resolve only their intended routes. Rotation invalidates the previous token; add tokens cannot authorize general operator routes or provider settings.

Self-host mode without PostgreSQL uses `NEWSROOM_FILE`, default `/data/newsroom.json`. Writes use a private temporary file, fsync, and rename. Encrypted provider keys use the `.keys` companion; hashed add/share capabilities use `.access`. A malformed retained file fails closed and degrades the `newsroom` readiness check. An empty configured path is an ephemeral development desk, and cannot retain credentials or add/share capabilities.

The desk has hard limits of 10,000 play-note snapshots, 50,000 delivery receipts, 2,000 links, 50 shelves, 50 segments, 1,000 questions, and 2,000 raffle entrants. Archived played/rejected links can be removed explicitly. PostgreSQL retention prunes expired archive/question records, viewer deletion removes the viewer’s submissions/questions/raffle entries, and tenant deletion cascades all Newsroom rows. Workspace exports include the retained desk document. Provider custody and add/share credential hashes are excluded; encrypted custody belongs in operator backups.

Back up PostgreSQL and the vault keyring together for managed installs. File-mode backups must include the JSON, `.keys`, and `.access` files, plus `BOT_COMMANDS_FILE` (default `/data/bot-commands.json`). The shared file command registry uses private atomic writes and retains command edits and the import marker across restarts. Timers, community data, and durable bot audit storage still require PostgreSQL. To recover a malformed file, stop its writer, preserve the file, and restore a verified backup. Do not clear another installation’s data to repair a preview.

These reliability fixes add fields to the existing retained JSON document and require no additional SQL migration beyond `0004_newsroom.sql`. Legacy played cards acquire deterministic history snapshots on read and retain them on the next successful write. Older binaries do not understand the new history, processing, and receipt fields and can discard them when rewriting the document. Preserve a current database/file checkpoint and stop the writer before an application rollback.

An application rollback can leave the new table and its data intact. Stop the new writer before restarting the prior binary; it will not process Newsroom records. Do not drop the new table as a quick rollback. If restoring an earlier database backup, preserve the current database and keyring first and use a matching application/database checkpoint.

## Browser extension

Load the repository’s `extension/` directory as an unpacked Chromium extension. Generate an add token in Newsroom settings, then enter the API origin and token in the extension options. It requests access only to that API origin. Refresh its destinations to select a shelf or show segment. The toolbar adds the current page; the context menu adds a link. Rotate or revoke the add token when retiring an extension profile. No operator secret belongs in an overlay or share URL.

## Verification

Run `make verify`. For disposable PostgreSQL persistence and the full Go race suite, run `bash scripts/ci-postgres.sh`. Tests cover queue transitions, atomic batches, stale writes, failed file persistence, credential encryption/rotation, tenant isolation and RLS, chat filters, provider fixtures, internal-address refusal, and uncertain-delivery receipts. Browser checks exercise the submit → approve → focus → takeaway → played → notes path, shelves, questions, and the demo overlay.

Live Twitch delivery, provider billing/acceptance, extension installation, and OBS capture require configured external consumers and are separate qualification states. This integration does not deploy or send real chat/Discord messages during local verification.

## Enrichment input evidence

Reaction cards show the latest extraction evidence separately from the input behind retained AI notes. Evidence records source type, bounded body character count, truncation, check time, model, outcome, and retrieval mode. It adds no raw prompts or provider responses. Existing extracted source text remains available in its own disclosure. Historical AI notes without evidence display “Input not recorded”; later metadata refreshes do not invent evidence for them. Failed retries retain the evidence attached to a previous successful summary.

Backend source retrieval is recorded only after an observed fetch. Anthropic receives supplied text and has no independent retrieval tool in this integration. Transcript and description-only input have separate labels. These labels describe input availability and do not establish source accuracy or permission to rebroadcast.

## Story bundles

Open a pending or approved reaction card's Story bundle disclosure to create a bundle. Editors, managers, and owners can curate; moderators can read. Creation is idempotent for a lead card. A bundle retains a title, shared prep notes, and up to twelve unique supporting sources with coverage, primary-document, or clip roles. Supporting links are normalized and saved without fetching them or scheduling enrichment. Related coverage can be added explicitly.

Bundle edits require the saved bundle revision. An unrelated provider update does not invalidate that revision. A conflicting edit retains the local draft in session storage and requires explicitly discarding it and loading the latest saved bundle before editing again. The Story bundles tab pages through fifty records at a time.

Show start captures the starting bundle context. Playing captures a separate bundle snapshot for that note. Later edits and lead-card removal do not rewrite either snapshot. Markdown and chapters use saved play context. Ordinary lead removal detaches the bundle; viewer deletion removes the viewer's bundles, play history, and rundown records. Retention can detach removed leads and prune older detached bundles. File mode still requires one writer.

## Preparation templates and timing

Each shelf now owns independent segment definitions. Older shelves without those fields derive their existing segments from retained assignments; imports and copies get fresh segment IDs. Under Preparation and timing, save template status and private prompts, add segments (including empty placeholders), and set optional whole-minute targets from 1–1440. The total identifies a partial plan when segments remain untimed. Ungrouped links are outside the timing total and have a saved position.

Create working shelf copies prompts, item notes, existing metadata, empty segments, timing, and order into fresh private records without fetching providers. Copy prepared rundown to deck copies populated segments and skips URLs already approved. Individual Send to deck retains its existing move behavior. Neither path schedules a provider refresh. Shared shelf reads and portable exports contain source links and timing, excluding template status and prompts. Quick-add capabilities list shelf names/IDs without exposing prompts.

## Owner usage controls

Newsroom settings shows UTC monthly counts for AI, related-coverage searches, and metadata attempts. Estimates are $0.003 per admitted AI request and $0.005 per search. Preferred-source coverage reserves two searches before either call. These are application estimates rather than provider prices or invoice limits. Counts begin with this feature; earlier unclassified HTTP usage is not guessed into AI/search totals. Up to one year of monthly aggregates is retained.

The owner may pause AI and search independently, set an estimated monthly allowance (blank is unlimited; zero blocks paid work), and choose a warning percentage. Admission and count updates lock the same retained document as settings. Failed or refused admitted calls remain counted. Provider redirects are refused. An unavailable admission check blocks paid dispatch. Metadata, approval, and play remain available. Allowance-blocked enrichment is visibly skipped and does not automatically retry; use Enrich / retry after changing settings. A request admitted before a pause may finish. Members cannot read the owner's live usage policy/counts through general document responses.

## Activity and archived shows

Moderator activity retains committed editorial changes and review actions with a trusted actor ID (or System or unattributed), source title/URL, status transition, and changed field names. It excludes note/warning text, source bodies, and claim tokens. Conflicts and provider-only changes add no success record. Activity is bounded to 20,000 records and pages in groups of fifty.

Shows and recordings searches retained show titles and covered-story title, URL, summary, and takeaway snapshots. It supports active/ended filters and inclusive UTC start-date ranges. Literal percent and underscore characters have no wildcard behavior. Fifty-row cursors bind the filters and retain timestamp precision. The active-show control remains independent of the archive filter.

## Per-show exports and unused stories

For an ended show, Preview show Discord export displays the exact messages. Post this exact show to Discord saves immutable chunks and records each acknowledgment under a fenced delivery lease. A response processes at most five chunks; resume the saved export if more remain. Known HTTP failures can resume unsent chunks. Uncertain delivery or a crash between post and receipt blocks automatic resend and requires inspection. Mentions are disabled and redirects refused. A repeated completed version sends nothing; changed content requires another preview and creates another version. General notes receipts remain separate. Saved exports are bounded to 1,000 versions.

Prepare next show shelf copies stories with no uncancelled play in that historical show into one independent private shelf. It preserves the saved segment structure, timing and source positions, plus available current prep and metadata. Deleted unused cards fall back to the starting snapshot. A played card remains excluded after cleanup by its saved identity/URL. The first copy's receipt makes retries idempotent; a removed destination is reported rather than recreated. No unused stories means no shelf.

## Public episode pages

An ended show's Public episode page controls require the owner. Summaries and personal takeaways start excluded. Preview public episode constructs an allowlisted projection of public title, date, covered-story source links, and recording timestamps; Publish this preview verifies the fingerprint and saves that projection. Private preparation, bundle prep, saved rundown, moderation fields, source bodies and internal IDs are excluded. Public reads serialize only the public schema and use no shared cache.

The new public URL is displayed once; copy it before leaving the page. The current browser keeps it in session storage for convenience. Republish preserves the token and replaces only the explicit snapshot. Revoke removes the snapshot and capability; publishing later creates a different URL. Already distributed copies of previously public text cannot be recalled. Viewer deletion removes derived saved exports and revokes affected public episodes in the same lifecycle transaction.

Apply additive `0005_newsroom_episodes.sql` after `0004_newsroom.sql`, before the matching runtime. It extends only the narrow capability lookup to episode tokens. Runtime startup checks require this migration. The publication projection and capability index are updated in the same PostgreSQL transaction. File mode retains the hash and projection in its atomic document; one writer is required. Rollback needs a matching document checkpoint, since older binaries discard unknown fields on rewrite. Retain additive SQL schema during rollback.

## Automatic Twitch boundaries and corrections

Automation is off by default. Connect Twitch EventSub, then enable it from Shows and recordings. Both stream.online and stream.offline subscriptions must register for the current authenticated WebSocket session before enabling. Check Twitch show subscriptions pages through Twitch's current list. Repair replaces only the lifecycle pair matching this session and broadcaster; chat subscriptions and other sessions are untouched. This follows [Twitch's WebSocket session/subscription ownership](https://dev.twitch.tv/docs/eventsub/handling-websocket-events/) and [subscription management](https://dev.twitch.tv/docs/eventsub/manage-subscriptions/). A fresh connection recreates the pair when automation is enabled; provider-directed grace transfers retain the migrated pair. Subscription checks/repairs refresh the token within the same broadcaster identity; disconnected events are not replayed by Twitch.

Notifications are accepted only from that authenticated session and broadcaster, with fresh message timestamps. Retained event IDs, stream IDs, and a timestamp watermark prevent duplicates, stale boundaries, and reopening a manually ended stream. Manual active shows remain intact. A newer online event repairs a missed offline boundary for the previous automatic show and reassigns recorded plays by time. Overlapping saved history is reported for manual correction. Offline uses the notification timestamp as an approximation. Enabling while already live does not synthesize a start. Disabling leaves the active show and subscriptions in place.

Correct show boundaries uses UTC start/end inputs, the loaded show revision, and a draft retained through failed saves/reload. It rejects overlap or exclusion of a recorded play and recalculates note offsets. Clearing an end reopens under manual control. Untouched forms refresh as the show changes; edited forms require Load latest boundaries to replace their draft. Saved public projections and Discord versions remain unchanged until explicitly republished/exported. No real subscription registration or provider delivery is part of local fixtures.

Queued general-registry requests carry [saved command identity and an opaque snapshot fingerprint](registry-command-snapshots.md). Newsroom compares them against a fresh registry read before execution, rejecting edits/replacements and missing metadata without a separate parser. This point-in-time check is not an atomic source/admission fence or provider authorization.

The explicit [source-bound reply collector](registry-reply-collection.md) routes `newsroom_current` through the same atomic admission batch as general custom replies when selected by a future runtime integration. No startup path selects it yet. Unbound manual announcements fail in that selection until their separate retained-source contract is implemented; defaults preserve the current sender behavior. Editorial mutations and display/history publication remain separate from ledger admission.

Retained chat replies use [transactional saved-command admission](registry-admission-binding.md) through the explicit collector. New `newsroom_current` reply snapshots stay locked until ledger/job commit. The earlier fresh registry read still guards Newsroom execution, while editorial mutations and publication retain their separate persistence boundaries. This does not activate a sender or cancel already admitted actions after later command edits.

Explicit manual current/raffle announcements now have a [separate retained source/admission foundation](manual-outbound-sources.md), with configure permission, immutable caller-key identity, database-clock deadlines and atomic action/job storage. The [request/UI intent contract](manual-announcement-intents.md) supplies content previews, receipt-only retries and truthful queued acknowledgements when the retained admission boundary is explicitly selected. Application startup defaults to direct sending; [retained chat sending](retained-chat-sender.md) explicitly selects the queued contract. All managed writers, including EventSub-disabled manual endpoints, require the existing database controller.

Manual announcements use the [preview/intent/receipt flow](manual-announcement-intents.md). The dashboard keeps unresolved requests across tab reloads, checks retained receipts without re-admitting missing work, and labels queued admission separately from delivery. Clearing an unresolved request requires an explicit acknowledgement after checking Twitch. New retained requests recheck approved current/raffle content under locks through admission. Default sender startup is unchanged.
