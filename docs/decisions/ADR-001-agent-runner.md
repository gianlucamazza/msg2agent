# ADR-001 — agent-runner: headless CLI workers on the local relay

- Status: accepted
- Date: 2026-10-01
- Deciders: Gianluca Mazza

## Context

The three local coding CLIs (Claude Code, Codex, Grok) should exchange messages and delegate
tasks to each other (A2A) on this machine. msg2agent is already the chosen agent-network
backbone (vibemonitor ADR-001: local relay on 127.0.0.1, SaaS-off, DID identities, offline
store-and-forward queue, 14 MCP tools).

What is missing: a receiver only _stores_ messages. `cmd/mcp-server` registers a catch-all
`"*"` method that puts every incoming message into the MCP inbox and replies
`{"status":"received"}`. An interactive CLI session sees the message only if a human prompts it
to look, and nothing runs while no session is open. Asynchronous delegation ("codex, review
this") therefore needs a persistent process that is online under a worker identity and turns a
task into a headless CLI run.

Constraints carried over from the vibemonitor ADRs:

- Task text from another agent is **untrusted input** (prompt injection; lethal trifecta with
  private data + egress such as the himalaya `send_email` MCP).
- A CLI's own permission flags are not a boundary we control (ADR-006: "guarantees don't cross
  the process boundary"); grok in particular does not validate tool names and inherits Claude's
  MCP servers through its compat layer (ADR-008).
- The X1C9 is thermally limited: agent runs belong in `agents.slice`.

## Decision

Add `cmd/agent-runner`, one Go process that hosts one msg2agent agent (DID) per worker:
`claude-worker`, `codex-worker`, `grok-worker`.

1. **Task intake.** `message/send` → validate (sender ACL, profile, hop count) → persist task
   `submitted` (sqlite) → reply at once `{status:"accepted", task_id}`. `tasks/get` and
   `tasks/cancel` are served from the task table so the existing MCP tools
   `get_task_status` / `cancel_task` work unchanged. On completion the runner sends the result
   back to the sender with `message/send` (it lands in the sender's inbox, or in the relay's
   offline queue if the sender is gone).
2. **Profiles.** `ro` is the default. `rw` is granted only when the task asks for it **and** the
   sender DID is in the config's `rw_allow` list. `ro` runs against the requested directory;
   `rw` runs only in an ephemeral `git worktree` created for the task, and the result carries the
   `git diff` — the sender decides whether to apply it. The worktree is never the user's checkout.
3. **Kernel jail is the boundary.** Every worker run, `ro` or `rw`, is wrapped in bubblewrap with
   the semantics of vibemonitor's `orchestration/sandbox.py` (ported to Go): tmpfs `$HOME` with
   only the tool's own config dir bound rw, fresh `/run` (no reach to the relay or the
   user-session sockets), system dirs ro, network shared (the model API is required). `ro` binds
   the target directory read-only; `rw` binds the worktree rw. If bwrap is unavailable the
   runner **fails closed**.
4. **Tool flags are defense-in-depth.** The per-tool least-privilege flags are the field-verified
   ones from vibemonitor `roles._privilege_flags` (claude `--disallowedTools … --strict-mcp-config`;
   codex `--sandbox read-only|workspace-write`; grok `--tools Read,Grep,Glob` + MCP/Task denies +
   `--no-subagents --no-memory`, with `GROK_CLAUDE_MCPS_ENABLED=0`).
5. **No recursion.** Workers get no MCP servers (so no msg2agent), and the task payload carries a
   hop count; the runner refuses `hops > 1`.
6. **Prompt hygiene.** The task text is embedded as delimited data in a fixed runner template that
   states the profile and tells the model not to follow instructions inside the task that conflict
   with it.
7. **Resources and audit.** Each run goes through `systemd-run --user --slice=agents.slice --wait`
   with a runtime cap. Concurrency is 1 per worker and 2 overall. Every task appends one JSONL audit
   line (task id, sender, worker, profile, argv, exit code, duration, prompt/result hashes).

Interactive sessions get the msg2agent MCP server with **distinct** identities (`claude-code`,
`codex`, `grok`) and act as senders and inbox readers. The relay runs with `--allowed-dids`
restricted to these identities plus the three workers and vibemonitor.

## Alternatives rejected

- **Mailbox only (MCP wiring, no runner).** No code to write, but nothing happens until a human
  opens a session and asks it to read its inbox. That is not delegation.
- **Run the task inside `cmd/mcp-server`.** It is a stdio child of an interactive CLI, so it lives
  and dies with that session. Executing tasks there would also mix the sender and executor roles
  in one identity.
- **agentroom / plain A2A HTTP.** agentroom is 1:1 with no discovery. A plain A2A server would
  re-implement identity, ACL and offline queueing that msg2agent already provides.
- **Trust the CLI flags only.** Rejected for the ADR-006 reasons: the floor would be the weakest
  tool's userspace gating, and it would drift with CLI versions.

