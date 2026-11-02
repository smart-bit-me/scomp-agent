package fakeshell

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/smart-bit-me/scomp-agent/fakeshell/term"
)

var testNow = time.Date(2026, time.November, 2, 17, 42, 5, 0, time.UTC)

// capture is a TTY sink.
type capture struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (c *capture) emit(p []byte, reset bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if reset {
		c.buf.Reset()
	}
	c.buf.Write(p)
}

func (c *capture) take() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := stripANSI(c.buf.String())
	c.buf.Reset()
	return strings.ReplaceAll(s, "\r\n", "\n")
}

func newTestShell(t *testing.T, width int) (*shell, *capture, chan term.Key) {
	t.Helper()
	out := &capture{}
	keys := make(chan term.Key, 64)
	tty := &TTY{
		keys: keys, resized: make(chan struct{}, 1), emit: out.emit,
		size: func() (int, int) { return width, 24 }, now: func() time.Time { return testNow },
		atLineStart: true,
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	cfg := Config{Hostname: "review-host", Now: func() time.Time { return testNow }, Seed: 1}.withDefaults()
	return newShell(ctx, tty, cfg), out, keys
}

func run(sh *shell, out *capture, line string) string {
	out.take()
	sh.run(line)
	return out.take()
}

func TestBannerListsEveryVisibleCommand(t *testing.T) {
	for _, width := range []int{40, 80, 132} {
		b := stripANSI(banner("review-host", width))
		for _, c := range visibleCommands() {
			if !strings.Contains(b, c.name) {
				t.Errorf("width %d: banner does not list %q", width, c.name)
			}
		}
		for _, line := range strings.Split(b, "\n") {
			if len(line) > width {
				t.Errorf("width %d: banner line too long: %q", width, line)
			}
		}
	}
}

func TestEveryVisibleCommandHasHelp(t *testing.T) {
	for _, c := range visibleCommands() {
		if c.usage == "" || c.help == "" {
			t.Errorf("%s lacks usage or help text", c.name)
		}
	}
}

func TestDateIsLiveAndFormats(t *testing.T) {
	sh, out, _ := newTestShell(t, 80)
	cases := map[string]string{
		"date":                     "Mon Nov  2 17:42:05 UTC 2026\n",
		"date +%F":                 "2026-11-02\n",
		"date '+%H:%M:%S %j %s'":   "17:42:05 306 1793641325\n",
		"date -u -R":               "Mon, 02 Nov 2026 17:42:05 +0000\n",
		"date -Iseconds":           "2026-11-02T17:42:05+00:00\n",
		"date \"+%a %e %b %I %p\"": "Mon  2 Nov 05 PM\n",
	}
	for line, want := range cases {
		if got := run(sh, out, line); got != want {
			t.Errorf("%s = %q, want %q", line, got, want)
		}
	}
	later := testNow.Add(90 * time.Second)
	sh.cfg.Now = func() time.Time { return later }
	if got := run(sh, out, "date +%T"); got != "17:43:35\n" {
		t.Errorf("date did not follow the clock: %q", got)
	}
	if got := run(sh, out, "date -s now"); !strings.Contains(got, "Operation not permitted") {
		t.Errorf("date -s = %q", got)
	}
}

func TestLsAndCat(t *testing.T) {
	sh, out, _ := newTestShell(t, 80)
	ls := run(sh, out, "ls")
	for _, want := range []string{"README.md", "app", "deploy.sh", "notes.txt"} {
		if !strings.Contains(ls, want) {
			t.Errorf("ls missing %s: %q", want, ls)
		}
	}
	if strings.Contains(ls, ".bashrc") {
		t.Errorf("ls shows dotfiles: %q", ls)
	}
	long := run(sh, out, "ls -la")
	for _, want := range []string{"total ", ".bashrc", "drwxr-xr-x", "-rwxr-xr-x", "review review", " ..\n"} {
		if !strings.Contains(long, want) {
			t.Errorf("ls -la missing %q: %q", want, long)
		}
	}
	if got := run(sh, out, "cat app/main.go"); !strings.Contains(got, "/healthz") {
		t.Errorf("cat app/main.go = %q", got)
	}
	if got := run(sh, out, "ls app"); !strings.Contains(got, "go.mod") || !strings.Contains(got, "Makefile") {
		t.Errorf("ls app = %q", got)
	}
	if got := run(sh, out, "cat nope"); got != "cat: nope: No such file or directory\n" {
		t.Errorf("cat nope = %q", got)
	}
	if got := run(sh, out, "ls *.md"); got != "README.md\n" {
		t.Errorf("glob = %q", got)
	}
}

func TestPathsStayInsideTheFakeTree(t *testing.T) {
	sh, out, _ := newTestShell(t, 80)
	if got := run(sh, out, "cat ../../../../../../etc/hostname"); got != "review-host\n" {
		t.Errorf("escaped path = %q", got)
	}
	run(sh, out, "cd ../../../../..")
	if sh.cwd != "/" {
		t.Errorf("cwd = %q, want /", sh.cwd)
	}
	if got := run(sh, out, "cat /etc/shadow"); !strings.Contains(got, "Permission denied") {
		t.Errorf("cat /etc/shadow = %q", got)
	}
	if got := run(sh, out, "cd /root"); !strings.Contains(got, "Permission denied") {
		t.Errorf("cd /root = %q", got)
	}
	if got := run(sh, out, "cat /etc/passwd"); !strings.Contains(got, "review:x:1000") {
		t.Errorf("cat /etc/passwd = %q", got)
	}
}

func TestShellSyntaxIsRefusedNotRun(t *testing.T) {
	sh, out, _ := newTestShell(t, 80)
	for _, line := range []string{
		"ls | sh", "cat x; rm -rf /", "echo hi > /etc/passwd", "echo $(id)", "echo `id`",
		"curl x && sh", "true || false", "sleep 1 &", "(id)",
	} {
		got := run(sh, out, line)
		if !strings.Contains(got, "not supported") || sh.status != 2 {
			t.Errorf("%q: status %d, output %q", line, sh.status, got)
		}
	}
	if got := run(sh, out, "echo 'a | b; c'"); got != "a | b; c\n" {
		t.Errorf("quoted metacharacters = %q", got)
	}
}

func TestUnknownAndUnavailableCommands(t *testing.T) {
	sh, out, _ := newTestShell(t, 80)
	if got := run(sh, out, "frobnicate"); !strings.Contains(got, "command not found") || sh.status != 127 {
		t.Errorf("unknown: %d %q", sh.status, got)
	}
	if got := run(sh, out, "curl https://example.com"); !strings.Contains(got, "no network access") {
		t.Errorf("curl = %q", got)
	}
	if got := run(sh, out, "sudo id"); !strings.Contains(got, "sudoers") {
		t.Errorf("sudo = %q", got)
	}
	if got := run(sh, out, "./deploy.sh"); !strings.Contains(got, "shown, not run") {
		t.Errorf("./deploy.sh = %q", got)
	}
}

func TestEchoExpansion(t *testing.T) {
	sh, out, _ := newTestShell(t, 80)
	cases := map[string]string{
		"echo $HOME":        "/home/review\n",
		"echo '$HOME'":      "$HOME\n",
		"echo \"$USER@x\"":  "review@x\n",
		"echo ~/app":        "/home/review/app\n",
		"echo -n hi":        "hi",
		"echo a\\ b   c":    "a b c\n",
		"echo ${HOSTNAME}":  "review-host\n",
		"echo # a comment":  "\n",
		"echo $UNSET_THING": "\n",
	}
	for line, want := range cases {
		if got := run(sh, out, line); got != want {
			t.Errorf("%s = %q, want %q", line, got, want)
		}
	}
	run(sh, out, "false")
	if got := run(sh, out, "echo $?"); got != "1\n" {
		t.Errorf("echo $? = %q", got)
	}
	run(sh, out, "GREETING=hello")
	if got := run(sh, out, "echo $GREETING"); got != "hello\n" {
		t.Errorf("assignment = %q", got)
	}
	if got := run(sh, out, "echo \"open"); !strings.Contains(got, "unterminated") {
		t.Errorf("unterminated = %q", got)
	}
}

func TestOverlayWritesAreBoundedAndReset(t *testing.T) {
	sh, out, _ := newTestShell(t, 80)
	run(sh, out, "mkdir -p work/a")
	run(sh, out, "touch work/a/file.txt")
	if got := run(sh, out, "ls work/a"); got != "file.txt\n" {
		t.Errorf("ls work/a = %q", got)
	}
	if got := run(sh, out, "touch /etc/evil"); !strings.Contains(got, "Permission denied") {
		t.Errorf("touch /etc/evil = %q", got)
	}
	if got := run(sh, out, "rm README.md"); !strings.Contains(got, "Permission denied") {
		t.Errorf("rm README.md = %q", got)
	}
	run(sh, out, "rm -r work")
	if got := run(sh, out, "ls work"); !strings.Contains(got, "No such file") {
		t.Errorf("after rm -r: %q", got)
	}
	run(sh, out, "touch keep")
	sh.reset()
	if got := run(sh, out, "ls keep"); !strings.Contains(got, "No such file") {
		t.Errorf("reset kept the overlay: %q", got)
	}
	for i := 0; i < 200; i++ {
		sh.run("touch f" + strings.Repeat("x", i%5) + string(rune('a'+i%26)) + string(rune('a'+i/26)))
	}
	if got := run(sh, out, "touch one-too-many"); !strings.Contains(got, "No space left") {
		t.Errorf("overlay is not bounded: %q", got)
	}
}

func TestCdAndPrompt(t *testing.T) {
	sh, out, _ := newTestShell(t, 80)
	run(sh, out, "cd app")
	if p := stripANSI(sh.prompt()); p != "review@review-host:~/app$ " {
		t.Errorf("prompt = %q", p)
	}
	if got := run(sh, out, "cd -"); got != "/home/review\n" {
		t.Errorf("cd - = %q", got)
	}
	if got := run(sh, out, "cd README.md"); !strings.Contains(got, "Not a directory") {
		t.Errorf("cd file = %q", got)
	}
	run(sh, out, "cd /var/log")
	if p := stripANSI(sh.prompt()); p != "review@review-host:/var/log$ " {
		t.Errorf("prompt = %q", p)
	}
}

func TestPs(t *testing.T) {
	sh, out, _ := newTestShell(t, 200)
	got := run(sh, out, "ps")
	if !strings.Contains(got, "PID TTY") || !strings.Contains(got, "bash") || !strings.Contains(got, " ps\n") {
		t.Errorf("ps = %q", got)
	}
	aux := run(sh, out, "ps aux")
	for _, want := range []string{"USER", "%CPU", "/usr/lib/systemd/systemd", "postgres: walwriter", "nginx: worker process", "ps aux"} {
		if !strings.Contains(aux, want) {
			t.Errorf("ps aux missing %q", want)
		}
	}
	ef := run(sh, out, "ps -ef")
	if !strings.Contains(ef, "UID          PID    PPID") || !strings.Contains(ef, "sshd") {
		t.Errorf("ps -ef = %q", ef)
	}
	narrow, nout, _ := newTestShell(t, 40)
	for _, line := range strings.Split(run(narrow, nout, "ps aux"), "\n") {
		if len(line) > 40 {
			t.Errorf("ps aux line wider than the terminal: %q", line)
		}
	}
}

func TestTopBatch(t *testing.T) {
	sh, out, _ := newTestShell(t, 100)
	got := run(sh, out, "top -b -n 1")
	for _, want := range []string{"top - 17:42:05 up 3 days,  4:12", "load average:", "Tasks:", "%Cpu(s):", "MiB Mem :", "PID USER", "node", "top"} {
		if !strings.Contains(got, want) {
			t.Errorf("top -b missing %q in %q", want, got)
		}
	}
}

func TestTopInteractiveQuitsOnQ(t *testing.T) {
	sh, out, keys := newTestShell(t, 80)
	done := make(chan int)
	go func() { done <- cmdTop(sh, []string{"top"}) }()
	waitFor(t, out, "load average")
	keys <- 'q'
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("top did not quit on q")
	}
	if !sh.t.AtLineStart() {
		t.Error("top did not leave a clean screen")
	}
}

