# Security Policy

## Supported versions

Only the latest release is actively maintained.

## Reporting a vulnerability

Please **do not** open a public GitHub issue for security vulnerabilities.

Email **andrewskea.as@gmail.com** with:
- A description of the vulnerability
- Steps to reproduce
- Potential impact

You will receive a response within 72 hours. If confirmed, a fix will be released as soon as possible and you will be credited unless you prefer otherwise.

## Threat model

team-memory is a local-first tool. Understanding its trust boundaries:

| Component | Trust boundary |
|-----------|---------------|
| Binary (`team-memory-mcp`) | Binds `127.0.0.1` only — never exposed to the network |
| Config file (`~/.config/team-memory/config.json`) | Readable by the current OS user only (`chmod 0600`) |
| Web UI credentials | Stored in browser `localStorage` — scoped to `127.0.0.1` origin |
| GitHub PAT | Stored in config file and browser localStorage — never sent anywhere except `api.github.com` |
| Anthropic API key | Optional; stored in browser localStorage only — never written to disk by the binary |

## Known limitations

- The web UI does not use HTTPS (loopback only). Do not expose port 7438 to other machines.
- PATs are stored in plaintext in the config file. OS-level disk encryption (FileVault, BitLocker) is the mitigation.
- The MCP server at `127.0.0.1:7438` has no authentication — any process on the local machine can call it.
