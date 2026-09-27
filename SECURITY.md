# Security

## Reporting a vulnerability

Please open a [private security advisory](https://github.com/AndrewSkea/team-memory/security/advisories/new) rather than a public issue. Only the latest release is maintained.

## What leaves your machine

| Data | Where it goes | Notes |
|---|---|---|
| Memories you save, and knowledge the capture step extracts | Your memory repo on GitHub | Only after secret redaction and your capture mode's rules (see below) |
| Session transcripts (text only, no tool output) | Anthropic, via your own `claude` CLI login (or `ANTHROPIC_API_KEY`) | Only for extraction; transcripts are never written to the memory repo |
| Search queries, prompts | Nowhere | Recall runs locally against the mirrored repo |
| Telemetry | Nowhere | There is none |

## Safeguards

- **Secret redaction.** Before anything is written to the memory repo, the pending queue, or sent for classification, text is scanned for credentials: private keys, cloud and SaaS tokens (AWS, GitHub, GitLab, Slack, Stripe, Google, Anthropic, OpenAI, npm), JWTs, passwords in URLs and `password=`/`api_key:`-style assignments. Matches become `[REDACTED]`. In `ai` mode, an item that needed redaction is held for review instead of being published.
- **Sensitivity classification.** The extractor labels each item shareable, personal or sensitive; personal and sensitive items are never published automatically in any mode.
- **Org boundary.** Sessions in repositories not owned by the memory repo's owner (or an org listed in `policy.yml`) are held for review by default (`outside_org: review`).
- **Review stays local.** Items waiting for review live only in your data directory until you approve them.
- **Environment files.** Recall reads environment variable *names* from `.env*` files to match systems; values are never read.
- **No prompt recursion.** The capture step's own `claude -p` call runs with hooks and MCP servers disabled and without saving a session.

## Local attack surface

| Component | Protection |
|---|---|
| Web UI / API | Binds `127.0.0.1` only. Requests with a non-loopback `Host` are refused (DNS rebinding). Cross-origin browser requests are refused, and write endpoints require `Content-Type: application/json` (CSRF). Strict CSP (`connect-src 'self'`), `X-Frame-Options: DENY`. |
| GitHub token | Stored in the config file (mode `0600`) or not stored at all when using `gh auth login` (`token_source: gh`). Never returned by the API or sent to the browser. |
| Config file | `~/.config/team-memory/config.json` (`%APPDATA%\team-memory` on Windows), mode `0600`. |
| Data directory | `~/.local/share/team-memory` (`%LOCALAPPDATA%\team-memory`): mirror, review queue, capture queue, logs. Mode `0700`. |
| Binary | Installed under your home directory (`~/bin`). Anything that can write there can replace it — keep default permissions. |

## Known limitations

- The local API has no authentication: any process running as you can call it — the same processes that can already read your config file.
- The web UI is plain HTTP on loopback. Don't forward port 7438 to other machines.
- Memories are as confidential as the memory repo. Use a private repo and GitHub's access controls; everyone with read access sees all team memories (personal-scope memories are hidden from others' recall but are still in the repo).
- Redaction is pattern-based. It catches common credential formats, not every secret imaginable — review mode exists for teams that need a human check.
- Prompt injection: text in a memory could try to instruct an assistant that recalls it. Recalled memories are clearly delimited as untrusted team notes in a `<team-memory>` block, and only people with write access to the memory repo can add them.
