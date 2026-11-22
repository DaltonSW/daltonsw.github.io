package commands

import (
	"testing"

	"go.dalton.dog/gamelog/internal/model"
	"go.dalton.dog/gamelog/internal/providers/steam"
)

func TestSteamUnchanged_BlankHiddenWithCachedTextIsChanged(t *testing.T) {
	steam.LocalStatsDir = "../providers/steam/testdata/stats"
	t.Cleanup(func() { steam.LocalStatsDir = "" })

	prev := &model.ArchiveRecord{}
	prev.ID, prev.Fetched = "1392820", "2026-01-01"
	prev.Achievements = []model.ArchivedAchievement{{Key: "Achievement02_Milk1", Hidden: true}}
	if steamUnchanged(prev, &steam.SteamOwnedGame{}) {
		t.Error("blank hidden achievement the cache can fill must not be skipped")
	}

	prev.Achievements[0].Description = "..."
	if !steamUnchanged(prev, &steam.SteamOwnedGame{}) {
		t.Error("already-filled record with unchanged playtime should be skipped")
	}
}
