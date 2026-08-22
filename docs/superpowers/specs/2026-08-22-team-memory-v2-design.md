# team-memory v2 — Design

Date: 2026-08-22
Status: Approved (sections 1–4 by user; section 5 delegated)
Branch: `v2`

## 1. Problem

team-memory captures knowledge from AI coding sessions into a shared GitHub
repository. Three things are wrong with v1.

**No human gate.** `mcp/hook/handler.go` sends a truncated transcript to an
LLM and commits the result unreviewed. `UNSURE.md` is the only holding pen,
and it is a dumping ground rather than a queue. A user who talks to Claude to
think out loud has no way to stop that reaching a shared team repo.

**Fragile storage.** `INDEX.md` is authoritative state mutated by string
concatenation. Entries are appended into shared Markdown files, so two writers
collide and the code carries a 409-retry loop to cope.

**Fragile distribution.** The frontend is copied into the Go module by a shell
script before every build. There is no bundler, no types, and no build step in
the release workflow.

## 2. Goals

1. Installation is one command per harness and requires no other tooling.
2. The user can see and understand everything stored about them.
3. Data stays on the user's machine or in their GitHub. Nothing else.
4. One process. Not an MCP server plus an API plus a daemon.
5. The same project serves a home user and a company deployment.

### Non-goals for v1

- Phone / hosted-MCP access. Deliberately deferred.
- Any telemetry, licence check, or update ping.
- Support for Aider (no MCP) or Roo Code (archived 2026-05-15).

## 3. Runtime architecture

### 3.1 One daemon, no subprocesses

A single Go binary. A single long-lived daemon process. MCP is served over
**Streamable HTTP on `127.0.0.1`**, so no harness ever spawns a per-session
MCP subprocess.

Measured on Go 1.26.2 / windows-amd64, `CGO_ENABLED=0`:

| Metric | Value |
|---|---|
| Stripped binary | 5.90 MB |
| With 1.9 MB embedded SPA | 7.93 MB |
| Process entry to `net.Listen` | 0.55–1.11 ms |
| Idle RSS | 12.0 MB |
| Idle CPU | 0.13% |
| Process spawn (Windows) | 28.9 ms avg |

Daemon start is ~1 ms, so lazy start is free. Process spawn is ~29 ms, so a
per-session stdio shim is not free — which is why it is the fallback, not the
design.

The stdio path survives as `team-memory mcp`: a dumb `io.Copy` proxy onto the
same loopback endpoint, with no protocol awareness, for Zed and any future
stdio-only harness.

### 3.2 Two listeners, one handler

```go
h := buildHandler(srv)
go http.Serve(udsLn, h)        // UDS / named pipe — OS enforces access
go http.Serve(tcpLn, guard(h)) // 127.0.0.1 — Host + Origin + bearer
```

Routes: `/mcp` (MCP), `/api/*` (REST), `/` (embedded SPA).

On Windows, AF_UNIX sockets are NTFS reparse points whose permissions are ACLs;
`os.Chmod(0600)` is a silent no-op there. Windows therefore uses named pipes via
`github.com/Microsoft/go-winio`, which allows an explicit SDDL and
`ImpersonateNamedPipeClient` to verify the caller's SID.

### 3.3 Lifecycle

Started lazily by the harness's own session-start hook, calling
`team-memory daemon --ensure`: idempotent, flock-guarded against a thundering
herd of terminals, health-check-then-spawn-detached, exits in ~1 ms when the
daemon is already up. Runs synchronously; the whole cost is ~30 ms and
synchronous execution removes the race against MCP connect.

Idle-out after 30 minutes with no MCP session and no browser client.
`team-memory ui` starts the daemon if needed and opens a browser.

