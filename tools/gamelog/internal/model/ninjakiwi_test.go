package model

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMergeNinjaKiwiRecord_FailedFetchKeepsExistingData(t *testing.T) {
	old := &BTD6Record{
		Provider: ProviderNinjaKiwi, ID: "u1", Fetched: "2026-08-01",
		XP: 1000, MonkeyMoney: 500,
		UnlockedTowers: map[string]bool{"DartMonkey": true},
	}
	fresh := &BTD6Record{
		Provider: ProviderNinjaKiwi, ID: "u1", LastAttempt: "2026-08-15",
		LastError: "token expired",
	}

	got := mergeNinjaKiwiRecord(old, fresh)

	if got.LastError != "token expired" || got.LastAttempt != "2026-08-15" {
		t.Fatalf("attempt metadata not updated: %+v", got)
	}
	if got.XP != 1000 || got.MonkeyMoney != 500 {
		t.Fatalf("a failed fetch must not touch existing data, got %+v", got)
	}
	if !got.UnlockedTowers["DartMonkey"] {
		t.Fatalf("unlock map lost on failed fetch: %+v", got.UnlockedTowers)
	}
}

func TestMergeNinjaKiwiRecord_CumulativeStatsNeverRegress(t *testing.T) {
	old := &BTD6Record{
		Provider: ProviderNinjaKiwi, ID: "u1", Fetched: "2026-08-01",
		XP: 100000, Rank: 50, HighestSeenRound: 120, GamesPlayed: 900,
	}
	// A fetch reporting smaller cumulative numbers than what's archived —
	// e.g. a stale/rate-limited response — must not roll history backward.
	fresh := &BTD6Record{
		Provider: ProviderNinjaKiwi, ID: "u1", Fetched: "2026-08-15",
		XP: 90000, Rank: 40, HighestSeenRound: 100, GamesPlayed: 850,
	}

	got := mergeNinjaKiwiRecord(old, fresh)

	if got.XP != 100000 || got.Rank != 50 || got.HighestSeenRound != 120 || got.GamesPlayed != 900 {
		t.Fatalf("cumulative stat regressed: %+v", got)
	}

	// And a genuine increase does move forward.
	fresh2 := &BTD6Record{
		Provider: ProviderNinjaKiwi, ID: "u1", Fetched: "2026-08-20",
		XP: 110000, Rank: 51, HighestSeenRound: 130, GamesPlayed: 910,
	}
	got2 := mergeNinjaKiwiRecord(got, fresh2)
	if got2.XP != 110000 || got2.Rank != 51 || got2.HighestSeenRound != 130 || got2.GamesPlayed != 910 {
		t.Fatalf("cumulative stat failed to advance: %+v", got2)
	}
}

// TestMergeNinjaKiwiRecord_PointInTimeFieldsTakeFreshValue guards the one
// place a *smaller* fresh value is the correct outcome, unlike every
// cumulative field above — accidentally applying max-merge here would make
// spent currency and consumed insta-monkeys silently reappear.
func TestMergeNinjaKiwiRecord_PointInTimeFieldsTakeFreshValue(t *testing.T) {
	old := &BTD6Record{
		Provider: ProviderNinjaKiwi, ID: "u1", Fetched: "2026-08-01",
		MonkeyMoney: 5000, Trophies: 300,
		InstaTowers: map[string]map[string]int{"DartMonkey": {"220": 3}},
		Powers:      map[string]BTD6Power{"CashDrop": {Quantity: 10}},
	}
	fresh := &BTD6Record{
		Provider: ProviderNinjaKiwi, ID: "u1", Fetched: "2026-08-15",
		MonkeyMoney: 1200, Trophies: 250, // spent money, lost a PvP match
		InstaTowers: map[string]map[string]int{"DartMonkey": {"220": 1}}, // used two
		Powers:      map[string]BTD6Power{"CashDrop": {Quantity: 4}},     // used some
	}

	got := mergeNinjaKiwiRecord(old, fresh)

	if got.MonkeyMoney != 1200 {
		t.Fatalf("MonkeyMoney should take the fresh (smaller) value, got %d", got.MonkeyMoney)
	}
	if got.Trophies != 250 {
		t.Fatalf("Trophies should take the fresh (smaller) value, got %d", got.Trophies)
	}
	if got.InstaTowers["DartMonkey"]["220"] != 1 {
		t.Fatalf("InstaTowers should take the fresh (smaller) value, got %+v", got.InstaTowers)
	}
	if got.Powers["CashDrop"].Quantity != 4 {
		t.Fatalf("Powers quantity should take the fresh (smaller) value, got %+v", got.Powers)
	}
}

