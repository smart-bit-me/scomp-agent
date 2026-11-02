package fakeshell

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/smart-bit-me/scomp-agent/fakeshell/vfs"
)

// command is one entry of the shell's command table.
type command struct {
	name   string
	usage  string // shown by help
	help   string
	hidden bool // accepted, but not listed in the banner or help
	run    func(sh *shell, args []string) int
}

var commandTable map[string]command

func init() {
	cmds := []command{
		{name: "build", usage: "build", help: "run the demo app's build (Ctrl-C stops it)", run: cmdBuild},
		{name: "cat", usage: "cat FILE...", help: "print files", run: cmdCat},
		{name: "cd", usage: "cd [DIR]", help: "change directory (cd -, cd ~)", run: cmdCd},
		{name: "clear", usage: "clear", help: "clear the screen (or Ctrl-L)", run: cmdClear},
		{name: "date", usage: "date [-u] [-R|-I] [+FORMAT]", help: "current date and time", run: cmdDate},
		{name: "df", usage: "df [-h]", help: "disk usage", run: cmdDf},
		{name: "echo", usage: "echo [-n] TEXT", help: "print text; $HOME and $? expand", run: cmdEcho},
		{name: "env", usage: "env", help: "environment variables", run: cmdEnv},
		{name: "exit", usage: "exit", help: "log out (a new login starts)", run: cmdExit},
		{name: "free", usage: "free [-h|-m]", help: "memory usage", run: cmdFree},
		{name: "git", usage: "git status|log|diff|branch", help: "the demo project's history", run: cmdGit},
		{name: "grep", usage: "grep [-i] [-n] TEXT FILE...", help: "find lines containing TEXT", run: cmdGrep},
		{name: "head", usage: "head [-n N] FILE...", help: "first lines of files", run: cmdHead},
		{name: "help", usage: "help", help: "this list", run: cmdHelp},
		{name: "history", usage: "history [-c]", help: "commands typed in this session", run: cmdHistory},
		{name: "hostname", usage: "hostname", help: "the machine name", run: cmdHostname},
		{name: "id", usage: "id", help: "user and group ids", run: cmdID},
		{name: "ls", usage: "ls [-la] [PATH...]", help: "list directory contents", run: cmdLs},
		{name: "mkdir", usage: "mkdir [-p] DIR...", help: "create directories (in memory)", run: cmdMkdir},
		{name: "ps", usage: "ps [aux|-ef]", help: "running processes", run: cmdPs},
		{name: "pwd", usage: "pwd", help: "current directory", run: cmdPwd},
		{name: "rm", usage: "rm [-r] PATH...", help: "remove files you created", run: cmdRm},
		{name: "sleep", usage: "sleep SECONDS", help: "wait (at most 60 s; Ctrl-C stops)", run: cmdSleep},
		{name: "tail", usage: "tail [-n N] FILE...", help: "last lines of files", run: cmdTail},
		{name: "top", usage: "top [-b] [-n N] [-d SEC]", help: "live process monitor (q quits)", run: cmdTop},
		{name: "touch", usage: "touch FILE...", help: "create empty files (in memory)", run: cmdTouch},
		{name: "uname", usage: "uname [-a]", help: "system information", run: cmdUname},
		{name: "uptime", usage: "uptime [-p]", help: "time since boot and load", run: cmdUptime},
		{name: "whoami", usage: "whoami", help: "current user", run: cmdWhoami},

		{name: "logout", hidden: true, run: cmdExit},
		{name: "printenv", hidden: true, run: cmdEnv},
		{name: "export", hidden: true, run: cmdExport},
		{name: "unset", hidden: true, run: cmdUnset},
		{name: "rmdir", hidden: true, run: cmdRmdir},
		{name: "less", hidden: true, run: cmdCat},
		{name: "more", hidden: true, run: cmdCat},
		{name: "true", hidden: true, run: func(*shell, []string) int { return 0 }},
		{name: "false", hidden: true, run: func(*shell, []string) int { return 1 }},
		{name: "which", hidden: true, run: cmdWhich},
		{name: "type", hidden: true, run: cmdWhich},
		{name: "man", hidden: true, run: func(sh *shell, args []string) int {
			if len(args) < 2 {
				sh.t.WriteString("What manual page do you want?\nFor example, try 'man man'.\n")
				return 1
			}
			sh.t.Printf("No manual entry for %s. Type help for the commands of this demo machine.\n", args[1])
			return 16
		}},
		{name: "sudo", hidden: true, run: func(sh *shell, _ []string) int {
			sh.t.WriteString("review is not in the sudoers file.\n")
			return 1
		}},
		{name: "su", hidden: true, run: func(sh *shell, _ []string) int {
			sh.t.WriteString("su: Authentication failure\n")
			return 1
		}},
		{name: "kill", hidden: true, run: cmdKill},
		{name: "systemctl", hidden: true, run: cmdSystemctl},
	}
	for _, n := range []string{"vi", "vim", "nano", "emacs"} {
		cmds = append(cmds, command{name: n, hidden: true, run: notAvailable("editors are not available; try cat")})
	}
	for _, n := range []string{"curl", "wget", "ssh", "scp", "ping", "nc", "telnet", "ftp"} {
		cmds = append(cmds, command{name: n, hidden: true, run: notAvailable("this demo machine has no network access")})
	}
	for _, n := range []string{"bash", "sh", "zsh", "python", "python3", "node", "go", "make", "docker", "dnf", "apt", "yum"} {
		cmds = append(cmds, command{name: n, hidden: true, run: notAvailable("not available on this demo machine; type help for what is")})
	}
	commandTable = make(map[string]command, len(cmds))
	for _, c := range cmds {
		commandTable[c.name] = c
	}
}

