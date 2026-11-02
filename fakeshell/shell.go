package fakeshell

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/smart-bit-me/scomp-agent/fakeshell/term"
	"github.com/smart-bit-me/scomp-agent/fakeshell/vfs"
)

// The simulated account.
const (
	shellUser = "review"
	shellHome = "/home/review"
	shellPath = "/home/review/.local/bin:/usr/local/bin:/usr/bin"
)

//go:embed all:honeyfs
var honeyfs embed.FS

// Files that cannot live in honeyfs: a go.mod or .go file there would be
// treated as Go source by the toolchain.
var staticFiles = map[string]string{
	"/home/review/app/go.mod": "module example.com/demo-app\n\ngo 1.27\n",
	"/home/review/app/main.go": `package main

import (
	"fmt"
	"log"
	"net/http"
)

func main() {
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	})
	log.Fatal(http.ListenAndServe(":8080", nil))
}
`,
}

// shell is one simulated interactive bash session.
type shell struct {
	ctx     context.Context
	t       *TTY
	cfg     Config
	fs      *vfs.FS
	procs   *procTable
	ed      *term.Editor
	cwd     string
	oldpwd  string
	env     map[string]string
	status  int
	dirty   bool // input since the last reset
	started time.Time
}

func runShell(ctx context.Context, t *TTY, cfg Config) {
	sh := newShell(ctx, t, cfg)
	sh.login()
	for ctx.Err() == nil {
		if !t.AtLineStart() {
			t.WriteString("\n")
		}
		line, err := sh.ed.ReadLine(ctx, sh.prompt())
		switch {
		case err == nil:
			sh.dirty = true
			sh.run(line)
		case errors.Is(err, term.ErrInterrupt):
			sh.dirty = true
			sh.status = 130
		case errors.Is(err, term.ErrIdle):
			if sh.dirty {
				sh.reset()
				sh.login()
			}
		case errors.Is(err, io.EOF):
			sh.logout()
		default:
			return
		}
	}
}

func newShell(ctx context.Context, t *TTY, cfg Config) *shell {
	now := cfg.Now()
	sh := &shell{ctx: ctx, t: t, cfg: cfg, started: now}
	sh.procs = newProcTable(now, cfg.Seed)
	base, err := fs.Sub(honeyfs, "honeyfs")
	if err != nil {
		panic(err) // the embedded tree is fixed at build time
	}
	static := func(s string) func() []byte { return func() []byte { return []byte(s) } }
	sh.fs = vfs.New(vfs.Config{
		Base:      base,
		Static:    staticFiles,
		ExtraDirs: []string{"/tmp", "/var/tmp", "/opt", "/mnt", "/home/review/.cache"},
		Generated: map[string]func() []byte{
			"/etc/hostname": static(cfg.Hostname + "\n"),
			"/etc/hosts":    static("127.0.0.1   localhost localhost.localdomain\n::1         localhost localhost.localdomain\n10.0.0.15   " + cfg.Hostname + "\n"),
			"/etc/motd":     func() []byte { return []byte(stripANSI(banner(cfg.Hostname, 80))) },
			"/proc/version": static("Linux version " + kernelRelease + " (mockbuild@fedoraproject.org) #1 SMP PREEMPT_DYNAMIC\n"),
			"/proc/uptime": func() []byte {
				up := sh.procs.uptime(sh.cfg.Now()).Seconds()
				return []byte(fmt.Sprintf("%.2f %.2f\n", up, up*1.83))
			},
			"/proc/loadavg": func() []byte {
				l := sh.procs.load
				return []byte(fmt.Sprintf("%.2f %.2f %.2f 1/%d %d\n", l[0], l[1], l[2], len(sh.procs.procs)+80, sh.procs.nextPID))
			},
			"/proc/meminfo": func() []byte {
				used := sh.procs.memUsedKiB()
				free := memTotalKiB - used - memCacheKiB
				return []byte(fmt.Sprintf("MemTotal:       %8d kB\nMemFree:        %8d kB\nMemAvailable:   %8d kB\nCached:         %8d kB\nSwapTotal:      %8d kB\nSwapFree:       %8d kB\n",
					memTotalKiB, free, free+memCacheKiB*8/10, memCacheKiB, swapTotalKiB, swapTotalKiB))
			},
		},
		User:     shellUser,
		Home:     shellHome,
		Writable: []string{shellHome, "/tmp", "/var/tmp"},
		Private:  []string{"/root", "/etc/shadow"},
		BaseTime: now.Add(-36 * time.Hour).Truncate(time.Minute),
		Now:      cfg.Now,
	})
	sh.ed = &term.Editor{
		Keys:     t.keys,
		Out:      t,
		Width:    t.Width,
		Clear:    t.ClearScreen,
		Idle:     cfg.IdleReset,
		Complete: sh.complete,
	}
	sh.reset()
	return sh
}

