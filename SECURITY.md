# Security Policy

## Supported scope

This repository contains the Over-Laid application: its Go API, React dashboard/overlays, PostgreSQL tenant stores, Twitch/OBS integration and optional Newsroom providers. Security-relevant boundaries include workspace authorization, forced row-level security, credential custody, public/add/share capabilities, untrusted URL retrieval and provider-effect execution.

The project is under active development. A passing test suite or implemented contract is not a supported-release guarantee; see [current capabilities and limits](docs/reference/project-status.md) for activation and acceptance boundaries.

## Reporting a vulnerability

Do not publish sensitive vulnerabilities in public issues.

Report privately to the repository owner or organization maintainer through the preferred private channel for the project. If no project-specific channel exists, contact the maintainer account for the Gitea organization.

Include:

- affected repository and commit
- impact summary
- reproduction steps
- affected configuration or deployment path
- suggested mitigation, if known

## Secret handling

Never commit:

- `.env` or `.env.*` files, except `.env.example`
- API tokens or OAuth secrets
- SSH keys or deploy keys
- database dumps containing real data
- private certificates or signing keys
- webhook secrets

Use placeholders in docs and examples. Report only secret key names and presence, never secret values.

## Dependency and supply-chain hygiene

Contributors should:

- commit lockfiles for application projects when the stack expects them
- pin container base images deliberately
- document update procedures
- run dependency and container scans where appropriate
- keep generated caches out of git

## Open Pilot safety

Open Pilot tasks should include a deterministic `Test Command` and clear scope limits. Do not queue tasks that require secret access, production credentials, destructive data changes, or ambiguous deployment actions.

For risky work, require human review before adding `agent:queued`.