func lookupCommand(name string) (command, bool) {
	c, ok := commandTable[name]
	return c, ok
}

func visibleCommands() []command {
	var out []command
	for _, c := range commandTable {
		if !c.hidden {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

func visibleCommandNames() []string {
	var names []string
	for _, c := range visibleCommands() {
		names = append(names, c.name)
	}
	return names
}

func notAvailable(msg string) func(*shell, []string) int {
	return func(sh *shell, args []string) int {
		sh.t.Printf("%s: %s\n", args[0], msg)
		return 127
	}
}

// fsErr formats an FS error the way coreutils does.
func fsErr(prog, name string, err error) string {
	return fmt.Sprintf("%s: %s: %v\n", prog, name, err)
}

func cmdHelp(sh *shell, _ []string) int {
	w := sh.t.Width()
	sh.t.WriteString("Commands of this demo machine (nothing is executed for real):\n\n")
	for _, c := range visibleCommands() {
		if w >= 60 {
			sh.t.Printf("  %-28s %s\n", c.usage, c.help)
		} else {
			sh.t.Printf("  %s\n      %s\n", c.usage, c.help)
		}
	}
	sh.t.WriteString("\nKeys: Up/Down history, Tab completion, Ctrl-C cancel, Ctrl-L clear.\n")
	return 0
}

func cmdPwd(sh *shell, _ []string) int {
	sh.t.WriteString(sh.cwd + "\n")
	return 0
}

func cmdCd(sh *shell, args []string) int {
	target := shellHome
	if len(args) > 2 {
		sh.t.WriteString("bash: cd: too many arguments\n")
		return 1
	}
	if len(args) == 2 {
		target = args[1]
	}
	if target == "-" {
		if sh.oldpwd == "" {
			sh.t.WriteString("bash: cd: OLDPWD not set\n")
			return 1
		}
		target = sh.oldpwd
		sh.t.WriteString(target + "\n")
	}
	p := sh.abs(target)
	st, err := sh.fs.Stat(p)
	if err != nil {
		sh.t.WriteString(fsErr("bash: cd", target, err))
		return 1
	}
	if !st.Dir {
		sh.t.WriteString(fsErr("bash: cd", target, vfs.ErrNotDir))
		return 1
	}
	if _, err := sh.fs.ReadDir(p); err != nil {
		sh.t.WriteString(fsErr("bash: cd", target, err))
		return 1
	}
	sh.oldpwd, sh.cwd = sh.cwd, p
	sh.env["PWD"], sh.env["OLDPWD"] = sh.cwd, sh.oldpwd
	return 0
}

func cmdLs(sh *shell, args []string) int {
	var long, all, almost bool
	var paths []string
	for _, a := range args[1:] {
		switch {
		case a == "--all":
			all = true
		case a == "--almost-all":
			almost = true
		case strings.HasPrefix(a, "--"):
		case strings.HasPrefix(a, "-") && len(a) > 1:
			for _, f := range a[1:] {
				switch f {
				case 'l':
					long = true
				case 'a':
					all = true
				case 'A':
					almost = true
				case 'h', '1', 'F', 'G', 'r', 't', 'S', 'd':
				default:
					sh.t.Printf("ls: invalid option -- '%c'\n", f)
					return 2
				}
			}
		default:
			paths = append(paths, a)
		}
	}
	if len(paths) == 0 {
		paths = []string{"."}
	}
	status := 0
	var files []vfs.Info
	var dirs []string
	for _, p := range paths {
		st, err := sh.fs.Stat(sh.abs(p))
		if err != nil {
			sh.t.Printf("ls: cannot access '%s': %v\n", p, err)
			status = 2
			continue
		}
		if st.Dir {
			dirs = append(dirs, p)
		} else {
			st.Name = p
			files = append(files, st)
		}
	}
	printed := false
	if len(files) > 0 {
		sh.lsPrint(files, long)
		printed = true
	}
	for _, d := range dirs {
		abs := sh.abs(d)
		entries, err := sh.fs.ReadDir(abs)
		if err != nil {
			sh.t.Printf("ls: cannot open directory '%s': %v\n", d, err)
			status = 2
			continue
		}
		var shown []vfs.Info
		if all {
			for _, special := range []struct{ name, p string }{{".", abs}, {"..", path.Dir(abs)}} {
				if st, err := sh.fs.Stat(special.p); err == nil {
					st.Name = special.name
					shown = append(shown, st)
				}
			}
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name, ".") && !all && !almost {
				continue
			}
			shown = append(shown, e)
		}
		if printed {
			sh.t.WriteString("\n")
		}
		if len(paths) > 1 {
			sh.t.Printf("%s:\n", d)
		}
		if long {
			blocks := int64(0)
			for _, e := range shown {
				blocks += (e.Size + 4095) / 4096 * 4
			}
			sh.t.Printf("total %d\n", blocks)
		}
		sh.lsPrint(shown, long)
		printed = true
	}
	return status
}

func lsColor(e vfs.Info) string {
	switch {
	case e.Dir:
		return "\x1b[01;34m" + e.Name + "\x1b[0m"
	case e.Mode&0o111 != 0:
		return "\x1b[01;32m" + e.Name + "\x1b[0m"
	}
	return e.Name
}

func (sh *shell) lsPrint(entries []vfs.Info, long bool) {
	if len(entries) == 0 {
		return
	}
	if !long {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = lsColor(e)
		}
		sh.t.WriteString(columns(names, sh.t.Width(), ""))
		return
	}
	now := sh.cfg.Now()
	for _, e := range entries {
		links := 1
		if e.Dir {
			links = 2
		}
		stamp := e.ModTime.Format("Jan _2 15:04")
		if now.Sub(e.ModTime) > 180*24*time.Hour || e.ModTime.After(now.Add(time.Hour)) {
			stamp = e.ModTime.Format("Jan _2  2006")
		}
		sh.t.Printf("%s %2d %-6s %-6s %6d %s %s\n", modeString(e), links, e.Owner, e.Owner, e.Size, stamp, lsColor(e))
	}
}

func modeString(e vfs.Info) string {
	b := []byte("-rwxrwxrwx")
	if e.Dir {
		b[0] = 'd'
	}
	for i := 0; i < 9; i++ {
		if e.Mode&(1<<uint(8-i)) == 0 {
			b[i+1] = '-'
		}
	}
	return string(b)
}

// readFiles calls each for every readable file and prints errors for the rest.
func (sh *shell) readFiles(prog string, names []string, each func(name string, data []byte)) int {
	if len(names) == 0 {
		sh.t.Printf("%s: reading standard input is not supported on this demo machine; give a file name\n", prog)
		return 1
	}
	status := 0
	for _, n := range names {
		data, err := sh.fs.ReadFile(sh.abs(n))
		if err != nil {
			sh.t.WriteString(fsErr(prog, n, err))
			status = 1
			continue
		}
		each(n, data)
	}
	return status
}

func cmdCat(sh *shell, args []string) int {
	var names []string
	for _, a := range args[1:] {
		if !strings.HasPrefix(a, "-") || a == "-" {
			names = append(names, a)
		}
	}
	return sh.readFiles(args[0], names, func(_ string, data []byte) { sh.t.Write(data) })
}

// lineCountArgs parses -n N / -N / -nN.
func lineCountArgs(args []string) (int, []string, error) {
	n := 10
	var names []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-n" && i+1 < len(args):
			i++
			v, err := strconv.Atoi(args[i])
			if err != nil || v < 0 {
				return 0, nil, fmt.Errorf("invalid number of lines: '%s'", args[i])
			}
			n = v
		case strings.HasPrefix(a, "-n"):
			v, err := strconv.Atoi(a[2:])
			if err != nil || v < 0 {
				return 0, nil, fmt.Errorf("invalid number of lines: '%s'", a[2:])
			}
			n = v
		case strings.HasPrefix(a, "-") && len(a) > 1:
			v, err := strconv.Atoi(a[1:])
			if err != nil || v < 0 {
				return 0, nil, fmt.Errorf("invalid option -- '%s'", a[1:])
			}
			n = v
		default:
			names = append(names, a)
		}
	}
	return n, names, nil
}

