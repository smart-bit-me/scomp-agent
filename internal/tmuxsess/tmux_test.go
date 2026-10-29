package tmuxsess

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

func TestParseOutput_Empty(t *testing.T) {
	if got := parseOutput(""); len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
}

func TestParseOutput_Single(t *testing.T) {
	got := parseOutput("main|2\n")
	if len(got) != 1 || got[0].Name != "main" || got[0].Windows != 2 {
		t.Fatalf("got %v", got)
	}
}

func TestParseOutput_FiltersLinked(t *testing.T) {
	out := "scomp-main|1\nmain|2\n"
	got := parseOutput(out)
	if len(got) != 1 || got[0].Name != "main" {
		t.Fatalf("expected only main, got %v", got)
	}
}

func TestParseOutput_Multiple(t *testing.T) {
	out := "alpha|1\nbeta|3\ngamma|2\n"
	got := parseOutput(out)
	if len(got) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(got))
	}
}

func TestParseOutput_NoWindowCount(t *testing.T) {
	got := parseOutput("mysession|0\n")
	if len(got) != 1 || got[0].Windows != 1 {
		t.Fatalf("got %v", got)
	}
}

func TestLinkedSessionArgs(t *testing.T) {
	_, args := LinkedSessionArgs("main")
	linked := "scomp-main"
	found := false
	for _, a := range args {
		if a == linked {
			found = true
		}
	}
	if !found {
		t.Fatalf("linked session name %q not in args %v", linked, args)
	}
}

func TestTargetsUseExactMatch(t *testing.T) {
	if got := ExactTarget("X"); got != "=X" {
		t.Fatalf("ExactTarget = %q", got)
	}
	_, args := AttachArgs("X")
	if want := []string{"attach-session", "-t", "=X"}; !slices.Equal(args, want) {
		t.Fatalf("AttachArgs = %v, want %v", args, want)
	}
	_, args = LinkedSessionArgs("X")
	if want := []string{"new-session", "-A", "-t", "=X", "-s", "scomp-X"}; !slices.Equal(args, want) {
		t.Fatalf("LinkedSessionArgs = %v, want %v", args, want)
	}
}

// privateTmux points every "tmux" invocation in this process at a throwaway
// server, so the test never touches the user's sessions.
func privateTmux(t *testing.T) func(args ...string) (string, error) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	// Short path: tmux socket paths are limited to ~104 bytes on macOS.
	dir, err := os.MkdirTemp("/tmp", "scomp-tmux-")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX", "") // otherwise tmux would target the enclosing server
	t.Setenv("TMUX_TMPDIR", dir)
	run := func(args ...string) (string, error) {
		out, err := exec.Command("tmux", append([]string{"-f", "/dev/null"}, args...)...).CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	t.Cleanup(func() {
		_, _ = run("kill-server")
		os.RemoveAll(dir)
	})
	return run
}

func sessionGroups(t *testing.T, run func(...string) (string, error)) map[string]string {
	t.Helper()
	out, err := run("list-sessions", "-F", "#{session_name}|#{session_group}")
	if err != nil {
		return map[string]string{}
	}
	groups := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		name, group, _ := strings.Cut(line, "|")
		groups[name] = group
	}
	return groups
}

func TestTmuxExactTargeting(t *testing.T) {
	run := privateTmux(t)
	for _, name := range []string{"Xprod", "scomp-Xprod"} {
		var err error
		if name == "scomp-Xprod" {
			_, err = run("new-session", "-d", "-t", "=Xprod", "-s", name)
		} else {
			_, err = run("new-session", "-d", "-s", name)
		}
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}

	// Stale "X": killing its linked session must not hit scomp-Xprod.
	if err := KillLinkedSession("X"); err == nil {
		t.Fatal("KillLinkedSession(X) succeeded with no scomp-X session")
	}
	if _, ok := sessionGroups(t, run)["scomp-Xprod"]; !ok {
		t.Fatal("KillLinkedSession(X) killed scomp-Xprod")
	}

	// Linking a missing "X" must not join Xprod's window group.
	_, args := LinkedSessionArgs("X")
	if out, err := run(append(args, "-d")...); err != nil {
		t.Fatalf("new-session for missing X: %v: %s", err, out)
	}
	if g := sessionGroups(t, run)["scomp-X"]; g == "Xprod" {
		t.Fatal("scomp-X was grouped with Xprod")
	}
	if err := KillLinkedSession("X"); err != nil {
		t.Fatalf("KillLinkedSession(X): %v", err)
	}

	// With X present, linking groups with X exactly.
	if _, err := run("new-session", "-d", "-s", "X"); err != nil {
		t.Fatal(err)
	}
	if out, err := run(append(args, "-d")...); err != nil {
		t.Fatalf("new-session for X: %v: %s", err, out)
	}
	groups := sessionGroups(t, run)
	if groups["scomp-X"] != "X" {
		t.Fatalf("scomp-X group = %q, want X (sessions %v)", groups["scomp-X"], groups)
	}

	if err := KillLinkedSession("X"); err != nil {
		t.Fatalf("KillLinkedSession(X): %v", err)
	}
	groups = sessionGroups(t, run)
	if _, ok := groups["scomp-X"]; ok {
		t.Fatal("scomp-X survived KillLinkedSession")
	}
	for _, name := range []string{"X", "Xprod", "scomp-Xprod"} {
		if _, ok := groups[name]; !ok {
			t.Fatalf("%s was killed (sessions %v)", name, groups)
		}
	}
}