OS-level autostart (`launchd`, `systemd --user`, HKCU `Run`) exists as
`team-memory autostart enable`. Opt-in. **Not run by the installer.** Honest
caveats to document: macOS Ventura+ surfaces a "Background item added"
notification; macOS 26 prompts for background tasks that outlive their starter;
Linux headless requires `loginctl enable-linger`, which needs sudo — so the
no-admin promise holds only for the hook default.

### 3.4 Commands

| Command | Behaviour |
|---|---|
| `daemon --ensure` | Idempotent, ~1 ms when healthy. Called by session-start hooks. |
| `daemon --serve` | The daemon. Binds both listeners. Idles out at 30 min. |
| `mcp` | stdio→daemon `io.Copy` proxy. Fallback only. |
| `ui` | `--ensure`, then open browser. |
| `mcp-headers` | Prints the Authorization header JSON. Header helper + launcher. |
| `hook <event>` | Converged hook handler. Reads JSON on stdin. |
| `install <harness>` | Per-harness wiring. See §7. |
| `install doctor <harness>` | Reports {MCP reachable, autostart wired, capture wired}. |
| `autostart enable\|disable\|status` | Opt-in OS integration. |
| `token rotate` | Regenerates the bearer token, rewrites harness configs. |
| `--mode=server --listen :8080` | Company deployment. |

**Removed from v1:** `tryListen(":80")` and the `http://team-mem/` hosts-file
entry. Binding :80 needs root, and a magic hostname makes the Host-header
allowlist strictly weaker — a security regression bought with a prettier URL.

## 4. Security

### 4.1 Threat model

The daemon holds a GitHub credential. A loopback TCP port is reachable by any
local process of any user, and by any web page the user visits, via DNS
rebinding — `evil.com` resolves to a real IP, then re-resolves to `127.0.0.1`,
and the browser treats the daemon as same-origin with attacker script. The MCP
specification names DNS rebinding explicitly, and it has been demonstrated
against real internal MCP servers.

### 4.2 Controls

```go
func guard(next http.Handler) http.Handler {
  return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    // 1. Host allowlist — the load-bearing control. A rebound request
    //    necessarily carries Host: evil.com.
    if !allowedHost(r.Host) { http.Error(w, "bad host", 421); return }

    // 2. Origin: absent is allowed; present-and-not-allowlisted is 403.
    //    Verified: no MCP client sends Origin (MCP TS SDK, Codex, Zed source).
    //    MCP spec requires 403 only "if the Origin header is present and invalid".
    if o := r.Header.Get("Origin"); o != "" && !allowedOrigin(o) {
      http.Error(w, "bad origin", 403); return }

    // 3. Bearer token, constant-time.
    if subtle.ConstantTimeCompare([]byte(bearer(r)), token) != 1 {
      http.Error(w, "unauthorized", 401); return }

    // 4. No credentialed CORS, ever.
    next.ServeHTTP(w, r)
  })
}
```

Bind `127.0.0.1` explicitly, never `0.0.0.0`.

**Requiring an Origin header would break every verified client.** This is a
correction to an earlier assumption and must not be re-introduced.

### 4.3 Credentials

Bearer token: 32 bytes from `crypto/rand`, stored at
`$TEAM_MEMORY_HOME/token` mode 0600 (Windows: `%LOCALAPPDATA%`, ACL to the
user SID).

GitHub PAT: OS keychain — `security` on macOS, `secret-tool` on Linux, DPAPI
via `golang.org/x/sys/windows`. All cgo-free. Plain 0600 file as fallback.
**The API never echoes the PAT back**, even to an authenticated caller.
Credential endpoints are write-only.

### 4.4 Egress allowlist

In `--mode=local`, the shared `http.Client`'s `DialContext` refuses to dial any
host other than `api.github.com` / `github.com` (plus a configured GHES host).
Any accidental call to a telemetry or licence endpoint becomes a hard error.

A test asserts zero out-of-allowlist connections across a full session
lifecycle. This converts "we do not phone home" from a README claim into
something CI enforces.

## 5. Storage

### 5.1 Layout