func headTail(sh *shell, args []string, tail bool) int {
	n, names, err := lineCountArgs(args[1:])
	if err != nil {
		sh.t.Printf("%s: %v\n", args[0], err)
		return 1
	}
	return sh.readFiles(args[0], names, func(name string, data []byte) {
		if len(names) > 1 {
			sh.t.Printf("==> %s <==\n", name)
		}
		lines := strings.SplitAfter(string(data), "\n")
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		if tail {
			lines = lines[max(0, len(lines)-n):]
		} else {
			lines = lines[:min(n, len(lines))]
		}
		sh.t.WriteString(strings.Join(lines, ""))
	})
}

func cmdHead(sh *shell, args []string) int { return headTail(sh, args, false) }
func cmdTail(sh *shell, args []string) int { return headTail(sh, args, true) }

func cmdGrep(sh *shell, args []string) int {
	var ignoreCase, number bool
	var rest []string
	for _, a := range args[1:] {
		if strings.HasPrefix(a, "-") && len(a) > 1 && len(rest) == 0 {
			for _, f := range a[1:] {
				switch f {
				case 'i':
					ignoreCase = true
				case 'n':
					number = true
				default:
					sh.t.Printf("grep: this demo supports only -i and -n\n")
					return 2
				}
			}
			continue
		}
		rest = append(rest, a)
	}
	if len(rest) == 0 {
		sh.t.WriteString("Usage: grep [-i] [-n] TEXT FILE...\n")
		return 2
	}
	pattern, names := rest[0], rest[1:]
	if ignoreCase {
		pattern = strings.ToLower(pattern)
	}
	found := false
	status := sh.readFiles("grep", names, func(name string, data []byte) {
		for i, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
			hay := line
			if ignoreCase {
				hay = strings.ToLower(line)
			}
			if !strings.Contains(hay, pattern) {
				continue
			}
			found = true
			prefix := ""
			if len(names) > 1 {
				prefix = "\x1b[35m" + name + "\x1b[36m:\x1b[0m"
			}
			if number {
				prefix += fmt.Sprintf("\x1b[32m%d\x1b[36m:\x1b[0m", i+1)
			}
			sh.t.WriteString(prefix + line + "\n")
		}
	})
	switch {
	case status != 0:
		return 2
	case !found:
		return 1
	}
	return 0
}

