package main

import "testing"

// oneShotConflict is doNewPlaythrough's guard, pulled out so it's testable
// without driving the interactive form (huh has no headless mode a unit
// test can exercise, so doNewPlaythrough itself isn't a useful test target).

func TestOneShotConflict_RefusesASecondEntryOfTheSameOneShotStatusOnThePlatform(t *testing.T) {
	existing := []PlaythroughEntry{
		{Status: "ongoing", Sessions: []SessionEntry{{Started: "2020-01-01"}}},
	}
	if oneShotConflict(existing, "ongoing", "", "", "ongoing") == nil {
		t.Fatal("expected a second ongoing entry on the same platform to be refused")
	}
}

func TestOneShotConflict_AllowsADifferentStatusToCoexist(t *testing.T) {
	// Hitman: a finished singleplayer campaign alongside an ongoing
	// Freelancer entry is two real modes, not fragmentation of one.
	existing := []PlaythroughEntry{{Status: "ongoing"}}
	if got := oneShotConflict(existing, "mastered", "", "", "mastered"); got != nil {
		t.Fatalf("a mastered entry should be able to coexist with an ongoing one, got conflict with %+v", got)
	}
}

func TestOneShotConflict_AllowsTheSameOneShotStatusOnADifferentPlatform(t *testing.T) {
	existing := []PlaythroughEntry{{Status: "multiplayer", Platform: "PS4"}}
	if got := oneShotConflict(existing, "multiplayer", "PC", "", "multiplayer"); got != nil {
		t.Fatalf("a second platform is a separate record, not a conflict, got %+v", got)
	}
}

func TestOneShotConflict_LegacyPlayingEntryStillBlocksANewOneShotEntry(t *testing.T) {
	// Every ongoing/multiplayer game written before per-entry ongoing/
	// multiplayer support has its sole entry's own status as "playing" —
	// the game-level status carried the real meaning. That old-style entry
	// must still block a redundant new "ongoing" entry on the same platform.
	existing := []PlaythroughEntry{
		{Status: "playing", Sessions: []SessionEntry{{Started: "2020-01-01"}}},
	}
	if oneShotConflict(existing, "ongoing", "", "", "ongoing") == nil {
		t.Fatal("expected a legacy 'playing' entry on an ongoing game to still block a second ongoing entry")
	}
}

func TestOneShotConflict_LegacyPlayingEntryDoesNotBlockADifferentNewStatus(t *testing.T) {
	existing := []PlaythroughEntry{{Status: "playing"}}
	if got := oneShotConflict(existing, "mastered", "", "", "ongoing"); got != nil {
		t.Fatalf("a legacy ongoing entry shouldn't block an unrelated mastered entry, got %+v", got)
	}
}

func TestHasManageableSessions(t *testing.T) {
	if hasManageableSessions(nil) {
		t.Error("no playthroughs means nothing to manage")
	}
	if hasManageableSessions([]Playthrough{{Started: "2024-01-01"}}) {
		t.Error("a flat entry with no sessions has nothing 'Manage sessions' can act on")
	}
	if !hasManageableSessions([]Playthrough{{Sessions: []Session{{Started: "2024-01-01"}}}}) {
		t.Error("an entry with sessions should be manageable")
	}
}
