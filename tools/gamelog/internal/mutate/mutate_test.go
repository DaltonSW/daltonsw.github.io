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
		{Status: "ongoing", Sessions: []model.SessionEntry{{Started: "2020-01-01"}}},
	}
	if OneShotConflict(existing, "ongoing", "", "", "ongoing", "") == nil {
		t.Fatal("expected a second ongoing entry on the same platform to be refused")
	}
}

func TestOneShotConflict_AllowsADifferentStatusToCoexist(t *testing.T) {
	// Hitman: a finished singleplayer campaign alongside an ongoing
	// Freelancer entry is two real modes, not fragmentation of one.
	existing := []model.PlaythroughEntry{{Status: "ongoing"}}
	if got := OneShotConflict(existing, "mastered", "", "", "mastered", ""); got != nil {
		t.Fatalf("a mastered entry should be able to coexist with an ongoing one, got conflict with %+v", got)
	}
}

func TestOneShotConflict_AllowsTheSameOneShotStatusOnADifferentPlatform(t *testing.T) {
	existing := []model.PlaythroughEntry{{Status: "multiplayer", Platform: "PS4"}}
	if got := OneShotConflict(existing, "multiplayer", "PC", "", "multiplayer", ""); got != nil {
		t.Fatalf("a second platform is a separate record, not a conflict, got %+v", got)
	}
}

func TestOneShotConflict_LegacyPlayingEntryStillBlocksANewOneShotEntry(t *testing.T) {
	// Every ongoing/multiplayer game written before per-entry ongoing/
	// multiplayer support has its sole entry's own status as "playing" —
	// the game-level status carried the real meaning. That old-style entry
	// must still block a redundant new "ongoing" entry on the same platform.
	existing := []model.PlaythroughEntry{
		{Status: "playing", Sessions: []model.SessionEntry{{Started: "2020-01-01"}}},
	}
	if OneShotConflict(existing, "ongoing", "", "", "ongoing", "") == nil {
		t.Fatal("expected a legacy 'playing' entry on an ongoing game to still block a second ongoing entry")
	}
}

func TestOneShotConflict_LegacyPlayingEntryDoesNotBlockADifferentNewStatus(t *testing.T) {
	existing := []model.PlaythroughEntry{{Status: "playing"}}
	if got := OneShotConflict(existing, "mastered", "", "", "ongoing", ""); got != nil {
		t.Fatalf("a legacy ongoing entry shouldn't block an unrelated mastered entry, got %+v", got)
	}
}

func TestOneShotConflict_AllowsTheSameOneShotStatusOnADifferentSubgame(t *testing.T) {
	// A Shovel Knight: Treasure Trove-style compilation where two different
	// campaigns each have their own ongoing mode on the same platform is two
	// real modes, not fragmentation of one.
	existing := []model.PlaythroughEntry{{Status: "ongoing", Subgame: "Plague of Shadows"}}
	if got := OneShotConflict(existing, "ongoing", "", "", "ongoing", "Specter of Torment"); got != nil {
		t.Fatalf("a different subgame is a separate record, not a conflict, got %+v", got)
	}
}
