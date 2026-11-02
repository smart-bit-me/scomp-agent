package fakeshell

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/smart-bit-me/scomp-agent/fakeshell/term"
)

// kernelThreads pads the task count: a real machine has many kernel threads
// that ps aux would list but that only clutter a demo.
const kernelThreads = 86

func truncate(s string, width int) string {
	if width > 0 && len(s) > width {
		return s[:width]
	}
	return s
}

func clockDuration(d time.Duration) string { // ps TIME: M:SS
	s := int(d.Seconds())
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func hmsDuration(d time.Duration) string { // ps -ef TIME: HH:MM:SS
	s := int(d.Seconds())
	return fmt.Sprintf("%02d:%02d:%02d", s/3600, s/60%60, s%60)
}

func topDuration(d time.Duration) string { // top TIME+: M:SS.hh
	cs := int(d / (10 * time.Millisecond))
	return fmt.Sprintf("%d:%02d.%02d", cs/6000, cs/100%60, cs%100)
}

func memPct(rss int) float64 { return float64(rss) * 100 / memTotalKiB }

func cmdPs(sh *shell, args []string) int {
	now := sh.cfg.Now()
	procs := sh.procs.sample(now)
	self := sh.procs.transient(now, "ps", strings.Join(args, " "), 0)
	width := sh.t.Width()
	style := "default"
	for _, a := range args[1:] {
		switch strings.TrimPrefix(a, "-") {
		case "aux", "axu", "au", "ux", "ax", "a", "x", "u":
			style = "aux"
		case "ef", "fe", "eF", "ely":
			style = "ef"
		case "e", "A":
			style = "e"
		case "f":
			if style == "e" {
				style = "ef"
			}
		default:
			sh.t.Printf("ps: this demo supports ps, ps aux, ps -e and ps -ef\n")
			return 1
		}
	}
	switch style {
	case "default":
		sh.t.WriteString("    PID TTY          TIME CMD\n")
		for _, p := range procs {
			if p.TTY == "pts/0" {
				sh.t.Printf("%7d %-8s %8s %s\n", p.PID, p.TTY, hmsDuration(p.CPUTime), p.Comm)
			}
		}
		sh.t.Printf("%7d %-8s %8s %s\n", self.PID, self.TTY, "00:00:00", "ps")
	case "e":
		sh.t.WriteString("    PID TTY          TIME CMD\n")
		for _, p := range append(procs, self) {
			sh.t.Printf("%7d %-8s %8s %s\n", p.PID, p.TTY, hmsDuration(p.CPUTime), p.Comm)
		}
	case "aux":
		sh.t.WriteString(truncate("USER         PID %CPU %MEM    VSZ   RSS TTY      STAT START   TIME COMMAND", width) + "\n")
		for _, p := range append(procs, self) {
			life := now.Sub(p.Start)
			avg := 0.0
			if life > 0 {
				avg = float64(p.CPUTime) * 100 / float64(life)
			}
			start := p.Start.Format("15:04")
			if now.Sub(p.Start) > 24*time.Hour {
				start = p.Start.Format("Jan02")
			}
			line := fmt.Sprintf("%-8s %7d %4.1f %4.1f %6d %5d %-8s %-4s %5s %6s %s",
				p.User, p.PID, avg, memPct(p.RSS), p.VSZ, p.RSS, p.TTY, p.Stat, start, clockDuration(p.CPUTime), p.Cmd)
			sh.t.WriteString(truncate(line, width) + "\n")
		}
	case "ef":
		sh.t.WriteString(truncate("UID          PID    PPID  C STIME TTY          TIME CMD", width) + "\n")
		for _, p := range append(procs, self) {
			stime := p.Start.Format("15:04")
			if now.Sub(p.Start) > 24*time.Hour {
				stime = p.Start.Format("Jan02")
			}
			line := fmt.Sprintf("%-8s %7d %7d %2d %5s %-8s %8s %s",
				p.User, p.PID, p.PPID, int(p.CPU), stime, p.TTY, hmsDuration(p.CPUTime), p.Cmd)
			sh.t.WriteString(truncate(line, width) + "\n")
		}
	}
	return 0
}

// topFrame renders one top screen of at most rows lines, each cut to width.
func (sh *shell) topFrame(now time.Time, procs []proc, width, rows int) []string {
	t := sh.procs
	total := 0.0
	for _, p := range procs {
		total += p.CPU
	}
	us := min(total/numCPU*0.72, 99)
	sy := min(total/numCPU*0.28, 100-us)
	id := max(100-us-sy-0.2, 0)
	used := t.memUsedKiB()
	free := memTotalKiB - used - memCacheKiB
	mib := func(kib int) float64 { return float64(kib) / 1024 }
	tasks := len(procs) + kernelThreads
	l := t.load
	lines := []string{
		fmt.Sprintf("top - %s up %s,  1 user,  load average: %.2f, %.2f, %.2f", now.Format("15:04:05"), formatUptime(t.uptime(now)), l[0], l[1], l[2]),
		fmt.Sprintf("Tasks: %3d total,   1 running, %3d sleeping,   0 stopped,   0 zombie", tasks, tasks-1),
		fmt.Sprintf("%%Cpu(s): %4.1f us, %4.1f sy,  0.0 ni, %4.1f id,  0.1 wa,  0.0 hi,  0.1 si,  0.0 st", us, sy, id),
		fmt.Sprintf("MiB Mem : %8.1f total, %8.1f free, %8.1f used, %8.1f buff/cache", mib(memTotalKiB), mib(free), mib(used), mib(memCacheKiB)),
		fmt.Sprintf("MiB Swap: %8.1f total, %8.1f free, %8.1f used. %8.1f avail Mem", mib(swapTotalKiB), mib(swapTotalKiB), 0.0, mib(free+memCacheKiB*8/10)),
		"",
	}
	for i := range lines {
		lines[i] = truncate(lines[i], width)
	}
	header := truncate(fmt.Sprintf("%-*s", width, "    PID USER      PR  NI    VIRT    RES    SHR S  %CPU  %MEM     TIME+ COMMAND"), width)
	lines = append(lines, "\x1b[7m"+header+"\x1b[0m")
	sorted := append([]proc(nil), procs...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].CPU != sorted[j].CPU {
			return sorted[i].CPU > sorted[j].CPU
		}
		return sorted[i].PID < sorted[j].PID
	})
	for _, p := range sorted {
		if len(lines) >= rows {
			break
		}
		state := p.Stat[:1]
		line := fmt.Sprintf("%7d %-8s %3s %3d %7d %6d %6d %s %5.1f %5.1f %9s %s",
			p.PID, truncate(p.User, 8), "20", 0, p.VSZ, p.RSS, p.SHR, state, p.CPU, memPct(p.RSS), topDuration(p.CPUTime), p.Comm)
		if p.Comm == "top" {
			line = "\x1b[1m" + truncate(line, width) + "\x1b[0m"
		} else {
			line = truncate(line, width)
		}
		lines = append(lines, line)
	}
	return lines
}

