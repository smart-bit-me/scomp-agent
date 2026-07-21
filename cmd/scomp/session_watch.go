package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsnotify"
	"github.com/smart-bit-me/scomp-agent/internal/screensess"
	"github.com/smart-bit-me/scomp-agent/internal/tmuxsess"
)

// watchSessions reacts to screen socket changes and tmux control-mode events.
// tmux uses one socket for all sessions, so filesystem events alone cannot
// report sessions created inside an existing server.
func watchSessions(ctx context.Context, catalog *sessionCatalog) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("sesswatch: %v", err)
		return
	}
	defer watcher.Close()

	watched := make(map[string]bool)
	watchDir := func(dir string) {
		if dir == "" || watched[dir] {
			return
		}
		if err := watcher.Add(dir); err == nil {
			watched[dir] = true
			return
		} else if !os.IsNotExist(err) {
			log.Printf("sesswatch: watch %s: %v", dir, err)
			return
		}
		parent := filepath.Dir(dir)
		if parent != dir && !watched[parent] {
			if err := watcher.Add(parent); err == nil {
				watched[parent] = true
			} else if !os.IsNotExist(err) {
				log.Printf("sesswatch: watch %s: %v", parent, err)
			}
		}
	}

	tmuxDir := tmuxsess.SocketDir()
	screenDir := screensess.SocketDir()
	watchDir(tmuxDir)
	watchDir(screenDir)

	tmuxChanged := make(chan struct{}, 1)
	tmuxDone := make(chan error, 1)
	tmuxMonitorRunning := false
	startTmuxMonitor := func() {
		if tmuxMonitorRunning {
			return
		}
		sessions := tmuxsess.List()
		if len(sessions) == 0 {
			return
		}
		tmuxMonitorRunning = true
		target := sessions[0].Name
		go func() {
			tmuxDone <- monitorTmuxSessions(ctx, target, tmuxChanged)
		}()
	}
	startTmuxMonitor()

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if !event.Has(fsnotify.Create) && !event.Has(fsnotify.Remove) && !event.Has(fsnotify.Rename) {
				continue
			}
			if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				delete(watched, event.Name)
			}
			watchDir(tmuxDir)
			watchDir(screenDir)
			catalog.scan(true)
			startTmuxMonitor()
		case <-tmuxChanged:
			catalog.scan(true)
		case err := <-tmuxDone:
			tmuxMonitorRunning = false
			catalog.scan(true)
			if err != nil && ctx.Err() == nil {
				log.Printf("sesswatch: tmux control mode: %v", err)
			}
			// A later socket event will retry. Avoid a hot loop if this tmux
			// version does not support the required control-client flags.
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("sesswatch: %v", err)
		case <-ctx.Done():
			return
		}
	}
}

func monitorTmuxSessions(ctx context.Context, target string, changed chan<- struct{}) error {
	cmd := exec.CommandContext(
		ctx,
		"tmux",
		"-C",
		"attach-session",
		"-f", "read-only,ignore-size,no-output,no-detach-on-destroy",
		"-t", target,
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return err
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		if !isTmuxSessionChange(scanner.Text()) {
			continue
		}
		select {
		case changed <- struct{}{}:
		default:
		}
	}
	_ = stdin.Close()
	if err := scanner.Err(); err != nil {
		_ = cmd.Wait()
		return err
	}
	if err := cmd.Wait(); err != nil && ctx.Err() == nil {
		return fmt.Errorf("control client exited: %w", err)
	}
	return nil
}

func isTmuxSessionChange(line string) bool {
	line = strings.TrimSpace(line)
	return line == "%sessions-changed" ||
		strings.HasPrefix(line, "%session-changed ") ||
		strings.HasPrefix(line, "%session-renamed ")
}