func cmdEcho(sh *shell, args []string) int {
	newline := true
	words := args[1:]
	for len(words) > 0 && (words[0] == "-n" || words[0] == "-e" || words[0] == "-E") {
		if words[0] == "-n" {
			newline = false
		}
		words = words[1:]
	}
	out := strings.Join(words, " ")
	if newline {
		out += "\n"
	}
	sh.t.WriteString(out)
	return 0
}

func cmdWhoami(sh *shell, _ []string) int {
	sh.t.WriteString(shellUser + "\n")
	return 0
}

func cmdID(sh *shell, _ []string) int {
	sh.t.WriteString("uid=1000(review) gid=1000(review) groups=1000(review) context=unconfined_u:unconfined_r:unconfined_t:s0-s0:c0.c1023\n")
	return 0
}

func cmdHostname(sh *shell, args []string) int {
	if len(args) > 1 && !strings.HasPrefix(args[1], "-") {
		sh.t.WriteString("hostname: you must be root to change the host name\n")
		return 1
	}
	sh.t.WriteString(sh.cfg.Hostname + "\n")
	return 0
}

const (
	kernelRelease = "6.17.4-300.fc45.x86_64"
	kernelVersion = "#1 SMP PREEMPT_DYNAMIC Fri Oct 24 15:03:21 UTC 2026"
)

