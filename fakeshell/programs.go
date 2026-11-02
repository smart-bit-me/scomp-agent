package fakeshell

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/smart-bit-me/scomp-agent/fakeshell/term"
)

type agentTask struct {
	title string
	diff  []string
}

var agentTasks = []agentTask{
	{"update the dependency lockfile", []string{"~ go.sum  (+3 -1)"}},
	{"add a cache header to /healthz", []string{"~ app/main.go  (+1)", "+   w.Header().Set(\"Cache-Control\", \"no-store\")"}},
	{"fix the flaky TestStoreReconnect", []string{"~ internal/store/store_test.go  (+4 -2)", "-   time.Sleep(100 * time.Millisecond)", "+   waitFor(t, store.Ready)"}},
	{"bump the Go toolchain to 1.27.2", []string{"~ go.mod  (+1 -1)", "-   go 1.27", "+   go 1.27.2"}},
}

// runAgent is a pretend coding agent that keeps asking for approval.
// Answers change nothing; they only advance to the next task.
func runAgent(ctx context.Context, t *TTY, cfg Config) {
	ed := &term.Editor{Keys: t.keys, Out: t, Width: t.Width, Clear: t.ClearScreen, MaxLen: 64}
	t.ClearScreen()
	t.WriteString("\x1b[1mdemo-agent\x1b[0m: a pretend coding agent. Answer y or n; nothing is changed.\n")
	for n := 1; ctx.Err() == nil; n++ {
		task := agentTasks[(n-1)%len(agentTasks)]
		t.Printf("\n\x1b[1;36magent>\x1b[0m Task %d: %s\n", n, task.title)
		for _, d := range task.diff {
			color := "\x1b[33m"
			switch {
			case strings.HasPrefix(d, "+"):
				color = "\x1b[32m"
			case strings.HasPrefix(d, "-"):
				color = "\x1b[31m"
			}
			t.Printf("  %s%s\x1b[0m\n", color, d)
		}
	ask:
		for ctx.Err() == nil {
			answer, err := ed.ReadLine(ctx, "Apply this change? (y/n) ")
			switch {
			case errors.Is(err, term.ErrInterrupt):
				t.WriteString("skipped.\n")
				break ask
			case errors.Is(err, io.EOF):
				continue
			case err != nil:
				return
			}
			switch strings.ToLower(strings.TrimSpace(answer)) {
			case "y", "yes":
				t.WriteString("\x1b[32mapplied.\x1b[0m\n")
				break ask
			case "n", "no":
				t.WriteString("skipped.\n")
				break ask
			default:
				t.WriteString("please answer y or n\n")
			}
		}
		if !t.Sleep(ctx, 1500*time.Millisecond) && ctx.Err() != nil {
			return
		}
	}
}

var logLines = []func(n int) string{
	func(int) string { return "deploy: health check ok (3/3 replicas)" },
	func(n int) string { return fmt.Sprintf("api: request p95 %dms, errors 0", 38+n%9) },
	func(n int) string { return fmt.Sprintf("worker: processed %d jobs", 120+n%37) },
	func(int) string { return "nginx: 200 GET /healthz" },
	func(n int) string { return fmt.Sprintf("db: checkpoint complete: wrote %d buffers", 400+n%211) },
}

// runLogs follows a pretend deploy log. Input is ignored.
func runLogs(ctx context.Context, t *TTY, cfg Config) {
	t.ClearScreen()
	t.WriteString("\x1b[2m-- following /var/log/demo-app/deploy.log (simulated, read-only) --\x1b[0m\n")
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for n := 0; ; {
		select {
		case <-ctx.Done():
			return
		case k := <-t.keys:
			if k == term.KeyCtrlC {
				t.WriteString("^C\n\x1b[2m-- this log is read-only and keeps following --\x1b[0m\n")
			}
		case <-tick.C:
			t.Printf("%s %s\n", cfg.Now().UTC().Format("2006-01-02T15:04:05Z"), logLines[n%len(logLines)](n))
			n++
		}
	}
}
