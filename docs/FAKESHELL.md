# Fake shell mode (`scomp --mode fakeshell`)

Status: implemented on `feat/fakeshell` (November 2026). Purpose: an always-on
demo host for App Store / Google Play review that can run anywhere — any
machine, any user, including root — without giving the reviewer anything real.

## Flow

```
reviewer app → link.scomp.me (unchanged) → scomp --mode fakeshell
                                            └─ in-process sessions: shell, agent, logs
                                               (no PTY, no tmux/Screen, no child process)
```

The agent advertises fixed sessions with ordinary `session_add` messages
(`mode: full`, so the apps allow typing). Keystrokes arrive as `input` and go
to a Go function; output returns through the same ring buffer, broadcast and
snapshot path as a PTY. The relay, the wire protocol and the apps are
unchanged; the relay cannot tell a fake session from a real one.

## Agent changes

| Piece | Change |
|---|---|
| `internal/stream` | Ring buffer + subscriber broadcast + ordered snapshots, extracted from `internal/pty` so PTY and fake sessions share the exact replay invariant documented in `protocol/`. Adds `Reset`, used to trim the ring at full-screen redraws. |
| `internal/sessions` | The registry stores a `Terminal` interface instead of `*pty.Session`. PTY spawning (`Create`, `CreateWithID`) lives in `spawn.go`, compiled only without the `fakeshellonly` tag. |
| `cmd/scomp` | `runAgent` takes a `sessionSource`. The tmux/Screen catalog is one source (files tagged `!fakeshellonly`); the fake catalog is the other. `--mode fakeshell` branches in `main` before any multiplexer code is constructed. |
| identity | Fake mode keeps its UID and key under `<config>/fakeshell/`, so it never takes over the identity of a real agent run by the same user. Pair the review account with that UID. |
| `--hostname` | Overrides the hostname sent in `hello`; fake mode defaults to `review-host` so the reviewer never sees the real machine name. The fake `hostname` command prints the same value. |

Session labels are plain (`shell`, `agent`, `logs`): the apps only special-case
`tmux:`/`screen:` prefixes, so no multiplexer key bar appears. Session IDs are
derived from the agent UID and the session name, so they are stable across
restarts.

## `fakeshell/` packages

Pure Go; none of them imports `os`, `os/exec`, `net`, `syscall`, `unsafe` or
`plugin` (enforced by `fakeshell/safety_test.go`).

| Package | Role |
|---|---|
| `fakeshell/vfs` | In-memory tree: embedded read-only base, generated files (`/proc/uptime`), capped writable overlay (128 entries, 256 KiB). Paths are cleaned against `/`, so `..` never leaves the tree. |
| `fakeshell/term` | Key decoder (CSI and SS3 arrows, Home/End/Delete) and a line editor: history, Tab completion, Ctrl-A/E/U/W/L/C/D, wrapping-aware redraw. |
| `fakeshell` | Shell commands, fake process table, the three programs, the embedded file tree, and `Session`, the `Terminal` implementation. |

Programs:

- **shell** — banner listing every command (generated from the command table,
  so it cannot drift), then a prompt. Commands include `ls`, `cd`, `cat`, `ps`
  (`aux`, `-ef`), `top` (full screen, 2 s refresh, sized to the window, `q` or
  Ctrl-C; `-b -n N`), live `date` (`-u`, `-R`, `-I`, `+FORMAT`), `uptime`,
  `free`, `df`, `touch`/`mkdir`/`rm` (overlay only), `build` (timed output).
  Pipes, redirection and chaining print an explanation instead of running.
- **agent** — a pretend coding agent asking "Apply this change? (y/n)"; works
  with `approved-only` key sets.
- **logs** — a scrolling deploy log with live timestamps.

All sessions are shared by every viewer, as with tmux. After 15 minutes
without input the shell resets (cwd, history, overlay) and redraws the banner,
so the next reviewer starts clean. A program that panics is recovered and
restarted; it cannot take the agent down.

## Safety argument

1. **Input reaches only the fake shell.** No command, path or argument is ever
   passed to the OS. The fake tree is embedded data plus memory, so
   `cat /etc/shadow` returns fake content even when the agent runs as root.
   The only real things read are the clock and the window size the client
   sends.
2. **No process spawning in the hosted binary.** `make build-fakeshell`
   builds with `-tags fakeshellonly`, which drops `internal/pty`,
   `creack/pty`, tmux and Screen. `make verify-fakeshell` (run in CI) checks
   that `go list -deps` contains no `os/exec` and no `creack/pty`, and that
   `go tool nm` finds no `os.StartProcess` or `syscall.forkExec` in the
   binary. Websocket, crypto, JSON and QR dependencies do not import
   `os/exec`.
3. **Bounded resources.** Input queue, line length (1024), overlay size,
   `sleep` (max 60 s), `top` refresh, a fixed number of sessions.
4. **What remains** is the agent's own network code (WebSocket and JSON
   parsing) — the same exposure as any agent — and its identity files, which
   only the agent writes. Running as root is therefore not required and not
   recommended: `demo/scomp-demo.service` still uses `DynamicUser` and the
   systemd sandbox as defence in depth.

The regular `scomp` binary also accepts `--mode fakeshell` (same code path,
branching before the multiplexer code); use the tagged build for anything
long-lived or exposed.
