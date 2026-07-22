package screensess

import "testing"

func TestParseScreenOutput_Empty(t *testing.T) {
	if got := parseScreenOutput(""); len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
}

func TestParseScreenOutput_Attached(t *testing.T) {
	out := "\t68764.myname\t(Attached)\n"
	got := parseScreenOutput(out)
	if len(got) != 1 || got[0].Name != "myname" || got[0].Status != "Attached" {
		t.Fatalf("got %v", got)
	}
}

func TestParseScreenOutput_Detached(t *testing.T) {
	out := "\t12345.work\t(Detached)\n"
	got := parseScreenOutput(out)
	if len(got) != 1 || got[0].Name != "work" || got[0].Status != "Detached" {
		t.Fatalf("got %v", got)
	}
}

func TestParseScreenOutput_SkipsHeader(t *testing.T) {
	out := "There are screens on:\n\t68764.myname\t(Attached)\n"
	got := parseScreenOutput(out)
	if len(got) != 1 {
		t.Fatalf("expected 1, got %v", got)
	}
}

func TestParseScreenOutput_FullName(t *testing.T) {
	out := "\t68764.myname\t(Attached)\n"
	got := parseScreenOutput(out)
	if got[0].FullName != "68764.myname" {
		t.Fatalf("got FullName %q", got[0].FullName)
	}
}

func TestParseScreenOutput_DeduplicatesDisplayName(t *testing.T) {
	out := "\t12345.work\t(Attached)\n\t67890.work\t(Detached)\n"
	got := parseScreenOutput(out)
	if len(got) != 1 {
		t.Fatalf("expected one logical display, got %v", got)
	}
	if got[0].FullName != "12345.work" {
		t.Fatalf("kept FullName %q, want first stable socket", got[0].FullName)
	}
}

func TestAttachArgs_NoAFlag(t *testing.T) {
	_, args := AttachArgs("68764.myname")
	if len(args) != 2 || args[0] != "-x" || args[1] != "68764.myname" {
		t.Fatalf("unexpected args %v (must not contain -A)", args)
	}
	for _, a := range args {
		if a == "-A" {
			t.Fatal("AttachArgs must not include -A")
		}
	}
}
