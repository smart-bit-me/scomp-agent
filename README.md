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

By default the agent connects to `wss://link.scomp.me/agent`, discovers existing
tmux and screen sessions, prints a mobile link/QR code, and watches for new
session sockets. PTYs are attached lazily when a client first opens them.

Useful flags:

| Flag | Default | Description |
|---|---|---|
| `--server` | `wss://link.scomp.me/agent` | Relay WebSocket URL |
| `--mode` | `full` | Input policy: `full`, `readonly`, or `approved-only` |
| `--no-qr` | false | Print the URL without rendering a terminal QR code |
| `--version` | — | Print the build version and exit |

For local development:

```bash
./bin/scomp --server ws://localhost:8000/agent
```

`full` mode lets an authorized remote client type arbitrary input into the PTY.
Use `readonly` when monitoring only, or `approved-only` to accept only Enter,
Tab, Escape, Ctrl+C, and `y`/`n` confirmation sequences.

## Identity and pairing

The agent creates a random UID and Ed25519 key under `~/.config/scomp/` on first
run (files are mode `0600`). Each relay connection proves possession of that key
with a challenge-response handshake. Pairing a machine to an account additionally
requires typing the PIN shown by the client into the foreground agent process.

Back up the config directory if the machine identity must survive migration.
Key rotation is currently manual and requires clearing the relay's pinned key
and pairing the machine again.

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
| `internal/auth/key.go` | Persistent Ed25519 identity at `~/.config/scomp/key` |

## License

Licensed under the **GNU Affero General Public License v3.0** — see
[`LICENSE`](LICENSE).

Security model and private reporting instructions: [`SECURITY.md`](SECURITY.md).
