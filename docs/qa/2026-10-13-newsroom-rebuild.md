# Newsroom rebuild verification

Source baseline: `PatrickFanella/newsroom` commit `b435e064a48f23462e48d441e39f345d3505b6c3`. Verification used this isolated worktree and temporary fixture data; production state and provider credentials were not used.

## Local evidence

- `make verify` passed: frontend TypeScript/lint, backend vet/tests, frontend tests/build, backend binaries, Compose validation, and retained Open Pilot templates. Frontend suite: 121 tests across five files.
- `bash scripts/ci-postgres.sh` passed with the new migration and persistence fixtures, then the full uncached Go race suite. Its disposable PostgreSQL container was removed by the script.
- New regressions cover atomic batches, stale revisions, failed file writes, persisted played notes, encrypted credential rotation, forced RLS, forged tenant writes, add/share rotation, private-read restrictions, provider extraction and failure handling, concurrent coverage retention, viewer deletion, and credential-free workspace exports.
- `node --check` passed for both extension scripts. The extension was not installed in a browser profile.
- Browser fixture completed submit → approve → put on desk → edit takeaway → mark played → show notes. Notes appeared in the rendered report and survived the isolated API restart.
- Browser fixture saved a link to a future-show shelf and sent it back to the deck. It submitted and approved an audience question, displayed it on the live local Newsroom overlay, and verified the question text there.
- Desktop and 390-pixel layouts had no horizontal overflow. Visual inspection found and corrected the active-tab contrast and collapsed the optional queue briefing so triage and the reaction card stay prominent.

The preview runs at API `127.0.0.1:41881` and web `127.0.0.1:41882`, with retained fixture data under `/tmp/overlaid-newsroom-qa/`. Twitch/EventSub and provider credentials are disabled there. Demo previews use separate in-memory sample data. This is a development instance, not a production deployment.

## Remaining qualification

Live Twitch ingestion/replies and Discord delivery have not been exercised with real accounts. Claude, YouTube and Brave interfaces passed fixtures; billing, quotas and real provider content acceptance remain unverified. Real extension installation and OBS browser-source capture remain unverified. No hosted CI run, commit, push, PR, merge or deployment is claimed by this local receipt.
