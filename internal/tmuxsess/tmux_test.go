package tmuxsess

import "testing"

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
