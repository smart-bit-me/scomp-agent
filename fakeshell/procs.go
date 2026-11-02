package fakeshell

import (
	"math/rand/v2"
	"sort"
	"time"
)

// proc is one row of the fake process table.
type proc struct {
	PID, PPID int
	User      string
	TTY       string
	Stat      string // ps STAT column; top uses its first letter
	Comm      string // short name (top COMMAND)
	Cmd       string // full command line (ps aux COMMAND)
	PR, NI    int
	VSZ, RSS  int // KiB
	SHR       int // KiB
	Start     time.Time
	CPUTime   time.Duration
	CPU       float64 // current %CPU
	base      float64 // typical %CPU
}

// Simulated machine resources.
const (
	memTotalKiB  = 4015452
	memCacheKiB  = 1214332
	memKernelKiB = 386240
	swapTotalKiB = 2097148
	numCPU       = 2
)

// fakeUptime is how long the machine pretends to have been up when the agent
// starts; the clock then advances it.
const fakeUptime = 3*24*time.Hour + 4*time.Hour + 12*time.Minute

// procTable is the simulated process list. Values move a little on every
// sample so top looks alive; the shell's own processes (ps, top) appear with
// fresh PIDs.
type procTable struct {
	boot    time.Time
	last    time.Time
	rng     *rand.Rand
	procs   []proc
	nextPID int
	load    [3]float64
}

func newProcTable(now time.Time, seed uint64) *procTable {
	boot := now.Add(-fakeUptime)
	at := func(d time.Duration) time.Time { return boot.Add(d) }
	t := &procTable{
		boot:    boot,
		last:    now,
		rng:     rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)),
		nextPID: 48211,
		load:    [3]float64{0.42, 0.37, 0.31},
	}
	t.procs = []proc{
		{PID: 1, User: "root", TTY: "?", Stat: "Ss", Comm: "systemd", Cmd: "/usr/lib/systemd/systemd --switched-root --system --deserialize=42", VSZ: 173940, RSS: 17920, SHR: 10752, Start: at(0), base: 0.1},
		{PID: 2, User: "root", TTY: "?", Stat: "S", Comm: "kthreadd", Cmd: "[kthreadd]", Start: at(0)},
		{PID: 612, PPID: 1, User: "root", TTY: "?", Stat: "Ss", Comm: "systemd-journal", Cmd: "/usr/lib/systemd/systemd-journald", VSZ: 58716, RSS: 22528, SHR: 21504, Start: at(2 * time.Second), base: 0.1},
		{PID: 655, PPID: 1, User: "root", TTY: "?", Stat: "Ss", Comm: "systemd-udevd", Cmd: "/usr/lib/systemd/systemd-udevd", VSZ: 34576, RSS: 11264, SHR: 8192, Start: at(2 * time.Second)},
		{PID: 812, PPID: 1, User: "root", TTY: "?", Stat: "Ss", Comm: "sshd", Cmd: "sshd: /usr/sbin/sshd -D [listener] 0 of 10-100 startups", VSZ: 16448, RSS: 9216, SHR: 7936, Start: at(6 * time.Second)},
		{PID: 815, PPID: 1, User: "root", TTY: "?", Stat: "Ss", Comm: "crond", Cmd: "/usr/sbin/crond -n", VSZ: 222380, RSS: 3584, SHR: 3200, Start: at(6 * time.Second)},
		{PID: 902, PPID: 1, User: "postgres", TTY: "?", Stat: "Ss", Comm: "postgres", Cmd: "/usr/bin/postgres -D /var/lib/pgsql/data", VSZ: 225312, RSS: 29184, SHR: 26880, Start: at(8 * time.Second), base: 0.3},
		{PID: 915, PPID: 902, User: "postgres", TTY: "?", Stat: "Ss", Comm: "postgres", Cmd: "postgres: checkpointer", VSZ: 225452, RSS: 8448, SHR: 6400, Start: at(8 * time.Second)},
		{PID: 916, PPID: 902, User: "postgres", TTY: "?", Stat: "Ss", Comm: "postgres", Cmd: "postgres: walwriter", VSZ: 225312, RSS: 10496, SHR: 8448, Start: at(8 * time.Second), base: 0.1},
		{PID: 917, PPID: 902, User: "postgres", TTY: "?", Stat: "Ss", Comm: "postgres", Cmd: "postgres: autovacuum launcher", VSZ: 226860, RSS: 7680, SHR: 5376, Start: at(8 * time.Second)},
		{PID: 1010, PPID: 1, User: "root", TTY: "?", Stat: "Ss", Comm: "nginx", Cmd: "nginx: master process /usr/sbin/nginx", VSZ: 11296, RSS: 3072, SHR: 1792, Start: at(9 * time.Second)},
		{PID: 1011, PPID: 1010, User: "nginx", TTY: "?", Stat: "S", Comm: "nginx", Cmd: "nginx: worker process", VSZ: 15580, RSS: 7424, SHR: 4352, Start: at(9 * time.Second), base: 0.3},
		{PID: 1012, PPID: 1010, User: "nginx", TTY: "?", Stat: "S", Comm: "nginx", Cmd: "nginx: worker process", VSZ: 15580, RSS: 7168, SHR: 4352, Start: at(9 * time.Second), base: 0.2},
		{PID: 2140, PPID: 1, User: "review", TTY: "?", Stat: "Ssl", Comm: "demo-app", Cmd: "/home/review/app/bin/demo-app --listen :8080", VSZ: 1247900, RSS: 48640, SHR: 12288, Start: at(2 * time.Hour), base: 1.6},
		{PID: 2177, PPID: 1, User: "review", TTY: "?", Stat: "Ssl", Comm: "node", Cmd: "node /home/review/app/web/server.js", VSZ: 1094812, RSS: 96512, SHR: 39936, Start: at(2 * time.Hour), base: 2.4},
		{PID: 2190, PPID: 1, User: "review", TTY: "?", Stat: "Ssl", Comm: "scomp", Cmd: "scomp --mode full", VSZ: 1235676, RSS: 15360, SHR: 9984, Start: at(2 * time.Hour), base: 0.4},
		{PID: 2214, PPID: 2190, User: "review", TTY: "pts/0", Stat: "Ss", Comm: "bash", Cmd: "-bash", VSZ: 224352, RSS: 5376, SHR: 3712, Start: at(2 * time.Hour), base: 0},
	}
	for i := range t.procs {
		p := &t.procs[i]
		p.CPUTime = time.Duration(p.base / 100 * float64(now.Sub(p.Start)))
	}
	t.sample(now)
	return t
}