func TestMergeNinjaKiwiRecord_UnlockMapsOnlyGrow(t *testing.T) {
	old := &BTD6Record{
		Provider: ProviderNinjaKiwi, ID: "u1", Fetched: "2026-08-01",
		UnlockedTowers: map[string]bool{"DartMonkey": true, "Sniper": true},
	}
	// A fetch that's missing a tower already known to be unlocked (e.g. a
	// truncated response) must not un-unlock it.
	fresh := &BTD6Record{
		Provider: ProviderNinjaKiwi, ID: "u1", Fetched: "2026-08-15",
		UnlockedTowers: map[string]bool{"Sniper": true, "NinjaMonkey": true},
	}

	got := mergeNinjaKiwiRecord(old, fresh)

	for _, tower := range []string{"DartMonkey", "Sniper", "NinjaMonkey"} {
		if !got.UnlockedTowers[tower] {
			t.Fatalf("%s should stay unlocked, got %+v", tower, got.UnlockedTowers)
		}
	}
}

func TestSaveAndLoadNinjaKiwiRecord_RoundTrips(t *testing.T) {
	archiveDir := filepath.Join(t.TempDir(), "archive")

	rec := &BTD6Record{
		Provider: ProviderNinjaKiwi, Title: "Bloons TD 6", ID: "u1",
		Fetched: "2026-08-01", XP: 1000,
		UnlockedTowers: map[string]bool{"DartMonkey": true},
	}
	if _, err := SaveNinjaKiwiRecord(archiveDir, "u1", rec); err != nil {
		t.Fatalf("SaveNinjaKiwiRecord: %v", err)
	}

	loaded, err := LoadNinjaKiwiRecord(archiveDir, "u1")
	if err != nil {
		t.Fatalf("LoadNinjaKiwiRecord: %v", err)
	}
	if loaded == nil || loaded.XP != 1000 || !loaded.UnlockedTowers["DartMonkey"] {
		t.Fatalf("round trip lost data: %+v", loaded)
	}

	// A second fetch with less data (e.g. rate-limited) must not shrink the
	// archive.
	partial := &BTD6Record{
		Provider: ProviderNinjaKiwi, ID: "u1", LastError: "rate limited",
	}
	if _, err := SaveNinjaKiwiRecord(archiveDir, "u1", partial); err != nil {
		t.Fatalf("SaveNinjaKiwiRecord (partial): %v", err)
	}
	stillThere, err := LoadNinjaKiwiRecord(archiveDir, "u1")
	if err != nil {
		t.Fatalf("LoadNinjaKiwiRecord: %v", err)
	}
	if stillThere.XP != 1000 || !stillThere.UnlockedTowers["DartMonkey"] {
		t.Fatalf("a failed refresh must not lose data: %+v", stillThere)
	}
	if stillThere.LastError != "rate limited" {
		t.Fatalf("expected LastError to record the failed attempt, got %+v", stillThere)
	}
}

func TestWriteBTD6Summary_NoDataMeansNoFile(t *testing.T) {
	dir := t.TempDir()
	archiveDir := filepath.Join(dir, "archive")
	gameDir := filepath.Join(dir, "content", "games", "bloons-td-6")

	wrote, err := WriteBTD6Summary(archiveDir, gameDir, "")
	if err != nil {
		t.Fatalf("WriteBTD6Summary: %v", err)
	}
	if wrote {
		t.Fatalf("expected no summary written for an unlinked game")
	}
}

func TestWriteBTD6Summary_ProjectsArchivedData(t *testing.T) {
	dir := t.TempDir()
	archiveDir := filepath.Join(dir, "archive")
	gameDir := filepath.Join(dir, "content", "games", "bloons-td-6")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	rec := &BTD6Record{
		Provider: ProviderNinjaKiwi, ID: "u1", Fetched: "2026-08-01",
		XP: 1000, Rank: 10, PrimaryHero: "Sauda",
		UnlockedTowers:      map[string]bool{"DartMonkey": true, "Sniper": false},
		TowerXP:             map[string]int{"DartMonkey": 500},
		UnlockedHeroes:      map[string]bool{"Sauda": true, "Quincy": false},
		AchievementsClaimed: []string{"First Win", "Hero Time"},
		InstaTowers:         map[string]map[string]int{"DartMonkey": {"220": 2}, "Sniper": {"000": 0}},
	}
	if _, err := SaveNinjaKiwiRecord(archiveDir, "u1", rec); err != nil {
		t.Fatalf("SaveNinjaKiwiRecord: %v", err)
	}

	wrote, err := WriteBTD6Summary(archiveDir, gameDir, "u1")
	if err != nil {
		t.Fatalf("WriteBTD6Summary: %v", err)
	}
	if !wrote {
		t.Fatalf("expected a summary to be written")
	}

	summary, err := LoadBTD6Summary(gameDir)
	if err != nil {
		t.Fatalf("LoadBTD6Summary: %v", err)
	}
	if summary == nil {
		t.Fatalf("expected a summary")
	}
	if summary.XP != 1000 || summary.AchievementsCount != 2 || summary.PrimaryHero != "Sauda" {
		t.Fatalf("unexpected headline stats: %+v", summary)
	}
	if len(summary.InstaMonkeys) != 1 || summary.InstaMonkeys[0].Tower != "DartMonkey" {
		t.Fatalf("expected only towers with nonzero insta-monkeys, got %+v", summary.InstaMonkeys)
	}
}
