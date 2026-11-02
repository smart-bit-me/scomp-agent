# AGENT.md — scomp-agent

Guidance for AI coding agents working in this repo. Keep changes minimal and
idiomatic to the surrounding Go.

## What this is

The agent binary (`scomp`) that runs on a developer's machine, discovers active
terminal sessions (tmux, screen), and relays PTY traffic to a browser over
WebSocket via the scomp relay server (default `wss://link.scomp.me/agent`).

- Module: `github.com/smart-bit-me/scomp-agent`
- Platforms: linux/darwin × amd64/arm64, `CGO_ENABLED=0`

## Build / test

```bash
make build          # native → bin/scomp (injects -X main.version via git describe)
make build-all      # 4 cross-compile targets → bin/scomp-{linux,darwin}-{amd64,arm64}
make test           # go test ./...
make test-race      # race detector
make lint           # golangci-lint run ./...
make build-fakeshell   # -tags fakeshellonly → bin/scomp-fakeshell (no PTY code)
make verify-fakeshell  # proves that build has no process-spawning code (CI runs it)
```

Always run `gofmt -w` on touched files and `make test` before committing.

## Layout

| Path | Role |
|---|---|
| `cmd/scomp/` | entrypoint; server/mode/QR/version flags; pairing prompt |
| `cmd/scomp/session_*` | stable multiplexer catalog, fsnotify, tmux control-mode events |
| `protocol/` | wire types SHARED with scomp-server (`AgentMsg`, `ServerMsg`, `SessionInfo`) |
| `internal/stream/` | ring buffer + broadcast + ordered snapshots (PTY and fake sessions) |
| `internal/pty/` | PTY session, DECCKM tracking |
| `internal/sessions/` | session registry of `Terminal`s, dedup by source key; PTY spawning in `spawn.go` |
| `cmd/scomp/multiplexer*.go` | tmux/Screen source (`!fakeshellonly`) and its stub (`fakeshellonly`) |
| `cmd/scomp/fakeshell_source.go` | fixed catalog for `--mode fakeshell` |
| `fakeshell/` | simulated shell/agent/logs programs, `vfs/` in-memory tree, `term/` keys + line editor |
| `internal/tmuxsess/` `internal/screensess/` | tmux / screen discovery |
| `internal/auth/` | persistent UID + Ed25519 key under `~/.config/scomp/` |

## Gotchas

- **`protocol/` is a shared contract.** Changing a wire type here means bumping
  the agent version AND updating the relay server's dependency in lockstep. Do
  not make breaking changes casually.
- **Cross-compile matters.** Keep any platform-specific code behind build tags
  so the darwin cross-compile stays green — verify with `make build-all`.
- **Version injection**: the binary version comes from
  `-ldflags "-X main.version=$(git describe --tags --always --dirty)"`. `go run`
  without the ldflag reports `dev`.
- **Identity files are compatibility-sensitive.** The UID and Ed25519 seed are
  both mode `0600`; changing their format needs a migration path. The server
  pins the public key on first use.
- **Pairing reads stdin.** `pair_request` handling prompts in the foreground;
  background/non-interactive agents cannot complete initial account pairing.
  `pairPrompter` owns the only stdin reader; a prompt expires after 2 min, a
  newer request replaces it, and the PIN goes out on the connection that is
  current when it is typed.
- **Session modes are enforced agent-side.** The relay is not trusted to enforce
  `readonly` or `approved-only`; keep input checks in `handleServerMsg`.
- **Session discovery is event-driven.** GNU screen uses socket filesystem
  events; tmux uses a read-only `ignore-size` control client. Do not replace
  either with a periodic scan timer.

- **Fake shell mode must stay unable to touch the system** (`docs/FAKESHELL.md`).
  Nothing under `fakeshell/` may import `os`, `os/exec`, `net`, `syscall`,
  `unsafe` or `plugin` (`fakeshell/safety_test.go`). Code that spawns processes
  or attaches PTYs must live in files tagged `//go:build !fakeshellonly`;
  `scripts/verify-fakeshell.sh` fails otherwise. New fake commands go in the
  command table in `fakeshell/commands.go`; the banner and `help` list them
  automatically. Never put `.go` or `go.mod` files in `fakeshell/honeyfs/`.
- **Run tests in both builds**: `make test` runs `go test` with and without
  `-tags fakeshellonly`.

## Release

Tags `vX.Y.Z` drive GitHub Actions (`release.yml`): it builds the four binaries,
attests provenance, and publishes them to a GitHub Release. The workflow is gated
on the `production` environment and uses SHA-pinned actions.

## Conventions

- `gofmt`-clean, `go vet`-clean, `make test` green, and `make build-all` green
  before opening a PR.
- Branches: `dev` for active work, `main` for release; PRs target `dev` first.

## Commit messages
- Never add `Co-Authored-By`, "Generated with", or any other AI/tool attribution to commits or PR descriptions. `.claude/settings.json` disables Claude Code's attribution for this repo.
