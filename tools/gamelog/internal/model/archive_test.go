package model

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func unlocked(key, date string) ArchivedAchievement {
	return ArchivedAchievement{Key: key, Name: key, Unlocked: true, Date: date}
}

func locked(key string) ArchivedAchievement {
	return ArchivedAchievement{Key: key, Name: key}
}

// steamLink is the common single-provider case in these tests.
func steamLink(id string) []ProviderLink {
	return []ProviderLink{{Provider: ProviderSteam, ID: id}}
}

func TestMergeProvider_NeverRetractsAnUnlock(t *testing.T) {
	old := &ProviderRecord{
		ID: "1145360", Total: 3,
		Achievements: []ArchivedAchievement{
			unlocked("a", "2020-10-23T22:39:49-05:00"),
			unlocked("b", "2022-12-20T17:06:33-06:00"),
			locked("c"),
		},
	}
	old.Summarize()

	// The provider now claims 'a' was never earned.
	fresh := &ProviderRecord{
		ID: "1145360", Total: 3, Fetched: "2026-07-26",
		Achievements: []ArchivedAchievement{locked("a"), unlocked("b", "2022-12-20T17:06:33-06:00"), locked("c")},
	}

	got := mergeProvider(old, fresh)
	byKey := map[string]ArchivedAchievement{}
	for _, a := range got.Achievements {
		byKey[a.Key] = a
	}
	if !byKey["a"].Unlocked {
		t.Error("an unlock recorded once must never be retracted")
	}
	if byKey["a"].Date != "2020-10-23T22:39:49-05:00" {
		t.Errorf("original unlock date lost: %q", byKey["a"].Date)
	}
	if got.Unlocked != 2 {
		t.Errorf("Unlocked = %d, want 2", got.Unlocked)
	}
}
func TestSaveRecord_KeepsBothProvidersSeparate(t *testing.T) {
	dir := t.TempDir()
	if _, err := SaveRecord(dir, ProviderSteam, "1", "Some Game", &ProviderRecord{
		ID: "1", Total: 1, Achievements: []ArchivedAchievement{unlocked("s", "2020-01-01T00:00:00-06:00")},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveRecord(dir, ProviderRA, "2", "Some Game", &ProviderRecord{
		ID: "2", Total: 1, Achievements: []ArchivedAchievement{unlocked("r", "2021-01-01T00:00:00-06:00")},
	}); err != nil {
		t.Fatal(err)
	}

	// Refreshing one provider must not disturb the other.
	if _, err := SaveRecord(dir, ProviderSteam, "1", "Some Game", &ProviderRecord{
		ID: "1", Total: 2, Fetched: "2026-07-26", Achievements: []ArchivedAchievement{
			unlocked("s", "2020-01-01T00:00:00-06:00"), unlocked("s2", "2026-07-20T00:00:00-05:00"),
		},
	}); err != nil {
		t.Fatal(err)
	}

	ra, err := LoadRecord(dir, ProviderRA, "2")
	if err != nil {
		t.Fatal(err)
	}
	if ra.Unlocked != 1 {
		t.Error("refreshing Steam disturbed the RetroAchievements record")
	}
	steam, _ := LoadRecord(dir, ProviderSteam, "1")
	if steam.Unlocked != 2 {
		t.Errorf("new Steam unlock not recorded: %d", steam.Unlocked)
	}

	// The two records are addressed by provider ID, not by any shared name.
	for _, want := range []string{
		filepath.Join(dir, ProviderSteam, "1.json"),
		filepath.Join(dir, ProviderRA, "2.json"),
	} {
		if _, err := os.Stat(want); err != nil {
			t.Errorf("expected an archive file at %s: %v", want, err)
		}
	}
}
func TestSaveRecord_WritesOutsideAnyGameBundle(t *testing.T) {
	root := t.TempDir()
	gamesDir := filepath.Join(root, "content", "games")
	if err := os.MkdirAll(filepath.Join(gamesDir, "hades-ii"), 0o755); err != nil {
		t.Fatal(err)
	}

	archiveDir := FindArchiveDir(gamesDir)
	if want := filepath.Join(root, "archive"); archiveDir != want {
		t.Fatalf("findArchiveDir = %q, want %q", archiveDir, want)
	}
	if _, err := SaveRecord(archiveDir, ProviderRA, "4650", "Hades II", &ProviderRecord{
		ID: "4650", Total: 1, Achievements: []ArchivedAchievement{unlocked("a", "2024-01-01T00:00:00-06:00")},
	}); err != nil {
		t.Fatal(err)
	}

	// Deleting the game must leave the captured history untouched.
	if err := os.RemoveAll(filepath.Join(gamesDir, "hades-ii")); err != nil {
		t.Fatal(err)
	}
	rec, err := LoadRecord(archiveDir, ProviderRA, "4650")
	if err != nil || rec == nil {
		t.Fatalf("archive did not survive deleting the game: %v", err)
	}
	if rec.Title != "Hades II" {
		t.Errorf("an orphaned record must stay identifiable, got title %q", rec.Title)
	}
}
func TestMergeProvider_AddsNewlyIntroducedAchievements(t *testing.T) {
	old := &ProviderRecord{Total: 1, Achievements: []ArchivedAchievement{unlocked("a", "2020-01-01T00:00:00-06:00")}}
	old.Summarize()
	fresh := &ProviderRecord{Total: 2, Fetched: "2026-07-26", Achievements: []ArchivedAchievement{
		unlocked("a", "2020-01-01T00:00:00-06:00"), locked("brand-new"),
	}}

	got := mergeProvider(old, fresh)
	if len(got.Achievements) != 2 || got.Total != 2 {
		t.Fatalf("expected the new achievement to be tracked: %+v", got)
	}
	if got.Unlocked != 1 {
		t.Errorf("Unlocked = %d, want 1", got.Unlocked)
	}
}
func TestProviderRecord_Summarize(t *testing.T) {
	p := &ProviderRecord{Achievements: []ArchivedAchievement{
		unlocked("c", "2026-03-20T22:14:05-05:00"),
		locked("z"),
		unlocked("a", "2026-01-05T10:03:41-06:00"),
	}}
	p.Summarize()

	if p.Unlocked != 2 || p.Total != 3 {
		t.Errorf("counts wrong: %d/%d", p.Unlocked, p.Total)
	}
	if p.First != "2026-01-05" || p.Last != "2026-03-20" {
		t.Errorf("range wrong: %s to %s", p.First, p.Last)
	}
	if p.Achievements[0].Key != "a" || p.Achievements[1].Key != "c" {
		t.Error("unlocked achievements should come first, in date order")
	}
	if p.Achievements[2].Key != "z" {
		t.Error("locked achievements should sort last")
	}
}
func TestLoadRecord_MissingFileIsNil(t *testing.T) {
	rec, err := LoadRecord(t.TempDir(), ProviderSteam, "239350")
	if err != nil {
		t.Fatal(err)
	}
	if rec != nil {
		t.Errorf("expected no record, got %+v", rec)
	}
}
func TestSaveRecord_WritesValidJSON(t *testing.T) {
	dir := t.TempDir()
	path, err := SaveRecord(dir, ProviderSteam, "239350", "Spelunky", &ProviderRecord{
		ID: "239350", Total: 2, Achievements: []ArchivedAchievement{
			unlocked("later", "2020-09-26T12:00:00-05:00"),
			unlocked("earlier", "2013-11-27T12:00:00-06:00"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, ProviderSteam, "239350.json"); path != want {
		t.Errorf("wrote %q, want %q", path, want)
	}
	raw, _ := os.ReadFile(path)
	var back ArchiveRecord
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	if back.Provider != ProviderSteam || back.ID != "239350" || back.Title != "Spelunky" {
		t.Errorf("identity fields wrong: %+v", back)
	}
	if back.First != "2013-11-27" || back.Last != "2020-09-26" {
		t.Errorf("range wrong: %s to %s", back.First, back.Last)
	}
	if back.Achievements[0].Key != "earlier" {
		t.Error("should persist in timeline order")
	}
}
func TestSaveRecord_PreservesRawResponse(t *testing.T) {
	dir := t.TempDir()
	raw := json.RawMessage(`{"UnnormalisedField":42,"Nested":{"a":["b"]}}`)
	if _, err := SaveRecord(dir, ProviderRA, "4650", "Hades II", &ProviderRecord{
		ID: "4650", Total: 1, Raw: raw,
		Achievements: []ArchivedAchievement{unlocked("a", "2024-01-01T00:00:00-06:00")},
	}); err != nil {
		t.Fatal(err)
	}
	back, err := LoadRecord(dir, ProviderRA, "4650")
	if err != nil {
		t.Fatal(err)
	}
	var got, want map[string]any
	if err := json.Unmarshal(back.Raw, &got); err != nil {
		t.Fatalf("raw did not survive: %v", err)
	}
	json.Unmarshal(raw, &want)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("raw changed: %v, want %v", got, want)
	}
}

// nUnlocked builds n distinct unlocked achievements — Unlocked is derived by
// summarize() from the achievement list, not taken at face value, so a
// realistic fixture needs the achievements themselves, not just the count.
func nUnlocked(prefix string, n int) []ArchivedAchievement {
	out := make([]ArchivedAchievement, n)
	for i := range out {
		out[i] = unlocked(fmt.Sprintf("%s-%d", prefix, i), "2024-01-01T00:00:00-06:00")
	}
	return out
}
func TestWriteAchievementSummary_SumsBothProviders(t *testing.T) {
	archiveDir := t.TempDir()
	gameDir := t.TempDir()
	if _, err := SaveRecord(archiveDir, ProviderRA, "4650", "Hades II", &ProviderRecord{Total: 40, Achievements: nUnlocked("ra", 12)}); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveRecord(archiveDir, ProviderSteam, "1145360", "Hades", &ProviderRecord{
		Total: 54, Achievements: nUnlocked("steam", 46), PlaytimeMins: 300,
	}); err != nil {
		t.Fatal(err)
	}

	wrote, err := WriteAchievementSummary(archiveDir, gameDir, BuildProviderLinks("4650", "1145360", ""))
	if err != nil {
		t.Fatal(err)
	}
	if !wrote {
		t.Error("expected wrote == true")
	}
	raw, err := os.ReadFile(achievementSummaryPath(gameDir))
	if err != nil {
		t.Fatal(err)
	}
	var got AchievementSummary
	if err := yaml.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Unlocked != 58 || got.Total != 94 || got.PlaytimeMins != 300 {
		t.Errorf("summary = %+v, want 58/94, playtime_mins=300", got)
	}
}

// last_played prefers Steam's real rtime_last_played over an achievement
// unlock date when both are present and Steam's is more recent — and still
// picks up RA's unlock-date proxy when that's the only signal available.
func TestWriteAchievementSummary_LastPlayedPrefersTheMostRecentSignal(t *testing.T) {
	archiveDir := t.TempDir()
	gameDir := t.TempDir()
	if _, err := SaveRecord(archiveDir, ProviderRA, "4650", "Hades II", &ProviderRecord{
		Total: 40, Achievements: nUnlocked("ra", 12), // last unlock 2024-01-01
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveRecord(archiveDir, ProviderSteam, "1145360", "Hades", &ProviderRecord{
		Total: 54, Achievements: nUnlocked("steam", 46), LastPlayed: "2026-03-15",
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := WriteAchievementSummary(archiveDir, gameDir, BuildProviderLinks("4650", "1145360", "")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(achievementSummaryPath(gameDir))
	if err != nil {
		t.Fatal(err)
	}
	var got AchievementSummary
	if err := yaml.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.LastPlayed != "2026-03-15" {
		t.Errorf("last_played = %q, want Steam's more recent 2026-03-15", got.LastPlayed)
	}
}

// A Steam game with playtime and zero achievements still deserves a summary
// file, not a discarded one.
func TestWriteAchievementSummary_PlaytimeWithNoAchievementsStillWrites(t *testing.T) {
	archiveDir := t.TempDir()
	gameDir := t.TempDir()
	if _, err := SaveRecord(archiveDir, ProviderSteam, "1145360", "Hades", &ProviderRecord{
		Total: 0, PlaytimeMins: 320,
	}); err != nil {
		t.Fatal(err)
	}

	wrote, err := WriteAchievementSummary(archiveDir, gameDir, steamLink("1145360"))
	if err != nil {
		t.Fatal(err)
	}
	if !wrote {
		t.Fatal("expected wrote == true when there's playtime even with no achievements")
	}
	raw, err := os.ReadFile(achievementSummaryPath(gameDir))
	if err != nil {
		t.Fatal(err)
	}
	var got AchievementSummary
	if err := yaml.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.PlaytimeMins != 320 || got.Total != 0 {
		t.Errorf("summary = %+v, want playtime_mins=320 total=0", got)
	}
}

// A provider with nothing archived contributes nothing — a game linked only
// to Steam shouldn't need a phantom RetroAchievements record to work.
func TestWriteAchievementSummary_MissingProviderIsSkipped(t *testing.T) {
	archiveDir := t.TempDir()
	gameDir := t.TempDir()
	if _, err := SaveRecord(archiveDir, ProviderSteam, "1145360", "Hades", &ProviderRecord{Total: 54, Achievements: nUnlocked("steam", 46)}); err != nil {
		t.Fatal(err)
	}

	if _, err := WriteAchievementSummary(archiveDir, gameDir, steamLink("1145360")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(achievementSummaryPath(gameDir))
	if err != nil {
		t.Fatal(err)
	}
	var got AchievementSummary
	if err := yaml.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Unlocked != 46 || got.Total != 54 {
		t.Errorf("summary = %+v, want 46/54", got)
	}
}

// Nothing archived for either provider means nothing to show — and a stale
// summary from a since-cleared or relinked provider must not linger and keep
// displaying an outdated count.
func TestWriteAchievementSummary_RemovesStaleSummaryWhenNothingArchived(t *testing.T) {
	archiveDir := t.TempDir()
	gameDir := t.TempDir()
	path := achievementSummaryPath(gameDir)
	if err := os.WriteFile(path, []byte("unlocked: 9\ntotal: 9\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	wrote, err := WriteAchievementSummary(archiveDir, gameDir, BuildProviderLinks("4650", "1145360", ""))
	if err != nil {
		t.Fatal(err)
	}
	if wrote {
		t.Error("expected wrote == false when nothing is archived")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("expected the stale summary to be removed")
	}
}
func TestSummaryKeepsPerProviderBreakdown(t *testing.T) {
	archiveDir := t.TempDir()
	gameDir := t.TempDir()

	if _, err := SaveRecord(archiveDir, ProviderSteam, "1687950", "Persona 5 Royal",
		&ProviderRecord{Total: 53, Platform: "PC", Achievements: nUnlocked("steam", 53)}); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveRecord(archiveDir, ProviderPSN, "NPWR19151_00", "Persona 5 Royal",
		&ProviderRecord{Total: 53, Platform: "PS4", Achievements: nUnlocked("psn", 53)}); err != nil {
		t.Fatal(err)
	}

	links := []ProviderLink{
		{Provider: ProviderSteam, ID: "1687950"},
		{Provider: ProviderPSN, ID: "NPWR19151_00"},
	}
	if _, err := WriteAchievementSummary(archiveDir, gameDir, links); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(achievementSummaryPath(gameDir))
	if err != nil {
		t.Fatal(err)
	}
	var got AchievementSummary
	if err := yaml.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}

	if len(got.Providers) != 2 {
		t.Fatalf("got %d provider breakdowns, want 2:\n%s", len(got.Providers), raw)
	}
	for _, p := range got.Providers {
		if p.Unlocked != 53 || p.Total != 53 {
			t.Errorf("%s breakdown = %d/%d, want 53/53", p.Provider, p.Unlocked, p.Total)
		}
	}
	if got.Providers[0].Platform != "PC" || got.Providers[1].Platform != "PS4" {
		t.Errorf("platforms = %q/%q, want PC/PS4 — the display labels by platform, not provider",
			got.Providers[0].Platform, got.Providers[1].Platform)
	}
	// The combined figure is still recorded: "how many in all" is a real
	// question, it just isn't the one the games list asks.
	if got.Unlocked != 106 || got.Total != 106 {
		t.Errorf("combined = %d/%d, want 106/106", got.Unlocked, got.Total)
	}
}
