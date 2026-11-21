package mutate

import (
	"testing"

	"go.dalton.dog/gamelog/internal/model"
)

// OneShotConflict is handleNewPlaythrough's guard, kept out of the HTTP
// handler so the rule itself can be tested directly.

func TestOneShotConflict_RefusesASecondEntryOfTheSameOneShotStatusOnThePlatform(t *testing.T) {
	existing := []model.PlaythroughEntry{
		{Status: "endless", Sessions: []model.SessionEntry{{Started: "2020-01-01"}}},
	}
	if OneShotConflict(existing, "endless", "", "", "") == nil {
		t.Fatal("expected a second endless entry on the same platform to be refused")
	}
}

func TestOneShotConflict_AllowsADifferentStatusToCoexist(t *testing.T) {
	// Hitman: a finished singleplayer campaign alongside an endless
	// Freelancer entry is two real modes, not fragmentation of one.
	existing := []model.PlaythroughEntry{{Status: "endless"}}
	if got := OneShotConflict(existing, "finished", "", "", ""); got != nil {
		t.Fatalf("a finished entry should be able to coexist with an endless one, got conflict with %+v", got)
	}
}

func TestOneShotConflict_AllowsTheSameOneShotStatusOnADifferentPlatform(t *testing.T) {
	existing := []model.PlaythroughEntry{{Status: "multiplayer", Platform: "PS4"}}
	if got := OneShotConflict(existing, "multiplayer", "PC", "", ""); got != nil {
		t.Fatalf("a second platform is a separate record, not a conflict, got %+v", got)
	}
}

func TestOneShotConflict_AllowsTheSameOneShotStatusOnADifferentSubgame(t *testing.T) {
	// A Shovel Knight: Treasure Trove-style compilation where two different
	// campaigns each have their own endless mode on the same platform is two
	// real modes, not fragmentation of one.
	existing := []model.PlaythroughEntry{{Status: "endless", Subgame: "Plague of Shadows"}}
	if got := OneShotConflict(existing, "endless", "", "", "Specter of Torment"); got != nil {
		t.Fatalf("a different subgame is a separate record, not a conflict, got %+v", got)
	}
}
