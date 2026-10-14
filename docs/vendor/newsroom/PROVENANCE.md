# Newsroom source provenance

The Newsroom integration was rebuilt from [PatrickFanella/newsroom](https://github.com/PatrickFanella/newsroom), a fork of [mapletakes/newsroom](https://github.com/mapletakes/newsroom), at commit `b435e064a48f23462e48d441e39f345d3505b6c3` on October 13, 2026.

The source is MIT-licensed, copyright 2026 The Broadside. Its complete notice is retained in [LICENSE](LICENSE). Over-Laid remains GPL-3.0-only. The Go and Vite implementation adapts the source workflow, URL normalization, provider requests, copyright host signals, and capability boundaries to Over-Laid’s accounts and storage.

The source checkout used for comparison is disposable and is not vendored. Newsroom does not introduce Next.js, Supabase, Vercel jobs, a second Twitch OAuth flow, or a second account system. See [the integration guide](../../runbooks/newsroom.md) for behavior, provider setup, verification, and recovery.

The follow-up reliability comparison also inspected the owner's clean Kvant checkout at the same baseline, [PR 1](https://github.com/PatrickFanella/newsroom/pull/1) at `d48de90502df38728bafe6a1856527fa9a4ca509`, and [PR 2](https://github.com/PatrickFanella/newsroom/pull/2) at `6f39e473710585ed69b222a342a9ab67852e9a5a`. Those unmerged changes informed the Go implementation's durable claims and play-history/delivery fixes; neither source PR was merged by this work. The [comparison receipt](../../qa/2026-10-13-newsroom-fork-fixes.md) records applicability and verification.

The newest-PR follow-up inspected [PR 3](https://github.com/PatrickFanella/newsroom/pull/3) at `fc9ba809bbb0b103c5de7d6fbc6d0765e55ab66f`, [PR 4](https://github.com/PatrickFanella/newsroom/pull/4) at `68960718714c57481ec931f60f2b58ec664cea47`, and [PR 5](https://github.com/PatrickFanella/newsroom/pull/5) at `1dda4fdc8145d173c22ed1535e96ec19a9a5ade4`. Their behavior was adapted to Over-Laid's Go tenant document and Vite desk, without copying Supabase migrations or merging the source PRs. See the [newest PR comparison](../../qa/2026-10-13-newsroom-newest-prs.md).

The same follow-up includes [PR 6](https://github.com/PatrickFanella/newsroom/pull/6) at `0e0056dd00de919f67f7fa8852978ca47e890b2b` and [PR 7](https://github.com/PatrickFanella/newsroom/pull/7) at `d083400511c3bf8feb10e8e89cd917858b2bf990`. PR 7 is stacked on PR 3; its bundle layer was reviewed against that parent. Input evidence and story bundles use the same retained Go document, permission boundaries, and immutable show history. These exact heads define the reviewed cutoff for this pass.
