# scomp-agent

The agent binary that runs on a developer's machine. It discovers active terminal sessions (tmux, screen) and relays PTY traffic to a browser over WebSocket via the scomp relay server.

## Platform support

| OS | amd64 | arm64 |
|---|---|---|
| Linux | ✓ | ✓ |
| macOS | ✓ | ✓ |

tmux and screen discovery work on both platforms.

## Build

```bash
make build          # native binary → bin/scomp
make build-all      # all 4 cross-compile targets → bin/scomp-{linux,darwin}-{amd64,arm64}
```

## Run

```bash
make run            # go run ./cmd/scomp
./bin/scomp         # or run the compiled binary directly
```

By default the agent connects to `wss://link.scomp.me/agent`. Override with `--server`:

```bash
./bin/scomp --server ws://localhost:8000/agent
```

## Test

```bash
make test           # go test ./...
make test-race      # race detector
make test-cover     # coverage report → coverage.html
```

## Key packages

| Package | Description |
|---|---|
| `protocol/` | Wire types shared with scomp-server (`AgentMsg`, `ServerMsg`, `SessionInfo`) |
| `internal/pty/` | PTY session + 1 MiB ring buffer + DECCKM tracking |
| `internal/sessions/` | Session registry with dedup by source key |
| `internal/tmuxsess/` | tmux session discovery (linked sessions for isolation) |
| `internal/screensess/` | screen session discovery |
| `internal/auth/uid.go` | Persistent UID at `~/.config/scomp/uid` |

## License

Licensed under the **GNU Affero General Public License v3.0** — see
[`LICENSE`](LICENSE).
