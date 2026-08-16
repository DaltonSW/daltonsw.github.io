package mutate

import (
	"testing"

	"go.dalton.dog/gamelog/internal/model"
)

// OneShotConflict is doNewPlaythrough's guard, pulled out so it's testable
// without driving the interactive form (huh has no headless mode a unit
// test can exercise, so doNewPlaythrough itself isn't a useful test target).

func TestOneShotConflict_RefusesASecondEntryOfTheSameOneShotStatusOnThePlatform(t *testing.T) {
	existing := []model.PlaythroughEntry{
		{Status: "endless", Sessions: []model.SessionEntry{{Started: "2020-01-01"}}},
	}
	if OneShotConflict(existing, "endless", "", "", "endless", "") == nil {
		t.Fatal("expected a second endless entry on the same platform to be refused")
	}
}

func TestOneShotConflict_AllowsADifferentStatusToCoexist(t *testing.T) {
	// Hitman: a finished singleplayer campaign alongside an endless
	// Freelancer entry is two real modes, not fragmentation of one.
	existing := []model.PlaythroughEntry{{Status: "endless"}}
	if got := OneShotConflict(existing, "mastered", "", "", "mastered", ""); got != nil {
		t.Fatalf("a mastered entry should be able to coexist with an endless one, got conflict with %+v", got)
	}
}

func TestOneShotConflict_AllowsTheSameOneShotStatusOnADifferentPlatform(t *testing.T) {
	existing := []model.PlaythroughEntry{{Status: "multiplayer", Platform: "PS4"}}
	if got := OneShotConflict(existing, "multiplayer", "PC", "", "multiplayer", ""); got != nil {
		t.Fatalf("a second platform is a separate record, not a conflict, got %+v", got)
	}
}

func TestOneShotConflict_LegacyPlayingEntryStillBlocksANewOneShotEntry(t *testing.T) {
	// Every endless/multiplayer game written before per-entry endless/
	// multiplayer support has its sole entry's own status as "playing" —
	// the game-level status carried the real meaning. That old-style entry
	// must still block a redundant new "endless" entry on the same platform.
	existing := []model.PlaythroughEntry{
		{Status: "playing", Sessions: []model.SessionEntry{{Started: "2020-01-01"}}},
	}
	if OneShotConflict(existing, "endless", "", "", "endless", "") == nil {
		t.Fatal("expected a legacy 'playing' entry on an endless game to still block a second endless entry")
	}
}

func TestOneShotConflict_LegacyPlayingEntryDoesNotBlockADifferentNewStatus(t *testing.T) {
	existing := []model.PlaythroughEntry{{Status: "playing"}}
	if got := OneShotConflict(existing, "mastered", "", "", "endless", ""); got != nil {
		t.Fatalf("a legacy endless entry shouldn't block an unrelated mastered entry, got %+v", got)
	}
}

func TestOneShotConflict_AllowsTheSameOneShotStatusOnADifferentSubgame(t *testing.T) {
	// A Shovel Knight: Treasure Trove-style compilation where two different
	// campaigns each have their own endless mode on the same platform is two
	// real modes, not fragmentation of one.
	existing := []model.PlaythroughEntry{{Status: "endless", Subgame: "Plague of Shadows"}}
	if got := OneShotConflict(existing, "endless", "", "", "endless", "Specter of Torment"); got != nil {
		t.Fatalf("a different subgame is a separate record, not a conflict, got %+v", got)
	}
}