```
memories/team/YYYY/MM/<hash>-<slug>.md
memories/people/<user>/YYYY/MM/<hash>-<slug>.md
INDEX.md            # derived, regenerated, never hand-edited
policy.yml          # org policy floor
```

One file per memory, content-addressed. Two writers never touch the same path,
so concurrent additions from different machines merge with no conflict
handling. This deletes the 409-retry loop in `frontend/src/services/github.js`
and the read-modify-write append in `CommitFile` outright.

YAML frontmatter: `id`, `created`, `author`, `scope`, `tags`, `source_session`,
`confidence`. Body is Markdown.

**`INDEX.md` becomes a derived artifact**, regenerated deterministically from
the files on disk. It is never authoritative and never a merge-conflict source.

### 5.2 Scope

`scope: team` is the default. `scope: personal` is the exception, for things
about an individual's own setup, preferences, or working habits — editor
config, local paths, "I prefer X". Team scope covers the codebase, decisions,
architecture, and conventions.

Personal memories live in the **same shared repository** under
`memories/people/<user>/`. A personal memory is *filed* differently, not
*concealed*. The UI must state this plainly so nobody reads `personal` as
`private`.

Scope is LLM-classified, so it must be correctable: the review queue shows
scope on every card with a one-key toggle, and edit-then-approve can change it.
Ambiguous cases default to `team`.

Search defaults to team plus the current user's own personal folder. Other
users' personal folders are reachable but excluded from default results — noise
reduction, not access control.

### 5.3 Local clone

`go-git` v5 at `$TEAM_MEMORY_HOME/clone/`. Pure Go, no CGO, no external git
binary. Reads are filesystem reads: instant, offline, no rate limit. Sync is
fetch plus **fast-forward-only** merge. Never force. Never `gc`.

Push happens on approve.

### 5.4 Search index

`modernc.org/sqlite` with FTS5. Pure Go. Treated as a **fully rebuildable
derived index** — delete the file and it reconstructs from the Markdown. It is
never a source of truth, so corruption is never data loss.

### 5.5 The privacy boundary is structural

```
$TEAM_MEMORY_HOME/
  clone/        <- the git worktree. Everything here is shareable.
  staging/      <- SIBLING, not a child. Unreviewed content.
    pending/
    bin/
```

`staging/` being a sibling of `clone/` means unreviewed content is unreachable
by any `worktree.Add` pattern, including a glob bug. This is backed by:

- a path-prefix assertion before every commit, and
- a permanent regression test.

The guarantee that nothing unapproved reaches the shared repo is a property of
the layout, not a promise that the code is correct.

### 5.6 Deletion is described honestly

Once a memory is committed and pushed, the blob is in git history, in every
teammate's clone, and on GitHub's servers. "Delete permanently" would be a lie.
Two differently-worded actions:

- **Remove from memory** — a commit deleting the file. History retains it.
- **Purge from history** — rewrite and force-push. Breaks every existing clone.
  Requires explicit confirmation.

Both are accompanied by: *if a secret was committed, rotate the credential.
Rewriting history is not a substitute.*

The Bin is a real state, not a delete: rejected memories move to `staging/bin/`
with their rejection reason, are restorable, and are never committed.

## 6. Capture and review

### 6.1 Memory level

Per-user, three values, set at install, changeable at any time.

| Level | Behaviour |
|---|---|
| `auto` | Extracted memories commit directly. No queue. |
| `review` | **Default.** Everything lands in `staging/pending/`. |
| `off` | No capture. The MCP `remember` tool still works. |

The default is `review`, not `auto`, because thinking out loud is the common
case and the safe mode should be the one you get without choosing.

**`policy.yml` sets a floor, never a ceiling.** An organisation may require
`review`. There is deliberately **no `max_memory_level` key** — a policy
compelling employees to log their AI conversations is surveillance, and this
project will not ship the mechanism for it.

### 6.2 Capture pipeline