func cmdUname(sh *shell, args []string) int {
	fields := map[rune]string{
		's': "Linux", 'n': sh.cfg.Hostname, 'r': kernelRelease, 'v': kernelVersion,
		'm': "x86_64", 'p': "unknown", 'i': "unknown", 'o': "GNU/Linux",
	}
	var want []rune
	for _, a := range args[1:] {
		if a == "--all" {
			a = "-a"
		}
		if !strings.HasPrefix(a, "-") {
			sh.t.Printf("uname: extra operand '%s'\n", a)
			return 1
		}
		for _, f := range a[1:] {
			if f == 'a' {
				want = append(want, 's', 'n', 'r', 'v', 'm', 'o')
				continue
			}
			if _, ok := fields[f]; !ok {
				sh.t.Printf("uname: invalid option -- '%c'\n", f)
				return 1
			}
			want = append(want, f)
		}
	}
	if len(want) == 0 {
		want = []rune{'s'}
	}
	order := "snrvmpio"
	var out []string
	for _, f := range order {
		for _, w := range want {
			if w == f {
				out = append(out, fields[f])
				break
			}
		}
	}
	sh.t.WriteString(strings.Join(out, " ") + "\n")
	return 0
}

func cmdDate(sh *shell, args []string) int {
	out, err := dateCommand(sh.cfg.Now(), args[1:])
	if err != nil {
		sh.t.WriteString(err.Error() + "\n")
		return 1
	}
	sh.t.WriteString(out)
	return 0
}

func cmdUptime(sh *shell, args []string) int {
	now := sh.cfg.Now()
	sh.procs.sample(now)
	up := sh.procs.uptime(now)
	if len(args) > 1 {
		switch args[1] {
		case "-p", "--pretty":
			sh.t.WriteString(formatUptimePretty(up) + "\n")
			return 0
		case "-s", "--since":
			sh.t.WriteString(sh.procs.boot.Format("2006-01-02 15:04:05") + "\n")
			return 0
		}
		sh.t.Printf("uptime: invalid option -- '%s'\n", strings.TrimLeft(args[1], "-"))
		return 1
	}
	l := sh.procs.load
	sh.t.Printf(" %s up %s,  1 user,  load average: %.2f, %.2f, %.2f\n", now.Format("15:04:05"), formatUptime(up), l[0], l[1], l[2])
	return 0
}

func cmdFree(sh *shell, args []string) int {
	unit := 1.0
	human := false
	for _, a := range args[1:] {
		switch a {
		case "-h", "--human":
			human = true
		case "-m", "--mebi":
			unit = 1024
		case "-g", "--gibi":
			unit = 1024 * 1024
		case "-k", "--kibi":
		default:
			sh.t.Printf("free: invalid option -- '%s'\n", strings.TrimLeft(a, "-"))
			return 1
		}
	}
	used := sh.procs.memUsedKiB()
	free := memTotalKiB - used - memCacheKiB
	avail := free + memCacheKiB*8/10
	f := func(kib int) string {
		if human {
			return humanKiB(kib)
		}
		return strconv.Itoa(int(float64(kib) / unit))
	}
	sh.t.Printf("               total        used        free      shared  buff/cache   available\n")
	sh.t.Printf("Mem:      %10s  %10s  %10s  %10s  %10s  %10s\n", f(memTotalKiB), f(used), f(free), f(10240), f(memCacheKiB), f(avail))
	sh.t.Printf("Swap:     %10s  %10s  %10s\n", f(swapTotalKiB), f(0), f(swapTotalKiB))
	return 0
}