// reset returns the session to a fresh login state.
func (sh *shell) reset() {
	sh.fs.Reset()
	sh.ed.ResetHistory()
	sh.cwd, sh.oldpwd = shellHome, ""
	sh.status = 0
	sh.dirty = false
	sh.env = map[string]string{
		"HOME":     shellHome,
		"USER":     shellUser,
		"LOGNAME":  shellUser,
		"SHELL":    "/bin/bash",
		"TERM":     "xterm-256color",
		"LANG":     "C.UTF-8",
		"PATH":     shellPath,
		"HOSTNAME": sh.cfg.Hostname,
		"EDITOR":   "vi",
	}
}

func (sh *shell) login() {
	sh.t.ClearScreen()
	sh.t.WriteString(banner(sh.cfg.Hostname, sh.t.Width()))
}

func (sh *shell) logout() {
	sh.t.WriteString("logout\n")
	sh.reset()
	if !sh.t.Sleep(sh.ctx, 800*time.Millisecond) && sh.ctx.Err() != nil {
		return
	}
	sh.login()
}

func (sh *shell) prompt() string {
	dir := sh.cwd
	switch {
	case dir == shellHome:
		dir = "~"
	case strings.HasPrefix(dir, shellHome+"/"):
		dir = "~" + dir[len(shellHome):]
	}
	return fmt.Sprintf("\x1b[1;32m%s@%s\x1b[0m:\x1b[1;34m%s\x1b[0m$ ", shellUser, sh.cfg.Hostname, dir)
}

// banner is the login message. Its command list is generated from the
// command table, so it always matches what the shell accepts.
func banner(hostname string, width int) string {
	var b strings.Builder
	welcome := strings.TrimSuffix(wrapText("Welcome to "+hostname+", a Scomp demo machine.", width), "\n")
	b.WriteString("\x1b[1m" + welcome + "\x1b[0m\n\n")
	b.WriteString(wrapText("This is a simulated shell: nothing you type is executed and no file here is real, so feel free to explore.", width))
	b.WriteString("\n\x1b[1mAvailable commands:\x1b[0m\n")
	b.WriteString(columns(visibleCommandNames(), width, "  "))
	b.WriteString("\n")
	b.WriteString(wrapText("Try: top, ps aux, ls -la, cat README.md, build, git status, date. Type help for details. Other sessions on this host: agent (answer y/n) and logs.", width))
	b.WriteString("\n")
	return b.String()
}

// wrapText wraps s at width on spaces.
func wrapText(s string, width int) string {
	if width < 20 {
		width = 20
	}
	var b strings.Builder
	col := 0
	for _, w := range strings.Fields(s) {
		if col > 0 && col+1+len(w) > width {
			b.WriteString("\n")
			col = 0
		} else if col > 0 {
			b.WriteString(" ")
			col++
		}
		b.WriteString(w)
		col += len(w)
	}
	b.WriteString("\n")
	return b.String()
}

