//go:build fakeshellonly

package main

import (
	"context"
	"errors"

	"github.com/smart-bit-me/scomp-agent/internal/sessions"
)

// multiplexerSupported reports whether this build can attach real sessions.
const multiplexerSupported = false

// defaultModeFlag is the --mode default of the fakeshellonly build, which
// cannot attach to real sessions.
const defaultModeFlag = "fakeshell"

// startMultiplexerSource is unavailable: this build links no tmux, Screen or
// PTY code (see docs/FAKESHELL.md).
func startMultiplexerSource(context.Context, *sessions.Registry, sessions.SessionMode) (sessionSource, func(), error) {
	return nil, nil, errors.New("this scomp build supports only --mode fakeshell")
}