func cmdTop(sh *shell, args []string) int {
	batch := false
	iterations := 0
	delay := 2 * time.Second
	for i := 1; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-b":
			batch = true
		case a == "-n" && i+1 < len(args):
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil || n < 1 {
				sh.t.Printf("top: bad iterations argument '%s'\n", args[i])
				return 1
			}
			iterations = n
		case a == "-d" && i+1 < len(args):
			i++
			d, err := strconv.ParseFloat(args[i], 64)
			if err != nil || d <= 0 {
				sh.t.Printf("top: bad delay interval '%s'\n", args[i])
				return 1
			}
			delay = time.Duration(min(max(d, 0.5), 10) * float64(time.Second))
		default:
			sh.t.Printf("top: this demo supports -b, -n N and -d SECONDS\n")
			return 1
		}
	}
	self := sh.procs.transient(sh.cfg.Now(), "top", "top", 0.7)

	if batch {
		if iterations == 0 {
			iterations = 1
		}
		for i := 0; i < iterations; i++ {
			if i > 0 && !sh.t.Sleep(sh.ctx, delay) {
				return 130
			}
			now := sh.cfg.Now()
			self.CPU = 0.3 + 0.6*sh.procs.rng.Float64()
			procs := append(sh.procs.sample(now), self)
			width, _ := sh.t.Size()
			lines := sh.topFrame(now, procs, width, 1<<30)
			sh.t.WriteString(stripANSI(strings.Join(lines, "\n")) + "\n\n")
		}
		return 0
	}

	defer func() {
		sh.t.WriteString("\x1b[?25h")
		sh.t.ClearScreen()
	}()
	idle := time.NewTimer(sh.cfg.IdleReset)
	defer idle.Stop()
	for i := 0; iterations == 0 || i < iterations; i++ {
		now := sh.cfg.Now()
		self.CPU = 0.3 + 0.6*sh.procs.rng.Float64()
		self.CPUTime += 20 * time.Millisecond
		procs := append(sh.procs.sample(now), self)
		width, rows := sh.t.Size()
		lines := sh.topFrame(now, procs, width, rows)
		sh.t.Frame("\x1b[?25l\x1b[H" + strings.Join(lines, "\x1b[K\n") + "\x1b[K\x1b[J")
		if !sh.topWait(delay, idle) {
			return 0
		}
	}
	return 0
}

// topWait waits for the next refresh. It reports false when top should quit:
// q, Ctrl-C, the idle timeout, or the session ending.
func (sh *shell) topWait(delay time.Duration, idle *time.Timer) bool {
	tick := time.NewTimer(delay)
	defer tick.Stop()
	for {
		select {
		case <-sh.ctx.Done():
			return false
		case <-idle.C:
			return false
		case <-tick.C:
			return true
		case <-sh.t.resized:
			return true
		case k := <-sh.t.keys:
			idle.Reset(sh.cfg.IdleReset)
			switch k {
			case 'q', 'Q', term.KeyCtrlC:
				return false
			case ' ', term.KeyEnter:
				return true
			}
		}
	}
}