func humanKiB(kib int) string {
	v := float64(kib)
	for _, u := range []string{"Ki", "Mi", "Gi", "Ti"} {
		if v < 1024 {
			if v < 10 && u != "Ki" {
				return fmt.Sprintf("%.1f%s", v, u)
			}
			return fmt.Sprintf("%.0f%s", v, u)
		}
		v /= 1024
	}
	return fmt.Sprintf("%.1fPi", v)
}

func cmdDf(sh *shell, args []string) int {
	human := len(args) > 1 && (args[1] == "-h" || args[1] == "--human-readable")
	type row struct {
		fs         string
		size, used int // KiB
		mount      string
	}
	rows := []row{
		{"/dev/vda3", 41930752, 9142316, "/"},
		{"devtmpfs", 4096, 0, "/dev"},
		{"tmpfs", 2007724, 84, "/dev/shm"},
		{"/dev/vda2", 996780, 271636, "/boot"},
		{"tmpfs", 2007728, 1200, "/tmp"},
	}
	if human {
		sh.t.WriteString("Filesystem      Size  Used Avail Use% Mounted on\n")
	} else {
		sh.t.WriteString("Filesystem     1K-blocks    Used Available Use% Mounted on\n")
	}
	for _, r := range rows {
		avail := r.size - r.used
		pct := (r.used*100 + r.size - 1) / r.size
		if human {
			sh.t.Printf("%-15s %4s %5s %5s %3d%% %s\n", r.fs, humanBytes(r.size), humanBytes(r.used), humanBytes(avail), pct, r.mount)
		} else {
			sh.t.Printf("%-14s %10d %7d %9d %3d%% %s\n", r.fs, r.size, r.used, avail, pct, r.mount)
		}
	}
	return 0
}

// humanBytes renders a KiB count like df -h ("40G", "973M").
func humanBytes(kib int) string {
	if kib == 0 {
		return "0"
	}
	v := float64(kib)
	for _, u := range []string{"K", "M", "G", "T"} {
		if v < 1024 {
			if v < 10 {
				return fmt.Sprintf("%.1f%s", v, u)
			}
			return fmt.Sprintf("%.0f%s", v, u)
		}
		v /= 1024
	}
	return fmt.Sprintf("%.0fP", v)
}

func cmdEnv(sh *shell, args []string) int {
	if len(args) > 1 {
		v, ok := sh.env[args[1]]
		if !ok {
			return 1
		}
		sh.t.WriteString(v + "\n")
		return 0
	}
	keys := make([]string, 0, len(sh.env))
	for k := range sh.env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		sh.t.Printf("%s=%s\n", k, sh.env[k])
	}
	return 0
}

func cmdExport(sh *shell, args []string) int {
	for _, a := range args[1:] {
		if k, v, ok := strings.Cut(a, "="); ok && k != "" {
			sh.env[k] = v
		}
	}
	return 0
}

func cmdUnset(sh *shell, args []string) int {
	for _, a := range args[1:] {
		delete(sh.env, a)
	}
	return 0
}

func cmdHistory(sh *shell, args []string) int {
	if len(args) > 1 && args[1] == "-c" {
		sh.ed.ResetHistory()
		return 0
	}
	for i, h := range sh.ed.History() {
		sh.t.Printf("%5d  %s\n", i+1, h)
	}
	return 0
}

func cmdClear(sh *shell, _ []string) int {
	sh.t.ClearScreen()
	return 0
}

func cmdExit(sh *shell, _ []string) int {
	sh.logout()
	return 0
}

func cmdTouch(sh *shell, args []string) int {
	if len(args) < 2 {
		sh.t.WriteString("touch: missing file operand\n")
		return 1
	}
	status := 0
	for _, a := range args[1:] {
		if err := sh.fs.Touch(sh.abs(a)); err != nil {
			sh.t.Printf("touch: cannot touch '%s': %v\n", a, err)
			status = 1
		}
	}
	return status
}

