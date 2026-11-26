package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func f(v float64) *float64 { return &v }

func statValue(t *testing.T, stats []ArchivedStat, key string) *float64 {
	t.Helper()
	for _, s := range stats {
		if s.Key == key {
			return s.Value
		}
	}
	t.Fatalf("stat %q missing from %+v", key, stats)
	return nil
}

// A refresh that mentions fewer stats, or none, must not shrink what was kept.
func TestMergeProvider_StatsAreNeverDropped(t *testing.T) {
	old := &ProviderRecord{ID: "1", Stats: []ArchivedStat{
		{Key: "crops", Name: "Crops", Value: f(594)},
		{Key: "fish", Value: f(23)},
	}}
	fresh := &ProviderRecord{ID: "1", Stats: []ArchivedStat{{Key: "crops", Value: f(600)}}}

	got := mergeProvider(old, fresh)
	if len(got.Stats) != 2 {
		t.Fatalf("Stats = %+v, want both keys kept", got.Stats)
	}
	if v := statValue(t, got.Stats, "fish"); v == nil || *v != 23 {
		t.Errorf("fish = %v, want the old 23", v)
	}

	// A fetch that returned no stats at all (error, private profile) is a no-op.
	got = mergeProvider(old, &ProviderRecord{ID: "1", LastError: "boom"})
	if len(got.Stats) != 2 {
		t.Errorf("an errored fetch changed the stats: %+v", got.Stats)
	}
}

// The reading is live: a lower value from Steam replaces a higher archived
// one, because games reset and spend stats. A missing value never does.
func TestMergeProvider_StatValueIsTheLatestReadingButNeverBlank(t *testing.T) {
	old := &ProviderRecord{ID: "1", Stats: []ArchivedStat{
		{Key: "gold", Name: "Gold", Value: f(900)},
		{Key: "deaths", Name: "Deaths", Value: f(4)},
	}}
	fresh := &ProviderRecord{ID: "1", Stats: []ArchivedStat{
		{Key: "gold", Value: f(120)},
		{Key: "deaths"}, // defined, no reading this time
	}}
	got := mergeProvider(old, fresh)
	if v := statValue(t, got.Stats, "gold"); v == nil || *v != 120 {
		t.Errorf("gold = %v, want the fresh 120", v)
	}
	if v := statValue(t, got.Stats, "deaths"); v == nil || *v != 4 {
		t.Errorf("deaths = %v, want the old 4 kept", v)
	}
	for _, s := range got.Stats {
		if s.Key == "gold" && s.Name != "Gold" {
			t.Errorf("a blank fresh name replaced %q", s.Name)
		}
	}
}

// Stats merge even when the achievement half yields nothing, which is the
// whole situation of a game with stats and no achievements.
func TestMergeProvider_StatsMergeWithNoAchievements(t *testing.T) {
	fresh := &ProviderRecord{ID: "1", Fetched: "2026-11-25", Stats: []ArchivedStat{{Key: "k", Value: f(1)}}}
	got := mergeProvider(&ProviderRecord{ID: "1"}, fresh)
	if len(got.Stats) != 1 || got.Fetched != "2026-11-25" {
		t.Errorf("got stats %+v fetched %q", got.Stats, got.Fetched)
	}
}

func TestWriteAchievementSummary_ProjectsReportedStatsOnly(t *testing.T) {
	archiveDir, gameDir := t.TempDir(), t.TempDir()
	if _, err := SaveRecord(archiveDir, ProviderSteam, "219740", "Don't Starve", &ProviderRecord{
		ID: "219740", Fetched: "2026-11-25", Platform: "PC", PlaytimeMins: 820,
		Stats: []ArchivedStat{
			{Key: "furlongs_travelled", Name: "Furlongs Travelled", Value: f(0.8)},
			{Key: "deaths", Name: "Deaths"}, // defined, unreported
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteAchievementSummary(archiveDir, gameDir, steamLink("219740")); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(gameDir, statsSummaryFilename))
	if err != nil {
		t.Fatalf("stats-summary.yaml not written: %v", err)
	}
	got := string(raw)
	for _, want := range []string{"provider: steam", "key: furlongs_travelled", "name: Furlongs Travelled", "value: 0.8"} {
		if !contains(got, want) {
			t.Errorf("projection missing %q:\n%s", want, got)
		}
	}
	if contains(got, "deaths") {
		t.Errorf("an unreported stat leaked into the projection:\n%s", got)
	}
}

// A game whose stats are gone from the archive must not keep serving old ones.
func TestWriteAchievementSummary_RemovesAStaleStatsProjection(t *testing.T) {
	archiveDir, gameDir := t.TempDir(), t.TempDir()
	stale := filepath.Join(gameDir, statsSummaryFilename)
	if err := os.WriteFile(stale, []byte("providers: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteAchievementSummary(archiveDir, gameDir, steamLink("1")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale stats projection not removed: %v", err)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
