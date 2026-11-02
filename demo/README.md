# Demo host for app review

`scomp-demo.service` is a single systemd unit that keeps an always-on demo
machine for App Store / Google Play reviewers: the real `scomp` agent plus three
tmux sessions that only run scripted programs.

| Session | What it runs |
|---|---|
| `demo` | a simulated shell; nothing typed is executed (`help`, `ls`, `cat README.md`, `build`, `date`, `clear`) |
| `agent` | a simulated AI agent asking "Apply this change? (y/n)" |
| `logs` | a scrolling deploy log |

## Status (2026-11-02)

**Draft — not yet run under systemd.** Verified on macOS only: the inline
scripts pass `bash -n` after systemd unescaping (`$$`→`$`, `%%`→`%`), and the
tmux lockdown leaves no prefix and no reachable key bindings (only the built-in
`move` table); the fake shell survives `Ctrl-C`/`Ctrl-D` and never executes input.

Next steps on the Fedora 45 NAS:

1. Install per the header of `scomp-demo.service`; run
   `systemd-analyze verify` and `systemd-analyze security scomp-demo`.
2. `systemctl --version` must be ≥ 258 for `ProtectHostname=private:review-host`;
   otherwise drop that line (the app then shows the NAS hostname).
3. Pair once with the review account (`review@scomp.me`, Firebase Email/Password,
   invite-only) using the `systemd-run --pty …` command in the header.
4. `systemctl enable --now scomp-demo`; confirm the app shows `review-host` with
   the three sessions, that typing works in `demo`, and that a restart
   (`systemctl restart scomp-demo`) brings them back.

## Design notes

- One agent has one `--mode` for all its sessions, so all three are "full";
  safety comes from the sessions running fake programs and tmux having no
  bindings, not from the mode. Showing real approvals-only / read-only sessions
  needs a second agent identity (a template unit `scomp-demo@.service`).
- Runs as a `DynamicUser` in a tight sandbox (no capabilities, read-only system,
  `@system-service` syscalls, 256 MB / 64 tasks / 50% CPU) and resets daily
  (`RuntimeMaxSec=1d`). Identity lives in `/var/lib/scomp-demo/.config/scomp`.
- Alternatives considered: a mock agent speaking the protocol (more code, drifts
  from the real agent); a guest mode with a shared demo host for anonymous
  Firebase users (needs relay changes and a security review — good later for
  first-run UX).