func cmdMkdir(sh *shell, args []string) int {
	parents := false
	var names []string
	for _, a := range args[1:] {
		if a == "-p" || a == "--parents" {
			parents = true
			continue
		}
		names = append(names, a)
	}
	if len(names) == 0 {
		sh.t.WriteString("mkdir: missing operand\n")
		return 1
	}
	status := 0
	for _, n := range names {
		if err := sh.fs.Mkdir(sh.abs(n), parents); err != nil {
			sh.t.Printf("mkdir: cannot create directory '%s': %v\n", n, err)
			status = 1
		}
	}
	return status
}

func cmdRm(sh *shell, args []string) int {
	recursive, force := false, false
	var names []string
	for _, a := range args[1:] {
		if strings.HasPrefix(a, "-") && len(a) > 1 {
			recursive = recursive || strings.ContainsAny(a[1:], "rR")
			force = force || strings.Contains(a[1:], "f")
			continue
		}
		names = append(names, a)
	}
	if len(names) == 0 {
		sh.t.WriteString("rm: missing operand\n")
		return 1
	}
	status := 0
	for _, n := range names {
		p := sh.abs(n)
		st, err := sh.fs.Stat(p)
		if err != nil {
			if !force {
				sh.t.Printf("rm: cannot remove '%s': %v\n", n, err)
				status = 1
			}
			continue
		}
		if st.Dir && !recursive {
			sh.t.Printf("rm: cannot remove '%s': Is a directory\n", n)
			status = 1
			continue
		}
		if err := sh.fs.Remove(p, recursive); err != nil {
			sh.t.Printf("rm: cannot remove '%s': %v\n", n, err)
			status = 1
		}
	}
	return status
}

func cmdRmdir(sh *shell, args []string) int {
	status := 0
	for _, n := range args[1:] {
		if err := sh.fs.Remove(sh.abs(n), false); err != nil {
			sh.t.Printf("rmdir: failed to remove '%s': %v\n", n, err)
			status = 1
		}
	}
	return status
}

func cmdWhich(sh *shell, args []string) int {
	status := 0
	for _, n := range args[1:] {
		if _, ok := lookupCommand(n); ok {
			if args[0] == "type" {
				sh.t.Printf("%s is /usr/bin/%s\n", n, n)
			} else {
				sh.t.Printf("/usr/bin/%s\n", n)
			}
			continue
		}
		sh.t.Printf("%s: no %s in (%s)\n", args[0], n, shellPath)
		status = 1
	}
	return status
}

func cmdKill(sh *shell, args []string) int {
	if len(args) < 2 {
		sh.t.WriteString("kill: usage: kill [-s sigspec | -n signum | -sigspec] pid | jobspec ... or kill -l [sigspec]\n")
		return 2
	}
	pid := args[len(args)-1]
	sh.t.Printf("bash: kill: (%s) - Operation not permitted\n", pid)
	return 1
}

func cmdSystemctl(sh *shell, args []string) int {
	if len(args) >= 3 && args[1] == "status" {
		now := sh.cfg.Now()
		sh.t.Printf("\x1b[32m●\x1b[0m %s.service - %s\n     Loaded: loaded (/usr/lib/systemd/system/%s.service; enabled)\n     Active: \x1b[32mactive (running)\x1b[0m since %s; %s ago\n",
			args[2], args[2], args[2], sh.procs.boot.Format("Mon 2006-01-02 15:04:05 MST"), formatUptimePretty(now.Sub(sh.procs.boot))[3:])
		return 0
	}
	sh.t.WriteString("systemctl: only 'systemctl status NAME' is available on this demo machine\n")
	return 1
}

func cmdSleep(sh *shell, args []string) int {
	if len(args) < 2 {
		sh.t.WriteString("sleep: missing operand\n")
		return 1
	}
	secs, err := strconv.ParseFloat(strings.TrimSuffix(args[1], "s"), 64)
	if err != nil || secs < 0 {
		sh.t.Printf("sleep: invalid time interval '%s'\n", args[1])
		return 1
	}
	if secs > 60 {
		sh.t.WriteString("sleep: limited to 60 seconds on this demo machine\n")
		secs = 60
	}
	if !sh.t.Sleep(sh.ctx, time.Duration(secs*float64(time.Second))) {
		return 130
	}
	return 0
}