// sample advances the table to now: new %CPU values, accumulated CPU time,
// smoothed load averages. It returns the table sorted by PID.
func (t *procTable) sample(now time.Time) []proc {
	dt := now.Sub(t.last)
	if dt < 0 {
		dt = 0
	}
	t.last = now
	total := 0.0
	for i := range t.procs {
		p := &t.procs[i]
		cpu := p.base * (0.4 + 1.2*t.rng.Float64())
		if p.base >= 1 && t.rng.IntN(8) == 0 {
			cpu += 4 + 6*t.rng.Float64() // an occasional burst of work
		}
		p.CPU = cpu
		p.CPUTime += time.Duration(cpu / 100 * float64(dt))
		total += cpu
	}
	inst := total/100*numCPU + 0.15*t.rng.Float64()
	for i, w := range []float64{0.30, 0.08, 0.03} {
		t.load[i] += w * (inst - t.load[i])
	}
	out := append([]proc(nil), t.procs...)
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return out
}

// transient returns a short-lived process of the shell (ps, top) with a
// fresh PID.
func (t *procTable) transient(now time.Time, comm, cmd string, cpu float64) proc {
	t.nextPID += 1 + t.rng.IntN(6)
	return proc{
		PID: t.nextPID, PPID: 2214, User: "review", TTY: "pts/0", Stat: "R+",
		Comm: comm, Cmd: cmd, VSZ: 226044, RSS: 4352, SHR: 3584, Start: now, CPU: cpu,
	}
}

// memUsedKiB is the simulated used memory.
func (t *procTable) memUsedKiB() int {
	used := memKernelKiB
	for _, p := range t.procs {
		used += p.RSS
	}
	return used
}

func (t *procTable) uptime(now time.Time) time.Duration { return now.Sub(t.boot) }