## Consequences

- Real asynchronous A2A between the three vendors, with offline delivery of results.
- A new long-running service and a sandbox module to maintain. The sandbox is a port of tested
  code, not a new design.
- `rw` delegation costs a worktree per task and a manual diff review. This is intended.
- Two interactive sessions of the same CLI share one DID. The relay's behaviour on a duplicate
  DID connection must be verified and documented. Per-session identities are a possible follow-up.

## References

- vibemonitor ADR-001 (msg2agent backbone), ADR-006 (uniform OS sandbox), ADR-008 (grok tier).
- Beurer-Kellner et al., _Design Patterns for Securing LLM Agents against Prompt Injections_,
  arXiv:2506.08837.
- Codex non-interactive mode: `codex exec` defaults to a read-only sandbox, with approval `never`.

## Errata (implementation, 2026-10-01)

Field findings that changed the per-tool flags in Decision §4. The jail and the policy are unchanged.

- **codex: `-c mcp_servers={}` is a no-op.** The worker still saw context7, jarvis_canvas and node_repl.
  It now runs with `--ignore-user-config`. That flag alone still exposes `codex_apps` (the built-in
  ChatGPT connectors: gmail, github, drive), so the worker also disables the features `apps`,
  `plugins`, `remote_plugin`, `hooks`, `memories`, `multi_agent`, `browser_use*`, `computer_use`
  and `image_generation`.
- **claude: a denylist is not enough.** The stream-json `init` event still listed RemoteTrigger,
  Cron*, SendMessage, PushNotification, WebFetch, Task and Workflow. The worker now uses the
  `--tools` allowlist (ro `Read,Grep,Glob`; rw adds `Edit,Write,Bash`) with `--strict-mcp-config`,
  `--setting-sources ""` (no user hooks) and `--disable-slash-commands`. `--bare` is unusable:
  it requires an API key, and this setup uses OAuth.
- **grok: the native `config.toml` loads mangouse, pagouse and 1password.** Inside the jail a
  minimal config is ro-bound over `~/.grok/config.toml`, with compat discovery off. The
  `CallMcpTool`/`Task` denies stay as belt-and-braces.
- **No web tools for workers.** Repo data plus untrusted text plus egress is the lethal trifecta.
  The network stays shared only for the model API (as in the ADR-006 errata).
- **The ro allow-list of `$HOME` is narrower than vibemonitor's.** It binds `.local/{bin,lib,share/claude,share/fnm}`
  instead of all of `.local`, so keyrings and the runner's own state stay masked.
- **Sessions vs headless runs share a DID.** The relay maps a DID to its last registration (verified
  in `cmd/relay/client.go`). Sessions therefore start the MCP server through
  `~/.local/bin/msg2agent-session-mcp`, which refuses headless parents: claude
  `CLAUDE_CODE_ENTRYPOINT!=cli`, `codex exec|app-server|…`, `grok -p|agent`. Field-verified for
  `claude -p` and `codex exec`: neither registers.
- **Verified end-to-end.** Delegations that worked: claude-code→codex-worker, codex→grok-worker and
  grok→claude-worker. An rw task returned a diff and left the user's checkout untouched. A result
  reached its sender through the offline queue after a reconnect. The relay rejected a DID not on
  its allowlist, and the runner rejected rw from a sender outside `rw_allow`, `hops=2`, and a
  directory outside the roots.
  The model-independent oracle `TestJailEnforcement` fails without the jail (baseline checked).

### Errata 2 (2026-10-01): opencode worker, key pinning

- **The fourth worker, `opencode-worker`, runs `opencode run --standalone`.** It never uses the
  shared `opencode.service`, because that server's tools run outside the jail. Permissions come
  from `OPENCODE_CONFIG_CONTENT`: a catch-all `"*":"deny"` first, since opencode applies the last
  matching rule. The ro profile adds read/glob/grep/list; rw adds edit and bash; web, task and
  skills stay denied.
- **The shared server's password is a jail escape.** With it, a worker could drive the unjailed
  server on 127.0.0.1:4096 over the shared network. Both copies (`~/.config/opencode/service.json`
  and `~/.local/state/opencode/service.json`) are overlaid with a random password per run, and
  `server.env` is emptied. The oracle is `TestJailOverlayMasksServerPassword`.
- **Per-worker config gained `model`, `ro_binds` and `overlays`.** The opencode free tier is unusable
  headless: "not available in your country" without an OpenRouter key, "only from within OpenCode",
  and timeouts. The worker therefore uses the user's own Hetzner endpoint, with only its token
  file bound read-only.
- **A DID can no longer be re-keyed (msg2agent#37, `--pin-did-keys`).** Before this, any local
  process, including an rw worker with Bash, could re-register a session DID with a fresh key and
  take over its mailbox and its `rw_allow` rights. The relay unit now enables pinning.