func cmdBuild(sh *shell, _ []string) int {
	start := sh.cfg.Now()
	steps := []string{
		"resolving modules",
		"compiling internal/store",
		"compiling internal/api",
		"compiling cmd/demo-app",
		"linking bin/demo-app",
	}
	for i, s := range steps {
		sh.t.Printf("\x1b[36m[%d/%d]\x1b[0m %s\n", i+1, len(steps), s)
		if !sh.t.Sleep(sh.ctx, 700*time.Millisecond) {
			sh.t.WriteString("\x1b[33mbuild interrupted\x1b[0m\n")
			return 130
		}
	}
	elapsed := sh.cfg.Now().Sub(start).Seconds()
	sh.t.Printf("\x1b[1;32mBUILD SUCCESSFUL\x1b[0m in %.1fs\n", elapsed)
	return 0
}

func cmdGit(sh *shell, args []string) int {
	if sh.cwd != shellHome && !strings.HasPrefix(sh.cwd, shellHome+"/") {
		sh.t.WriteString("fatal: not a git repository (or any of the parent directories): .git\n")
		return 128
	}
	sub := "help"
	if len(args) > 1 {
		sub = args[1]
	}
	now := sh.cfg.Now()
	switch sub {
	case "status":
		sh.t.WriteString("On branch main\nYour branch is up to date with 'origin/main'.\n\nChanges not staged for commit:\n  (use \"git add <file>...\" to update what will be committed)\n  (use \"git restore <file>...\" to discard changes in working directory)\n\t\x1b[31mmodified:   app/main.go\x1b[0m\n\nno changes added to commit (use \"git add\" and/or \"git commit -a\")\n")
	case "log":
		commits := []struct {
			hash, msg string
			ago       time.Duration
		}{
			{"3f9c2ab", "api: add /healthz endpoint", 2 * time.Hour},
			{"b71e04d", "store: retry the database connection on startup", 26 * time.Hour},
			{"9a0d5c3", "build: bump Go to 1.27", 3 * 24 * time.Hour},
			{"41ce8f7", "Initial commit", 9 * 24 * time.Hour},
		}
		oneline := len(args) > 2 && args[2] == "--oneline"
		for i, c := range commits {
			head := ""
			if i == 0 {
				head = " (\x1b[1;36mHEAD -> \x1b[1;32mmain\x1b[33m, \x1b[1;31morigin/main\x1b[33m)"
			}
			if oneline {
				sh.t.Printf("\x1b[33m%s%s\x1b[0m %s\n", c.hash, head, c.msg)
				continue
			}
			sh.t.Printf("\x1b[33mcommit %s9e1f04b2c7d3a8e6f5b0c1d2e3f4a5b6c7d8%s\x1b[0m\nAuthor: Review Demo <review@scomp.me>\nDate:   %s\n\n    %s\n\n",
				c.hash, head, now.Add(-c.ago).Format("Mon Jan 2 15:04:05 2006 -0700"), c.msg)
		}
	case "diff":
		sh.t.WriteString("\x1b[1mdiff --git a/app/main.go b/app/main.go\x1b[0m\n\x1b[1m--- a/app/main.go\x1b[0m\n\x1b[1m+++ b/app/main.go\x1b[0m\n\x1b[36m@@ -9,6 +9,7 @@\x1b[0m func main() {\n \thttp.HandleFunc(\"/healthz\", func(w http.ResponseWriter, r *http.Request) {\n\x1b[31m-\t\tfmt.Fprintln(w, \"ok\")\x1b[0m\n\x1b[32m+\t\tw.Header().Set(\"Cache-Control\", \"no-store\")\x1b[0m\n\x1b[32m+\t\tfmt.Fprintln(w, \"ok\")\x1b[0m\n \t})\n")
	case "branch":
		sh.t.WriteString("  feature/metrics\n* \x1b[32mmain\x1b[0m\n")
	case "push", "pull", "fetch", "clone", "remote":
		sh.t.WriteString("fatal: this demo machine has no network access\n")
		return 128
	case "help", "--help":
		sh.t.WriteString("usage: git status | git log [--oneline] | git diff | git branch\n(only these are available on this demo machine)\n")
	default:
		sh.t.Printf("git: '%s' is not available on this demo machine. Try: git status, git log, git diff, git branch\n", sub)
		return 1
	}
	return 0
}
