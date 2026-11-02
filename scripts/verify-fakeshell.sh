#!/usr/bin/env bash
# Proves that the fakeshellonly build of scomp cannot start processes:
#   1. no package that spawns processes or attaches PTYs is in its dependency
#      graph, and
#   2. the linked binary contains no process-start functions.
# The regular build is checked too, so a broken check cannot pass silently.
# See docs/FAKESHELL.md.
set -euo pipefail
cd "$(dirname "$0")/.."

forbidden_pkgs='^(os/exec|github\.com/creack/pty|github\.com/fsnotify/fsnotify|github\.com/smart-bit-me/scomp-agent/internal/(pty|tmuxsess|screensess))$'
forbidden_syms=' (os\.StartProcess|os\.startProcess|syscall\.forkExec|syscall\.StartProcess|os/exec\.)'
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
status=0

for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
	export GOOS=${target%/*} GOARCH=${target#*/} CGO_ENABLED=0
	if deps=$(go list -deps -tags fakeshellonly ./cmd/scomp | grep -E "$forbidden_pkgs"); then
		echo "FAIL $target: fakeshellonly build depends on:" $deps
		status=1
	fi
	go build -tags fakeshellonly -o "$tmp/fake" ./cmd/scomp
	go tool nm "$tmp/fake" >"$tmp/fake.nm"
	if syms=$(grep -E "$forbidden_syms" "$tmp/fake.nm" | head -5); then
		echo "FAIL $target: fakeshellonly binary links process-start code:"
		echo "$syms"
		status=1
	fi
	go build -o "$tmp/real" ./cmd/scomp
	go tool nm "$tmp/real" >"$tmp/real.nm"
	if ! grep -qE "$forbidden_syms" "$tmp/real.nm"; then
		echo "FAIL $target: the check found no process-start code in the regular build either; it is not detecting anything"
		status=1
	fi
	[ $status -eq 0 ] && echo "ok   $target: fakeshellonly build has no process-spawning code"
done
exit $status