```
hook fires -> read transcript -> gitleaks scan -> LLM extraction
           -> candidates written to staging/pending/
```

The secret scan runs **before** extraction, so a pasted key never reaches the
model or the disk.

**Gated paths:** session-end and pre-compact hooks.
**Ungated (commit directly):** the MCP `remember` tool, manual UI entry, and
`/v1/quick-add`. These are deliberate acts; asking the user to approve
something they just typed is insulting.

### 6.3 Review queue

Grouped **by session, not by item** — the natural unit of trust is "that
refactoring session on Tuesday". Each group header carries `Approve all (n)`.

Four actions: approve as-is, edit-then-approve, reject, bulk approve/reject.
Keyboard: `j`/`k` navigate, `a` approve, `r` reject, `e` edit, `u` undo,
`t`/`p` toggle scope, `?` help.

Actions are optimistic with a 6-second undo toast and **no confirmation
dialogs** — justified because rejection goes to the Bin and is non-destructive.

The queue is bounded at ~50 with a sweep action. An unbounded queue becomes a
second `UNSURE.md`: a dumping ground nobody reads, which is the failure mode
being fixed.

### 6.4 Notification

A single line printed by the session-end hook
(`team-memory: 3 memories pending — team-memory ui`) plus a nav badge in the
web UI.

### 6.5 LLM runner — open cost risk

v1 shells out to the `claude` CLI. Open issues anthropics/claude-code #43333,
#37686 and #44669 report `claude -p` billing as pay-per-token API usage even
for Max subscribers on OAuth with no `ANTHROPIC_API_KEY` set; one report cites
$1,800 in two days. team-memory calls the LLM on every session end.

**Unverified whether this is fixed. Must be tested before the extraction path
is written.** Regardless of the outcome, ship:

- a cost notice at install time,
- a configurable monthly call cap with a visible counter,
- the extraction runner behind an interface, so a local model is a config swap.

## 7. Harness integration

### 7.1 There is a converging standard, and it is Claude Code's

Not by committee. Four vendors independently cloned the shape: Codex's source
comments it as "Claude-style lifecycle hooks"; Cursor's changelog states
"Claude Code-format hook responses accepted"; Copilot accepts both
`sessionStart` and `SessionStart` spellings as a porting shim; Gemini CLI
reproduces the exit-code semantics and `hookSpecificOutput.additionalContext`.

The converged contract:

```
argv   = the configured command
stdin  = one JSON object: { session_id, transcript_path, cwd, hook_event_name, ... }
stdout = JSON, or plain text as context
exit   = 0 success | 2 block (stderr is the reason) | other = non-fatal warning
```

Implementation: **one hook binary** implementing this contract, plus thin
per-harness adapters normalising camelCase/PascalCase event names,
seconds/milliseconds timeouts, `PreCompact` vs `PreCompress`, and missing or
null `transcript_path`.

### 7.2 MCP sessions do not exist any more

MCP revision `2026-07-28` removed protocol-level sessions, removed
`Mcp-Session-Id`, removed the GET stream, and removed server-initiated JSON-RPC
requests. **Session identity comes from hooks, not from MCP.** No design
element may depend on MCP-level session state.

### 7.3 Support matrix

| Harness | HTTP MCP | Non-plaintext token | Session start | Capture |
|---|---|---|---|---|
| Claude Code | yes | `headersHelper` | `SessionStart` | `SessionEnd` + `PreCompact` |
| Codex CLI | yes | 4 mechanisms | `SessionStart` | `SessionEnd`, 3 s cap |
| Cursor | yes | `${env:VAR}` | `sessionStart` | `sessionEnd`, `preCompact` |
| Gemini CLI | yes | `${VAR}` | `SessionStart` | `SessionEnd`, `PreCompress` |
| Copilot CLI | yes | no | `sessionStart` | no `transcript_path` |
| Windsurf | yes | no | none | per-turn transcript hook |
| Zed | yes | no | none | tool-only |
| Cline / opencode | yes | mixed | JS plugin | JS plugin |
| Roo Code | — | — | — | archived 2026-05-15, dropped |
| Aider | no MCP | — | — | dropped |