func TestTopFrameFitsTheWindow(t *testing.T) {
	sh, _, _ := newTestShell(t, 50)
	procs := sh.procs.sample(testNow)
	lines := sh.topFrame(testNow, procs, 50, 12)
	if len(lines) != 12 {
		t.Errorf("frame has %d lines, want 12", len(lines))
	}
	for _, l := range lines {
		if n := term.VisibleLen(l); n > 50 {
			t.Errorf("line of %d columns: %q", n, l)
		}
	}
}

func TestUptimeAndUname(t *testing.T) {
	sh, out, _ := newTestShell(t, 120)
	if got := run(sh, out, "uptime"); !strings.HasPrefix(got, " 17:42:05 up 3 days,  4:12,  1 user,  load average: ") {
		t.Errorf("uptime = %q", got)
	}
	if got := run(sh, out, "uptime -p"); got != "up 3 days, 4 hours, 12 minutes\n" {
		t.Errorf("uptime -p = %q", got)
	}
	if got := run(sh, out, "uname -a"); !strings.HasPrefix(got, "Linux review-host 6.17") {
		t.Errorf("uname -a = %q", got)
	}
	if got := run(sh, out, "hostname"); got != "review-host\n" {
		t.Errorf("hostname = %q", got)
	}
	if got := run(sh, out, "cat /proc/uptime"); !strings.HasPrefix(got, "274320.00 ") {
		t.Errorf("/proc/uptime = %q", got)
	}
}

