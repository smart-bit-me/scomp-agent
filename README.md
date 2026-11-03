# scomp-agent

The agent binary that runs on a developer's machine. It discovers active terminal sessions (tmux, screen) and relays PTY traffic to a browser over WebSocket via the scomp relay server.

## Platform support

| OS | amd64 | arm64 |
|---|---|---|
| Linux | ✓ | ✓ |
| macOS | ✓ | ✓ |

tmux and screen discovery work on both platforms.

## Install

### Homebrew (macOS and Linux)

```bash
brew tap smart-bit-me/scomp
brew trust smart-bit-me/scomp   # Homebrew 7+ only loads third-party taps you trust
brew install scomp
```

Upgrade with `brew upgrade scomp`. The formula lives in
[smart-bit-me/homebrew-scomp](https://github.com/smart-bit-me/homebrew-scomp), which also ships
[`scomp-mcp`](https://github.com/smart-bit-me/scomp-mcp) (`brew install scomp-mcp`).

### Prebuilt binary

Checksum-verified binaries for Linux and macOS (amd64 and arm64) are attached to every
[release](https://github.com/smart-bit-me/scomp-agent/releases/latest). The install command
for your platform is on [scomp.me](https://scomp.me/#snippet).

## Build

```bash
make build          # native binary → bin/scomp
make build-all      # all 4 cross-compile targets → bin/scomp-{linux,darwin}-{amd64,arm64}
make build-fakeshell   # demo-only build, no PTY/tmux/Screen code → bin/scomp-fakeshell
make verify-fakeshell  # prove that build cannot start processes
```

## Run

```bash
make run            # go run ./cmd/scomp
./bin/scomp         # or run the compiled binary directly
```

By default the agent connects to `wss://link.scomp.me/agent`, discovers existing
tmux and screen sessions, prints a mobile link/QR code, and reacts to session
creation and removal. GNU screen changes arrive through socket filesystem
events; tmux changes arrive through a read-only, size-ignoring control-mode
client. There is no session polling timer. PTYs are attached lazily when a
client first opens them.

Useful flags:

| Flag | Default | Description |
|---|---|---|
| `--server` | `wss://link.scomp.me/agent` | Relay WebSocket URL |
| `--mode` | `readonly` | Input policy: `full`, `readonly`, or `approved-only`; or `fakeshell` (see below) |
| `--hostname` | OS hostname | Name shown in the apps (`review-host` in fakeshell mode) |
| `--no-qr` | false | Print the URL without rendering a terminal QR code |
| `--version` | — | Print the build version and exit |

For local development:

```bash
./bin/scomp --server ws://localhost:8000/agent
```

The safe default is `readonly`, so a connected client can monitor but cannot
type. Select `--mode full` deliberately to let an authorized remote client type
arbitrary input into the PTY, or `--mode approved-only` to accept only Enter,
Tab, Escape, Ctrl+C, and `y`/`n` confirmation sequences. Unknown mode values are
rejected instead of falling back to full control.

## Fake shell mode (demo / app review host)

```bash
make build-fakeshell
./bin/scomp-fakeshell            # same as: scomp --mode fakeshell
```

Instead of tmux and Screen, the agent offers three simulated sessions:
`shell` (a pretend bash with `ls`, `cat`, `ps`, `top`, live `date`, `git`,
`build`, …), `agent` (a pretend coding agent asking `y/n`) and `logs` (a
scrolling log). Everything a client types is interpreted in Go; nothing is ever
executed, opened or sent to the operating system, so the host can run anywhere
and as any user. The `fakeshellonly` build does not even link the PTY code, which
`make verify-fakeshell` (run in CI) proves. Fake mode keeps a separate identity
under `~/.config/scomp/fakeshell/` and announces itself as `review-host`.
Design and safety argument: [`docs/FAKESHELL.md`](docs/FAKESHELL.md); the
systemd unit for the always-on review host is in [`demo/`](demo/).

## Identity and pairing

The agent creates a random UID and Ed25519 key under `~/.config/scomp/` on first
run (files are mode `0600`). Each relay connection proves possession of that key
with a challenge-response handshake. Pairing a machine to an account additionally
requires typing the PIN shown by the client into the foreground agent process.

Back up the config directory if the machine identity must survive migration.
Key rotation is currently manual and requires clearing the relay's pinned key
and pairing the machine again.

## Compatibility

Agent `v0.2.4` is the minimum recommended version for relay `v0.4.x`. It reports
its build version during the authenticated relay handshake so paired clients can
show update guidance. Development builds report their Git-derived build version.

## Test

```bash
make test           # go test ./... in both the regular and fakeshellonly builds
make test-race      # race detector
make test-cover     # coverage report → coverage.html
```

## Key packages

| Package | Description |
|---|---|
| `protocol/` | Wire types shared with scomp-server (`AgentMsg`, `ServerMsg`, `SessionInfo`) |
| `internal/stream/` | Ring buffer + broadcast + ordered snapshots, shared by PTY and fake sessions |
| `internal/pty/` | PTY session + DECCKM tracking |
| `internal/sessions/` | Session registry (`Terminal` interface) with dedup by source key |
| `fakeshell/` | Simulated sessions for `--mode fakeshell`: shell, in-memory file tree (`vfs/`), key decoder and line editor (`term/`) |
| `internal/tmuxsess/` | tmux session discovery (linked sessions for isolation) |
| `internal/screensess/` | screen session discovery |
| `internal/auth/uid.go` | Persistent UID at `~/.config/scomp/uid` |
| `internal/auth/key.go` | Persistent Ed25519 identity at `~/.config/scomp/key` |

## License

Licensed under the **GNU Affero General Public License v3.0** — see
[`LICENSE`](LICENSE).

Security model and private reporting instructions: [`SECURITY.md`](SECURITY.md).