### 7.4 Autostart ladder

1. Native session-start hook calling `daemon --ensure`.
   *Claude Code, Codex, Cursor, Gemini, Copilot CLI.*
2. Header helper as launcher — `headersHelper` / `http_headers_helper` runs at
   connect time, so it can start the daemon, wait for health, then print the
   auth header. One config field, both jobs. A **backstop** to (1), not a
   replacement: Claude Code caches remote tool discovery for ~2 h.
   *Claude Code, Codex.*
3. Nearest pre-turn hook. *Windsurf `pre_user_prompt`.*
4. JS/TS plugin shim. *Cline `session_start`, opencode `session.created`.*
5. Shell-profile snippet. *Zed, VS Code Copilot.*
6. OS-level autostart. Offered, never required, never silent.

### 7.5 Per-harness gotchas that fail silently

These are the ones that cost debugging time if not encoded in the installer:

- Claude Code `SessionEnd` default timeout is **1.5 s**. Must be overridden or
  capture is killed mid-write.
- Codex `SessionEnd` is capped at **3 s**, and `reason` is hardcoded to
  `"other"` — never branch on it. Capture must detach.
- Codex `transcript_path` is `Option<PathBuf>` and **can be null**. Codex
  rollouts are zstd-compressed in place and moved to `archived_sessions/`, so a
  naive tailer watches a file that vanishes. Watch the directory, not the file.
- Gemini: use `httpUrl`, not `url` — `url` **silently** selects deprecated SSE.
- Cline: `type` must be set explicitly; omitting it **silently** falls back to
  legacy SSE.
- Claude Code strips credential-shaped env vars (`*_TOKEN`, `*_KEY`, `*_SECRET`)
  from project-scope helper environments. The header helper must therefore read
  from the keychain or token file, never from `$TEAM_MEMORY_TOKEN`. Install at
  **user scope** to avoid both this and the folder-trust gate.
- Codex prompts for hook trust on first run. The installer must say so, or the
  hooks silently never fire.
- Claude Code documents that the transcript is written asynchronously and may
  lag the in-memory conversation. Never read the transcript from a `Stop` hook
  expecting the final message; use `last_assistant_message`.
- Never compute the munged-cwd or project-hash transcript directory yourself.
  Always take `transcript_path` from the hook payload.

### 7.6 Installer rules

Every installer is idempotent, backs up each file it touches to
`<file>.team-memory.bak`, and merges into existing JSON rather than
overwriting. Every installer must write the harness's required transport
discriminator.

`install doctor <harness>` re-reads each edited file, checks daemon health, and
reports {MCP reachable, autostart wired, capture wired}. On several harnesses
only two of three will ever be green; the doctor presents that as a stable
state, not a bug.

Degraded cases are documented rather than hidden: Windsurf starts on the first
prompt and captures per turn; Zed gets MCP tools only, with no autostart and no
automatic capture.

## 8. Web UI

Vite + SvelteKit with `adapter-static` (SPA fallback) + Svelte 5 runes +
TypeScript. `@tanstack/svelte-query` for server state. Plain CSS, retaining the
existing custom-property palette (`--accent: #c84e1a`, `--bg: #f0ebe0`). No
Tailwind, no CSS-in-JS, no UI kit.

`adapter-static` emits a plain directory of static assets: exactly what
`go:embed` wants for home mode and exactly what nginx or a CDN wants for
company mode. **Same artefact, both deployments, zero conditional build.**
Assets are gzipped at build time and served with `Content-Encoding: gzip`,
which saves both binary size and per-request CPU.

