# Demo host for app review

An always-on demo machine for App Store / Google Play reviewers, signed in as
the invite-only review account (`review@scomp.me`, Firebase Email/Password).
It runs the `fakeshellonly` agent build (`scomp --mode fakeshell`): three
simulated sessions handled in Go, with no PTY, tmux, Screen or child processes
linked in, so nothing a reviewer types reaches the OS. See `docs/FAKESHELL.md`.

| Session | What it runs |
|---|---|
| `shell` | a simulated shell; nothing typed is executed (`help` lists the commands: `ls`, `cat`, `ps`, `top`, `date`, `build`, …) |
| `agent` | a simulated AI agent asking "Apply this change? (y/n)" |
| `logs` | a scrolling deploy log |

The host is announced as `review-host` (the `--hostname` default in fakeshell
mode), so the real machine name is never shown.

## Files

| File | Install as |
|---|---|
| `scomp-demo.sysusers` | `/etc/sysusers.d/scomp-demo.conf` — the `scomp-demo` system user (no shell, no password) |
| `scomp-demo.service` | `/etc/systemd/system/scomp-demo.service` — the sandboxed agent |

The service runs as a real system user rather than `DynamicUser`: a dynamic
user exists only while its unit runs, so `id scomp-demo`, `sudo -u scomp-demo`
and pairing outside the unit fail on the host.

## Setup (Fedora)

1. Build the binary from a release tag: `GOOS=linux GOARCH=amd64 make build-fakeshell`
   (or `arm64`). It is not published as a release asset.
2. Install the binary, user, state directory and unit — the exact commands are
   in the header of `scomp-demo.service`. Check `id scomp-demo` prints the user.
3. Pair once with the review account, as the service user:
   `sudo -u scomp-demo HOME=/var/lib/scomp-demo /usr/local/bin/scomp-fakeshell`.
   In the app (signed in as `review@scomp.me`): Pair → enter the printed UID →
   type the PIN into the terminal → Ctrl-C.
4. `sudo systemctl enable --now scomp-demo`, then `journalctl -u scomp-demo -f`.
5. In the app: Hosts shows `review-host` with `shell`, `agent`, `logs`; typing
   works in `shell`; `sudo systemctl restart scomp-demo` brings them back.

Optional checks: `systemd-analyze verify /etc/systemd/system/scomp-demo.service`
and `systemd-analyze security scomp-demo`.

### Moving from the earlier DynamicUser draft

If the old unit already ran, its state lives in `/var/lib/private/scomp-demo`.
Stop the service, install the new files as above, and check
`sudo ls -la /var/lib/scomp-demo/.config/scomp/fakeshell/` after the first
start. If the UID file is not there, pair again (step 3) and remove the stale
host from the review account in the app.

## Design notes

- All sessions are `full` so reviewers can type; safety comes from the fake
  programs, not from the session mode.
- Sandbox: no capabilities, read-only system, only the agent binary may be
  executed, `@system-service` syscalls, 128 MB / 32 tasks / 25% CPU, daily
  restart (`RuntimeMaxSec=1d`). The shell also resets after 15 idle minutes.
- Identity: `/var/lib/scomp-demo/.config/scomp/fakeshell/` (separate from any
  real agent run by the same user).
