package main

import "testing"

// A session-based game gets exactly one playthrough entry, ever —
// doNewPlaythrough guards this directly, not just via the menu.
func TestDoNewPlaythrough_RefusesASecondSessionBasedEntry(t *testing.T) {
	doc := &Doc{FM: FrontMatter{Title: "Team Fortress 2", Status: "session-based"}}
	pf := &PlaythroughsFile{Playthroughs: []PlaythroughEntry{
		{Sessions: []SessionEntry{{Started: "2020-01-01"}}},
	}}

	err := doNewPlaythrough(pf, doc, true)
	if err == nil {
		t.Fatal("expected a second playthrough on a session-based game to be refused")
	}
	if len(pf.Playthroughs) != 1 {
		t.Fatalf("refused call must not mutate the playthroughs, got %d entries", len(pf.Playthroughs))
	}
}
