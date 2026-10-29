# Security policy

## Trust model

`scomp` attaches a remote client to terminal sessions running as your local
user. The default `--mode readonly` lets an authorized client watch sessions but
drops all remote input. With `--mode full`, anyone authorized by the relay to
open a session can send arbitrary keystrokes and therefore execute commands with
your local account's permissions; enable it deliberately.

The agent authenticates itself to the relay with an Ed25519 key stored at
`~/.config/scomp/key`. This prevents another client that only knows the public
UID from impersonating an already-pinned agent. It does not make the relay
zero-knowledge or end-to-end encrypted: the relay handles plaintext terminal
traffic and remains part of the trusted computing base.

To allow arbitrary remote input, run:

```bash
scomp --mode full
```

`--mode approved-only` permits only a small set of confirmation/control
sequences (Enter, Tab, Escape, Ctrl+C, and `y`/`n`). The mode is enforced by the
agent, not by the relay. Read-only sessions also ignore remote resize requests.

## Protecting local identity

- Keep `~/.config/scomp/uid` and `~/.config/scomp/key` private and backed up when
  machine identity must survive migration.
- Pair only when you initiated the request in your client and recognize the
  displayed context.
- Key rotation/revocation is currently manual: remove the server-side pinned
  key and host binding, regenerate local identity, then pair again.
- Prefer `wss://` relay URLs. Plain `ws://` is intended only for trusted local
  development.

## Reporting a vulnerability

Do not open a public issue for an unpatched vulnerability. Use the repository's
private GitHub Security Advisory reporting flow (or contact the repository owner
privately if that flow is unavailable). Include impact, reproduction steps, and
the affected version. Avoid including real terminal output, credentials, private
keys, or user data.

You should receive an acknowledgement within 72 hours. Coordinated disclosure
timing will be agreed after the report is reproduced and a fix is available.

## Supported versions

Security fixes are made on the latest release line. Upgrade to the newest GitHub
Release before reporting behavior that may already have been fixed.