Svelte over React because the migration from HTML strings plus `querySelector`
to `.svelte` files is near-mechanical, whereas React requires a full JSX and
hooks rewrite. The counter-risk is honest and stated in CONTRIBUTING: React has
the larger drive-by-contributor pool, mitigated with a translation-table
section.

### 8.1 Routes

| Route | Purpose |
|---|---|
| `/` Memory Profile | Landing. AI-written prose summary first, facets below. |
| `/review` | The queue. §6.3. |
| `/browse` | Faceted search plus the folder tree. |
| `/bin` | Restore or remove. §5.6. |
| `/reorganize` | The agent run. §8.3. |
| `/settings` | Memory level, connections, token rotation. |

The Memory Profile leads with synthesised prose, not a list. A list of 300
atomic facts does not produce understanding — a lesson learned at OpenAI's
expense, which since June 2026 labels ChatGPT's raw saved-memories list
"legacy" in favour of an editable synthesised summary. Every claim in the
summary links to the notes backing it. An `Ask: what do you know about ___?`
probe box lets the user choose the question, which is what makes an audit feel
real. A `Stale (n)` facet offers `Still true` / `Bin it`.

### 8.2 Visualisation

Ranked topic bars, a contributions-style activity heatmap, faceted search, and
the folder tree — which doubles as the "review it in its entirety" requirement.
All are plain SVG and CSS with no dependencies.

**No force-directed graph, no tag cloud, no 3D.** A graph is read-only, becomes
unusable past a few hundred nodes, and its edges here would mostly mean "shares
a tag" — which is a facet, not an edge.

### 8.3 Reorganize

Runs the user's own local `claude` or `codex` through the daemon.

Three phases: `Plan -> Working -> Review`, with the plan as a genuine gate.
Transport is **SSE, not WebSocket** — `EventSource` provides auto-reconnect and
`Last-Event-ID` resume for free, and traverses corporate proxies that block
`wss://`, which matters for company mode. Modelled as a `runs` resource plus an
append-only sequence-numbered event log, so closing the tab does nothing and
reopening replays then tails.

Progress is a step counter and elapsed time, never a percentage. The agent
works on a scratch branch. Review is per-file checkboxes, and **deletions are
rendered loud and sorted to the top** — "the reorganizer quietly ate 40 notes"
is the single failure that would end the product's credibility.

## 9. Dual deployment

Home and company differ in exactly three concerns: who is calling, where the
credential lives, and which repository. Three interfaces:

```go
package identity;  type Provider interface { Identify(*http.Request) (User, error) }
package credstore; type Store    interface { GitHubToken(context.Context, User) (string, error) }
package repostore; type Resolver interface { RepoFor(User) (owner, name, branch string) }
```

`--mode=local` (default) wires `localUser` + OS keychain + config file.
`--mode=server` wires `oidcUser` + server-side GitHub App tokens + org policy.
Roughly 95% of the code is byte-identical, and the Kubernetes deployment runs
the same executable the user curl-installs.

Company mode eliminates PATs entirely: a GitHub App with user-to-server OAuth,
tokens held server-side against the SSO identity, the browser holding nothing
but its SSO session cookie. Scoped, revocable, expiring, auditable.

`GET /api/config` returns `{mode, identity, capabilities, storage}`. **All
mode-dependent UI derives from `capabilities`**, never from scattered
`if (mode === 'team')`. A control the mode cannot honour is absent, not
disabled.

### 9.1 Known soft spot

Company-mode reorganize needs the user's own CLI, which does not exist on a
hosted server. The proposal is a local companion (`team-memory agent`) with a
pairing code, the browser streaming from the local process — because running an
agent server-side under a shared org credential across other people's memories
is a permissions and audit problem larger than the feature.

No precedent for this pairing flow was located. **It needs its own prototype
and is the most likely part of "both modes in v1" to slip.**

## 10. Migration and delivery

### 10.1 Repository strategy

Rewrite in place on a `v2` branch. `main` continues to work until `v2` merges.

### 10.2 Data migration

