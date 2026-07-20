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
```

Always run `gofmt -w` on touched files and `make test` before committing.

## Layout

| Path | Role |
|---|---|
| `cmd/scomp/` | entrypoint; server/mode/QR/version flags; pairing prompt |
| `protocol/` | wire types SHARED with scomp-server (`AgentMsg`, `ServerMsg`, `SessionInfo`) |
| `internal/pty/` | PTY session, 1 MiB ring buffer, DECCKM tracking |
| `internal/sessions/` | session registry, dedup by source key |
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
- **Session modes are enforced agent-side.** The relay is not trusted to enforce
  `readonly` or `approved-only`; keep input checks in `handleServerMsg`.

## Release

Tags `vX.Y.Z` drive GitHub Actions (`release.yml`): it builds the four binaries,
attests provenance, and publishes them to a GitHub Release. The workflow is gated
on the `production` environment and uses SHA-pinned actions.

## Conventions

- `gofmt`-clean, `go vet`-clean, `make test` green, and `make build-all` green
  before opening a PR.
- Branches: `dev` for active work, `main` for release; PRs target `dev` first.
