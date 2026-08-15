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

	wrote, err := WriteAchievementSummary(archiveDir, gameDir, BuildProviderLinks("4650", "1145360", "", "", ""))
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

	if _, err := WriteAchievementSummary(archiveDir, gameDir, BuildProviderLinks("4650", "1145360", "", "", "")); err != nil {
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

	wrote, err := WriteAchievementSummary(archiveDir, gameDir, BuildProviderLinks("4650", "1145360", "", "", ""))
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

func TestSplitRASubsetTitle(t *testing.T) {
	cases := []struct{ title, base, subset string }{
		{"Professor Layton and the Last Specter [Subset - Mouse Alley]", "Professor Layton and the Last Specter", "Mouse Alley"},
		{"Some Game [Subset - Bonus Set]", "Some Game", "Bonus Set"},
	}
	for _, tc := range cases {
		base, subset, ok := SplitRASubsetTitle(tc.title)
		if !ok || base != tc.base || subset != tc.subset {
			t.Errorf("SplitRASubsetTitle(%q) = %q/%q/%v, want %q/%q/true", tc.title, base, subset, ok, tc.base, tc.subset)
		}
	}
	for _, title := range []string{
		"Hades II",
		"Sonic & Knuckles [Bonus]",
		"Mega Man (Subset - X)",
		"",
	} {
		if _, _, ok := SplitRASubsetTitle(title); ok {
			t.Errorf("SplitRASubsetTitle(%q) matched, want no match", title)
		}
	}
}

// A subset is a bonus achievement set for the same game, not a second copy of
// it: folding Mouse Alley's 52 into Last Specter's 40 would restate the game
// as 24%% complete when the game itself is 40%% complete, and describe a set of
// 92 achievements that exists nowhere.
func TestWriteAchievementSummary_KeepsSubsetsOutOfTheHeadlineTotals(t *testing.T) {
	archiveDir := t.TempDir()
	gameDir := t.TempDir()

	if _, err := SaveRecord(archiveDir, ProviderRA, "7601", "Professor Layton and the Last Specter",
		&ProviderRecord{Total: 40, Platform: "Nintendo DS", PlaytimeMins: 1449, Achievements: nUnlocked("base", 16)}); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveRecord(archiveDir, ProviderRA, "25709", "Professor Layton and the Last Specter [Subset - Mouse Alley]",
		&ProviderRecord{Total: 52, Platform: "Nintendo DS", PlaytimeMins: 202, Achievements: nUnlocked("sub", 6)}); err != nil {
		t.Fatal(err)
	}

	links := BuildProviderLinks("7601", "", "", "", "", "25709")
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

	if got.Unlocked != 16 || got.Total != 40 {
		t.Errorf("headline = %d/%d, want the base set's 16/40:\n%s", got.Unlocked, got.Total, raw)
	}
	if len(got.Providers) != 1 {
		t.Errorf("got %d provider breakdowns, want 1 — a subset is not a second place it was played", len(got.Providers))
	}
	// Per-id playtime would double-count the same hours if summed.
	if got.PlaytimeMins != 1449 {
		t.Errorf("playtime_mins = %d, want the base set's 1449", got.PlaytimeMins)
	}
	if len(got.Subsets) != 1 {
		t.Fatalf("got %d subsets, want 1:\n%s", len(got.Subsets), raw)
	}
	sub := got.Subsets[0]
	if sub.Name != "Mouse Alley" || sub.ID != "25709" || sub.Unlocked != 6 || sub.Total != 52 || sub.PlaytimeMins != 202 {
		t.Errorf("subset = %+v, want Mouse Alley 25709 6/52 202m", sub)
	}

	// The unlocks themselves are still earned achievements, so they stay in
	// the one flat feed — tagged, so a view can group or label them.
	var tagged int
	for _, e := range got.Earned {
		if e.Subset == "Mouse Alley" {
			tagged++
		}
	}
	if len(got.Earned) != 22 || tagged != 6 {
		t.Errorf("earned = %d entries (%d tagged), want 22 (6 tagged)", len(got.Earned), tagged)
	}
}

func TestBuildProviderLinks_PutsSubsetsBehindTheirBaseSet(t *testing.T) {
	links := BuildProviderLinks("7601", "1145360", "", "", "", "25709", "25710")
	want := []ProviderLink{
		{Provider: ProviderRA, ID: "7601"},
		{Provider: ProviderRA, ID: "25709", Subset: true},
		{Provider: ProviderRA, ID: "25710", Subset: true},
		{Provider: ProviderSteam, ID: "1145360"},
	}
	if len(links) != len(want) {
		t.Fatalf("got %+v, want %+v", links, want)
	}
	for i := range want {
		if links[i] != want[i] {
			t.Errorf("link %d = %+v, want %+v", i, links[i], want[i])
		}
	}

	// A subset id that duplicates the base set would archive the same record
	// twice and count it twice in the summary.
	if got := BuildProviderLinks("7601", "", "", "", "", "7601"); len(got) != 1 {
		t.Errorf("got %+v, want the base link only", got)
	}
}
