package main

import "testing"

func TestRestoreLazySessionAfterScreenDisplayExit(t *testing.T) {
	entry := lazySession{id: "stable-id", cmd: "screen:yughljkkok"}
	available := map[string]lazySession{entry.id: entry}
	lazy := make(map[string]*lazySession)

	got, ok := restoreLazySession(entry.id, available, lazy)
	if !ok || got.id != entry.id {
		t.Fatalf("restoreLazySession() = %#v, %v", got, ok)
	}
	if lazy[entry.id] == nil || lazy[entry.id].cmd != entry.cmd {
		t.Fatalf("lazy session was not restored: %#v", lazy)
	}
}

func TestRestoreLazySessionDoesNotReviveRemovedCatalogEntry(t *testing.T) {
	lazy := make(map[string]*lazySession)
	if _, ok := restoreLazySession("removed", map[string]lazySession{}, lazy); ok {
		t.Fatal("removed catalog entry was restored")
	}
	if len(lazy) != 0 {
		t.Fatalf("unexpected lazy sessions: %#v", lazy)
	}
}