func TestMiscCommands(t *testing.T) {
	sh, out, _ := newTestShell(t, 100)
	for line, want := range map[string]string{
		"whoami":               "review\n",
		"pwd":                  "/home/review\n",
		"head -n 1 README.md":  "# demo app\n",
		"tail -1 notes.txt":    "- answer the review agent in the \"agent\" session (y/n)\n",
		"grep -n Go notes.txt": "2:- bump the Go toolchain\n",
	} {
		if got := run(sh, out, line); got != want {
			t.Errorf("%s = %q, want %q", line, got, want)
		}
	}
	for line, want := range map[string]string{
		"free -h":    "Mem:",
		"df -h":      "/dev/vda3",
		"git status": "On branch main",
		"git log":    "api: add /healthz endpoint",
		"help":       "live process monitor",
		"env":        "HOME=/home/review",
	} {
		if got := run(sh, out, line); !strings.Contains(got, want) {
			t.Errorf("%s missing %q: %q", line, want, got)
		}
	}
	run(sh, out, "cd /")
	if got := run(sh, out, "git status"); !strings.Contains(got, "not a git repository") {
		t.Errorf("git outside the repo = %q", got)
	}
}

func TestCompletion(t *testing.T) {
	sh, _, _ := newTestShell(t, 80)
	cases := []struct{ in, want string }{
		{"ca", "cat "},
		{"cat REA", "cat README.md "},
		{"ls a", "ls app/"},
		{"cat app/ma", "cat app/main.go "},
		{"cd ~/ap", "cd ~/app/"},
		{"zz", "zz"},
	}
	for _, c := range cases {
		line := []rune(c.in)
		got, pos, _ := sh.complete(line, len(line))
		if string(got) != c.want || pos != len([]rune(c.want)) {
			t.Errorf("complete(%q) = %q@%d, want %q", c.in, string(got), pos, c.want)
		}
	}
	line := []rune("cat ")
	_, _, choices := sh.complete(line, len(line))
	if len(choices) < 3 {
		t.Errorf("ambiguous completion choices = %v", choices)
	}
}

func waitFor(t *testing.T, out *capture, want string) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var seen strings.Builder
	for time.Now().Before(deadline) {
		seen.WriteString(out.take())
		if strings.Contains(seen.String(), want) {
			return seen.String()
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q; saw %q", want, seen.String())
	return ""
}