// columns lays names out column-major like ls, each row prefixed by indent.
func columns(names []string, width int, indent string) string {
	if len(names) == 0 {
		return ""
	}
	cell := 0
	for _, n := range names {
		cell = max(cell, term.VisibleLen(n))
	}
	cell += 2
	cols := max(1, (width-len(indent)+2)/cell)
	rows := (len(names) + cols - 1) / cols
	var b strings.Builder
	for r := 0; r < rows; r++ {
		b.WriteString(indent)
		for c := 0; c < cols; c++ {
			i := c*rows + r
			if i >= len(names) {
				break
			}
			b.WriteString(names[i])
			if c < cols-1 && i+rows < len(names) {
				b.WriteString(strings.Repeat(" ", cell-term.VisibleLen(names[i])))
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

func stripANSI(s string) string {
	var b strings.Builder
	inEsc, inCSI := false, false
	for _, r := range s {
		switch {
		case inCSI:
			inCSI = !(r >= 0x40 && r <= 0x7e)
		case inEsc:
			inEsc, inCSI = false, r == '['
		case r == 0x1b:
			inEsc = true
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Word splitting errors.
var (
	errUnsupported  = errors.New("unsupported shell syntax")
	errUnterminated = errors.New("unterminated quoted string")
)

type word struct {
	text string
	glob bool // has an unquoted * or ?
}

// split performs a small subset of bash word splitting: quotes, backslash
// escapes, $NAME / ${NAME} / $? expansion, a leading ~, and # comments.
// Pipes, redirection, command chaining and substitution are refused rather
// than half-interpreted.
func (sh *shell) split(line string) ([]word, error) {
	var (
		words   []word
		cur     strings.Builder
		inWord  bool
		glob    bool
		quote   rune // 0, '\'' or '"'
		runes   = []rune(line)
		closeUp = func() {
			if inWord {
				words = append(words, word{text: cur.String(), glob: glob})
			}
			cur.Reset()
			inWord, glob = false, false
		}
	)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch quote {
		case '\'':
			if r == '\'' {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
			continue
		case '"':
			switch {
			case r == '"':
				quote = 0
			case r == '\\' && i+1 < len(runes) && strings.ContainsRune("\"\\$`", runes[i+1]):
				i++
				cur.WriteRune(runes[i])
			case r == '`':
				return nil, errUnsupported
			case r == '$':
				n, err := sh.expand(runes, i, &cur)
				if err != nil {
					return nil, err
				}
				i = n
			default:
				cur.WriteRune(r)
			}
			continue
		}
		switch {
		case r == ' ' || r == '\t':
			closeUp()
		case r == '#' && !inWord:
			closeUp()
			return words, nil
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case r == '\\':
			inWord = true
			if i+1 < len(runes) {
				i++
				cur.WriteRune(runes[i])
			}
		case strings.ContainsRune("|;&<>`()", r):
			return nil, errUnsupported
		case r == '$':
			inWord = true
			n, err := sh.expand(runes, i, &cur)
			if err != nil {
				return nil, err
			}
			i = n
		case r == '~' && !inWord && (i+1 == len(runes) || runes[i+1] == '/' || runes[i+1] == ' '):
			inWord = true
			cur.WriteString(sh.env["HOME"])
		default:
			if r == '*' || r == '?' || r == '[' {
				glob = true
			}
			inWord = true
			cur.WriteRune(r)
		}
	}
	if quote != 0 {
		return nil, errUnterminated
	}
	closeUp()
	return words, nil
}

// expand handles a $ at runes[i] and returns the index of its last rune.
func (sh *shell) expand(runes []rune, i int, out *strings.Builder) (int, error) {
	if i+1 >= len(runes) {
		out.WriteRune('$')
		return i, nil
	}
	next := runes[i+1]
	switch {
	case next == '(':
		return 0, errUnsupported
	case next == '?':
		out.WriteString(strconv.Itoa(sh.status))
		return i + 1, nil
	case next == '$':
		out.WriteString("2214")
		return i + 1, nil
	case next == '{':
		end := i + 2
		for end < len(runes) && runes[end] != '}' {
			end++
		}
		if end >= len(runes) {
			return 0, errUnterminated
		}
		out.WriteString(sh.env[string(runes[i+2:end])])
		return end, nil
	case next == '_' || isAlpha(next):
		end := i + 1
		for end < len(runes) && (runes[end] == '_' || isAlpha(runes[end]) || (runes[end] >= '0' && runes[end] <= '9')) {
			end++
		}
		out.WriteString(sh.env[string(runes[i+1:end])])
		return end - 1, nil
	default:
		out.WriteRune('$')
		return i, nil
	}
}

func isAlpha(r rune) bool { return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') }

// glob expands a pattern in its last path element, sorted; no match leaves
// the word unchanged, as in bash.
func (sh *shell) glob(pattern string) []string {
	dir, base := path.Split(pattern)
	lookup := sh.abs(dir)
	if dir == "" {
		lookup = sh.cwd
	}
	if _, err := path.Match(base, ""); err != nil {
		return []string{pattern}
	}
	entries, err := sh.fs.ReadDir(lookup)
	if err != nil {
		return []string{pattern}
	}
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name, ".") && !strings.HasPrefix(base, ".") {
			continue
		}
		if ok, _ := path.Match(base, e.Name); ok {
			out = append(out, dir+e.Name)
		}
	}
	if len(out) == 0 {
		return []string{pattern}
	}
	return out
}

func (sh *shell) abs(p string) string { return vfs.Resolve(sh.cwd, p) }

// run executes one input line.
func (sh *shell) run(line string) {
	words, err := sh.split(line)
	switch {
	case errors.Is(err, errUnsupported):
		sh.t.WriteString("bash: pipes, redirection, command chaining and substitution are not supported\non this demo machine. Run one command at a time; type help for the list.\n")
		sh.status = 2
		return
	case errors.Is(err, errUnterminated):
		sh.t.WriteString("bash: syntax error: unterminated quoted string\n")
		sh.status = 2
		return
	}
	var args []string
	for _, w := range words {
		if w.glob {
			args = append(args, sh.glob(w.text)...)
		} else {
			args = append(args, w.text)
		}
	}
	if len(args) == 0 {
		return
	}
	// NAME=value assignments without a command set shell variables.
	if assignments(args) {
		for _, a := range args {
			k, v, _ := strings.Cut(a, "=")
			sh.env[k] = v
		}
		sh.status = 0
		return
	}
	name := args[0]
	if strings.Contains(name, "/") {
		sh.status = sh.runPath(name)
		return
	}
	cmd, ok := lookupCommand(name)
	if !ok {
		sh.t.Printf("bash: %s: command not found (type help for the commands of this demo machine)\n", name)
		sh.status = 127
		return
	}
	sh.status = cmd.run(sh, args)
}

func assignments(args []string) bool {
	for _, a := range args {
		k, _, ok := strings.Cut(a, "=")
		if !ok || k == "" || !(k[0] == '_' || isAlpha(rune(k[0]))) {
			return false
		}
	}
	return true
}

// runPath answers ./script and /usr/bin/x style invocations.
func (sh *shell) runPath(name string) int {
	p := sh.abs(name)
	st, err := sh.fs.Stat(p)
	switch {
	case err != nil:
		sh.t.Printf("bash: %s: No such file or directory\n", name)
		return 127
	case st.Dir:
		sh.t.Printf("bash: %s: Is a directory\n", name)
		return 126
	case st.Mode&0o111 == 0:
		sh.t.Printf("bash: %s: Permission denied\n", name)
		return 126
	default:
		sh.t.Printf("%s: scripts are shown, not run, on this demo machine. Try: cat %s\n", name, name)
		return 126
	}
}

// complete implements Tab: command names for the first word, paths after.
func (sh *shell) complete(line []rune, pos int) ([]rune, int, []string) {
	start := pos
	for start > 0 && line[start-1] != ' ' {
		start--
	}
	prefix := string(line[start:pos])
	type cand struct{ name, insert string }
	var cands []cand
	if strings.TrimSpace(string(line[:start])) == "" && !strings.Contains(prefix, "/") {
		for _, n := range visibleCommandNames() {
			if strings.HasPrefix(n, prefix) {
				cands = append(cands, cand{n, n + " "})
			}
		}
	} else {
		dir, base := path.Split(prefix)
		lookup := sh.cwd
		if dir != "" {
			expanded := dir
			if strings.HasPrefix(expanded, "~") {
				expanded = shellHome + expanded[1:]
			}
			lookup = sh.abs(expanded)
		}
		entries, _ := sh.fs.ReadDir(lookup)
		for _, e := range entries {
			if !strings.HasPrefix(e.Name, base) || (strings.HasPrefix(e.Name, ".") && !strings.HasPrefix(base, ".")) {
				continue
			}
			suffix := " "
			name := e.Name
			if e.Dir {
				suffix, name = "/", name+"/"
			}
			cands = append(cands, cand{name, dir + e.Name + suffix})
		}
	}
	switch len(cands) {
	case 0:
		return line, pos, nil
	case 1:
		return splice(line, start, pos, cands[0].insert)
	}
	common := cands[0].insert
	names := make([]string, len(cands))
	for i, c := range cands {
		names[i] = c.name
		for !strings.HasPrefix(c.insert, common) {
			common = common[:len(common)-1]
		}
	}
	if len(common) > len(prefix) {
		return splice(line, start, pos, common)
	}
	sort.Strings(names)
	return line, pos, names
}

func splice(line []rune, start, pos int, insert string) ([]rune, int, []string) {
	ins := []rune(insert)
	out := append(append(append([]rune(nil), line[:start]...), ins...), line[pos:]...)
	return out, start + len(ins), nil
}