`team-memory migrate` converts a v1 repository in place:

1. Parse existing `### Entry:` blocks from every tracked Markdown file, using
   the `renderEntry` format as the grammar.
2. Emit one content-addressed file per entry under `memories/team/`, deriving
   `created` from the entry timestamp and `author` from the commit that added
   the block.
3. Regenerate `INDEX.md` from the result.
4. Move `UNSURE.md` entries into `staging/pending/` so they arrive in the review
   queue rather than being silently promoted.
5. Write the whole conversion as a single commit on a branch, and print a diff
   summary. Never rewrite history.

Migration is idempotent and re-runnable. An entry that fails to parse is copied
verbatim into `memories/team/unparsed/` and reported, never dropped.

### 10.3 Build and release

The `scripts/copy-prompts.sh` copy-into-the-module pipeline is deleted. The
frontend builds with Vite directly into the path `go:embed` reads, prompts are
embedded from their source directory, and `mcp/frontend/` leaves `.gitignore`.

`.github/workflows/release.yml` currently has **no `actions/setup-node`** and
will break the moment the frontend needs a build step. Both it and
`.goreleaser.yaml` gain the Node setup and `npm ci && npm run build`.

Cross-compilation stays `CGO_ENABLED=0` for every target. Binary budget: 7–9 MB.
No UPX — it triggers antivirus false positives on Windows and breaks macOS
notarisation.

### 10.4 What gets deleted

- `scripts/copy-prompts.sh` and the `prompts` make target.
- `tryListen(":80")` and the hosts-file hostname.
- The 409-retry loop in `frontend/src/services/github.js`.
- The read-modify-write append in `mcp/hook/handler.go`.
- `UNSURE.md` as a concept, replaced by the review queue.
- The duplicated entry renderer (`entries.js` and `render.go`): one Go
  implementation, exposed to the UI over the API.
- The vanilla frontend, replaced route by route.

### 10.5 Testing

| Layer | Approach |
|---|---|
| Storage | Table-driven over a temp git repo. Concurrent-write test asserting two machines adding memories merge with no conflict. |
| Privacy | **Permanent regression test** asserting no path under `staging/` can be committed. |
| Egress | Test asserting zero connections outside the allowlist in `--mode=local`. |
| Security | Table-driven guard tests: rebound Host rejected, absent Origin allowed, bad Origin 403, bad token 401. |
| Hooks | Golden-file tests per harness: real payload JSON in, expected action out. |
| Installers | Run against fixture config files; assert idempotency and correct transport discriminator. |
| Migration | Fixture v1 repository, assert entry count preserved and unparsed entries reported. |
| E2E | Playwright over the review queue: approve, edit-approve, reject, undo, bulk. |

### 10.6 Build order

1. Storage layer plus migration. Everything else depends on the data model.
2. Daemon, guard, and listeners.
3. Hook binary plus the converged contract.
4. `install claude` and `install codex` — the two primary targets.
5. Capture pipeline with the LLM cost cap.
6. Memory Profile, then the Review Queue.
7. Browse, Bin, Settings.
8. Remaining installers and `install doctor`.
9. Reorganize.
10. `--mode=server`.

Company mode is last because it is the least verified and the most likely to
slip; nothing before it depends on it.

## 11. Verification required before implementation

| Item | Why it matters |
|---|---|
| `claude -p` subscription billing | Direct cost hazard on every session end. §6.5. |
| gitleaks import path and library weight | Affects binary budget and the capture pipeline. |
| go-git v5.19.2 vs the v5.19.1 symlink / path-traversal advisories | Security. |
| Whether `modernc.org/sqlite` FTS5 needs a build tag | Blocks search if wrong. |
| Cursor and Copilot transcript line schemas | Both undocumented. |
| Whether Cursor, Windsurf, Copilot, Gemini send `Origin` | Unverified; the allowlist tolerates absence, so this is confirmation, not a blocker. |
