package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func unlocked(key, date string) ArchivedAchievement {
	return ArchivedAchievement{Key: key, Name: key, Unlocked: true, Date: date}
}

func locked(key string) ArchivedAchievement {
	return ArchivedAchievement{Key: key, Name: key}
}

// The rule the whole archive rests on: a refresh must never lose an unlock.
func TestMergeProvider_NeverRetractsAnUnlock(t *testing.T) {
	old := &ProviderRecord{
		ID: "1145360", Total: 3,
		Achievements: []ArchivedAchievement{
			unlocked("a", "2020-10-23T22:39:49-05:00"),
			unlocked("b", "2022-12-20T17:06:33-06:00"),
			locked("c"),
		},
	}
	old.summarize()

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

// The exact regression that motivated this: a private profile used to wipe
// the archive and report success.
func TestSaveAchievements_PrivateProfileKeepsExistingData(t *testing.T) {
	dir := t.TempDir()
	if _, err := SaveRecord(dir, providerSteam, "1145360", "Hades", &ProviderRecord{
		ID: "1145360", Total: 49, Fetched: "2026-07-01", Achievements: []ArchivedAchievement{
			unlocked("AchClearTartarus", "2020-10-23T22:39:49-05:00"),
			unlocked("AchWarGod", "2022-12-20T17:06:33-06:00"),
		},
	}); err != nil {
		t.Fatal(err)
	}

	steamStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"playerstats":{"error":"Profile is not public","success":false}}`))
	})
	creds := Credentials{SteamAPIKey: "k", SteamID: testSteamID64}
	if _, err := saveAchievements(context.Background(), dir, "Hades", steamLink("1145360"), creds); err != nil {
		t.Fatalf("refresh errored: %v", err)
	}

	rec, err := LoadRecord(dir, providerSteam, "1145360")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Unlocked != 2 {
		t.Fatalf("DATA LOSS: 2 captured unlocks became %d", rec.Unlocked)
	}
	if rec.LastError == "" {
		t.Error("the failed attempt should be recorded")
	}
	if rec.Fetched != "2026-07-01" {
		t.Errorf("Fetched should not advance on a failed refresh, got %q", rec.Fetched)
	}
}

// A game tracked on both services keeps both histories — now as two files,
// one per provider, neither able to disturb the other.
func TestSaveRecord_KeepsBothProvidersSeparate(t *testing.T) {
	dir := t.TempDir()
	if _, err := SaveRecord(dir, providerSteam, "1", "Some Game", &ProviderRecord{
		ID: "1", Total: 1, Achievements: []ArchivedAchievement{unlocked("s", "2020-01-01T00:00:00-06:00")},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveRecord(dir, providerRA, "2", "Some Game", &ProviderRecord{
		ID: "2", Total: 1, Achievements: []ArchivedAchievement{unlocked("r", "2021-01-01T00:00:00-06:00")},
	}); err != nil {
		t.Fatal(err)
	}

	// Refreshing one provider must not disturb the other.
	if _, err := SaveRecord(dir, providerSteam, "1", "Some Game", &ProviderRecord{
		ID: "1", Total: 2, Fetched: "2026-07-26", Achievements: []ArchivedAchievement{
			unlocked("s", "2020-01-01T00:00:00-06:00"), unlocked("s2", "2026-07-20T00:00:00-05:00"),
		},
	}); err != nil {
		t.Fatal(err)
	}

	ra, err := LoadRecord(dir, providerRA, "2")
	if err != nil {
		t.Fatal(err)
	}
	if ra.Unlocked != 1 {
		t.Error("refreshing Steam disturbed the RetroAchievements record")
	}
	steam, _ := LoadRecord(dir, providerSteam, "1")
	if steam.Unlocked != 2 {
		t.Errorf("new Steam unlock not recorded: %d", steam.Unlocked)
	}

	// The two records are addressed by provider ID, not by any shared name.
	for _, want := range []string{
		filepath.Join(dir, providerSteam, "1.json"),
		filepath.Join(dir, providerRA, "2.json"),
	} {
		if _, err := os.Stat(want); err != nil {
			t.Errorf("expected an archive file at %s: %v", want, err)
		}
	}
}

// The archive must not be reachable from a game's slug: that coupling is what
// let deleting or renaming a game destroy captured history.
func TestSaveRecord_WritesOutsideAnyGameBundle(t *testing.T) {
	root := t.TempDir()
	gamesDir := filepath.Join(root, "content", "games")
	if err := os.MkdirAll(filepath.Join(gamesDir, "hades-ii"), 0o755); err != nil {
		t.Fatal(err)
	}

	archiveDir := findArchiveDir(gamesDir)
	if want := filepath.Join(root, "archive"); archiveDir != want {
		t.Fatalf("findArchiveDir = %q, want %q", archiveDir, want)
	}
	if _, err := SaveRecord(archiveDir, providerRA, "4650", "Hades II", &ProviderRecord{
		ID: "4650", Total: 1, Achievements: []ArchivedAchievement{unlocked("a", "2024-01-01T00:00:00-06:00")},
	}); err != nil {
		t.Fatal(err)
	}

	// Deleting the game must leave the captured history untouched.
	if err := os.RemoveAll(filepath.Join(gamesDir, "hades-ii")); err != nil {
		t.Fatal(err)
	}
	rec, err := LoadRecord(archiveDir, providerRA, "4650")
	if err != nil || rec == nil {
		t.Fatalf("archive did not survive deleting the game: %v", err)
	}
	if rec.Title != "Hades II" {
		t.Errorf("an orphaned record must stay identifiable, got title %q", rec.Title)
	}
}

// New achievements added to a game after the fact must appear without
// disturbing what's already recorded.
func TestMergeProvider_AddsNewlyIntroducedAchievements(t *testing.T) {
	old := &ProviderRecord{Total: 1, Achievements: []ArchivedAchievement{unlocked("a", "2020-01-01T00:00:00-06:00")}}
	old.summarize()
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
	p.summarize()

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

// Dates must round-trip through the RFC3339 parsing Hugo's `time` uses.
func TestStamp_IsParseableRFC3339InSiteZone(t *testing.T) {
	winter := stamp(time.Date(2026, 12, 20, 17, 6, 33, 0, time.UTC))
	summer := stamp(time.Date(2026, 10, 23, 22, 39, 49, 0, time.UTC))

	for _, s := range []string{winter, summer} {
		parsed, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatalf("stamp %q is not RFC3339: %v", s, err)
		}
		if parsed.In(siteLocation).Format("2006-01-02") != s[:10] {
			t.Errorf("date prefix %q disagrees with the instant it encodes", s)
		}
	}
	if winter[19:] == summer[19:] {
		t.Errorf("expected different UTC offsets across DST, both were %q", winter[19:])
	}
}

func TestFetchRARecord_CapturesLockedAndRaw(t *testing.T) {
	raStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
		  "Title":"Kirby","ConsoleName":"Wii","NumAchievements":3,"UserCompletion":"66.67%",
		  "UserTotalPlaytime":1029,
		  "HighestAwardKind":"beaten-hardcore","HighestAwardDate":"2026-07-11T18:55:04+00:00",
		  "Achievements":{
		    "1":{"ID":1,"Title":"First","Description":"d1","Points":1,"BadgeName":"111",
		         "DateEarned":"2026-06-29 14:04:08","DateEarnedHardcore":"2026-06-29 14:04:08"},
		    "2":{"ID":2,"Title":"Locked","Description":"d2","Points":5,"BadgeName":"222"},
		    "3":{"ID":3,"Title":"Softcore","Description":"d3","Points":2,"BadgeName":"333",
		         "DateEarned":"2026-06-30 10:00:00"}
		  }}`))
	})

	client := &RAClient{Username: "u", APIKey: "k", Throttle: time.Nanosecond}
	rec, err := FetchRARecord(context.Background(), client, "104")
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Achievements) != 3 {
		t.Fatalf("locked achievements must be kept, got %d of 3", len(rec.Achievements))
	}
	if rec.Unlocked != 2 || rec.Total != 3 {
		t.Errorf("counts wrong: %d/%d", rec.Unlocked, rec.Total)
	}
	if rec.Platform != "Wii" || rec.Completion != "66.67%" {
		t.Errorf("metadata not captured: %+v", rec)
	}
	// RA reports UserTotalPlaytime in seconds; PlaytimeMins is minutes
	// everywhere else in the archive, so it must be converted, not copied.
	if rec.PlaytimeMins != 17 {
		t.Errorf("playtime = %d mins, want 17 (1029s)", rec.PlaytimeMins)
	}
	if len(rec.Raw) == 0 {
		t.Error("raw response should be retained")
	}
	byKey := map[string]ArchivedAchievement{}
	for _, a := range rec.Achievements {
		byKey[a.Key] = a
	}
	if !byKey["1"].Hardcore {
		t.Error("hardcore unlock should be flagged")
	}
	if byKey["3"].Hardcore {
		t.Error("softcore-only unlock must not be flagged hardcore")
	}
	if byKey["2"].Unlocked {
		t.Error("locked achievement must not be marked unlocked")
	}
	if byKey["2"].Icon != "222" {
		t.Errorf("badge not captured: %q", byKey["2"].Icon)
	}
}

func TestFetchSteamRecord_CapturesPlaytimeBreakdown(t *testing.T) {
	steamStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("l") == "" {
			t.Error("expected a language parameter so names are returned")
		}
		w.Write([]byte(`{"playerstats":{"success":true,"achievements":[
		  {"apiname":"A","achieved":1,"unlocktime":1603510789,"name":"Escaped","description":"d"},
		  {"apiname":"B","achieved":0,"unlocktime":0,"name":"Locked"}
		]}}`))
	})

	owned := &SteamOwnedGame{
		AppID: 1145360, PlaytimeMins: 4400, LastPlayed: 1671577593,
		PlaytimeWindows: 4000, PlaytimeDeck: 400, PlaytimeMac: 0,
	}
	client := &SteamClient{APIKey: "k", SteamID: testSteamID64}
	rec, err := FetchSteamRecord(context.Background(), client, "1145360", owned)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Unlocked != 1 || rec.Total != 2 {
		t.Errorf("counts wrong: %d/%d", rec.Unlocked, rec.Total)
	}
	if rec.PlaytimeMins != 4400 {
		t.Errorf("playtime not captured: %d", rec.PlaytimeMins)
	}
	if rec.Playtime["windows"] != 4000 || rec.Playtime["deck"] != 400 {
		t.Errorf("per-device playtime not captured: %v", rec.Playtime)
	}
	if _, ok := rec.Playtime["mac"]; ok {
		t.Error("zero-playtime devices should be omitted")
	}
	if rec.LastPlayed == "" {
		t.Error("last played not captured")
	}
}

// A failed fetch must produce a record that records the failure rather than
// one that looks like an empty success.
func TestFetchSteamRecord_FailureIsMarked(t *testing.T) {
	steamStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"playerstats":{"error":"Requested app has no stats","success":false}}`))
	})

	client := &SteamClient{APIKey: "k", SteamID: testSteamID64}
	rec, err := FetchSteamRecord(context.Background(), client, "365670", nil)
	if err != nil {
		t.Fatalf("expected no hard error, got %v", err)
	}
	if rec.LastError == "" {
		t.Error("failure should be recorded so a merge knows not to trust it")
	}
	if rec.Fetched != "" {
		t.Error("Fetched must only be set on a successful fetch")
	}
}

func TestLoadRecord_MissingFileIsNil(t *testing.T) {
	rec, err := LoadRecord(t.TempDir(), providerSteam, "239350")
	if err != nil {
		t.Fatal(err)
	}
	if rec != nil {
		t.Errorf("expected no record, got %+v", rec)
	}
}

func TestSaveRecord_WritesValidJSON(t *testing.T) {
	dir := t.TempDir()
	path, err := SaveRecord(dir, providerSteam, "239350", "Spelunky", &ProviderRecord{
		ID: "239350", Total: 2, Achievements: []ArchivedAchievement{
			unlocked("later", "2020-09-26T12:00:00-05:00"),
			unlocked("earlier", "2013-11-27T12:00:00-06:00"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, providerSteam, "239350.json"); path != want {
		t.Errorf("wrote %q, want %q", path, want)
	}
	raw, _ := os.ReadFile(path)
	var back ArchiveRecord
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	if back.Provider != providerSteam || back.ID != "239350" || back.Title != "Spelunky" {
		t.Errorf("identity fields wrong: %+v", back)
	}
	if back.First != "2013-11-27" || back.Last != "2020-09-26" {
		t.Errorf("range wrong: %s to %s", back.First, back.Last)
	}
	if back.Achievements[0].Key != "earlier" {
		t.Error("should persist in timeline order")
	}
}

// `raw` is the whole point of keeping the archive out of the content tree —
// it must survive a round trip rather than being normalised away.
func TestSaveRecord_PreservesRawResponse(t *testing.T) {
	dir := t.TempDir()
	raw := json.RawMessage(`{"UnnormalisedField":42,"Nested":{"a":["b"]}}`)
	if _, err := SaveRecord(dir, providerRA, "4650", "Hades II", &ProviderRecord{
		ID: "4650", Total: 1, Raw: raw,
		Achievements: []ArchivedAchievement{unlocked("a", "2024-01-01T00:00:00-06:00")},
	}); err != nil {
		t.Fatal(err)
	}
	back, err := LoadRecord(dir, providerRA, "4650")
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

// The projection sums both providers rather than picking one, so a
// dual-tracked game's card shows its whole achievement history, not just
// whichever provider happened to be checked first.
func TestWriteAchievementSummary_SumsBothProviders(t *testing.T) {
	archiveDir := t.TempDir()
	gameDir := t.TempDir()
	if _, err := SaveRecord(archiveDir, providerRA, "4650", "Hades II", &ProviderRecord{Total: 40, Achievements: nUnlocked("ra", 12)}); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveRecord(archiveDir, providerSteam, "1145360", "Hades", &ProviderRecord{
		Total: 54, Achievements: nUnlocked("steam", 46), PlaytimeMins: 300,
	}); err != nil {
		t.Fatal(err)
	}

	wrote, err := writeAchievementSummary(archiveDir, gameDir, buildProviderLinks("4650", "1145360", ""))
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
	if _, err := SaveRecord(archiveDir, providerRA, "4650", "Hades II", &ProviderRecord{
		Total: 40, Achievements: nUnlocked("ra", 12), // last unlock 2024-01-01
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveRecord(archiveDir, providerSteam, "1145360", "Hades", &ProviderRecord{
		Total: 54, Achievements: nUnlocked("steam", 46), LastPlayed: "2026-03-15",
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := writeAchievementSummary(archiveDir, gameDir, buildProviderLinks("4650", "1145360", "")); err != nil {
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
	if _, err := SaveRecord(archiveDir, providerSteam, "1145360", "Hades", &ProviderRecord{
		Total: 0, PlaytimeMins: 320,
	}); err != nil {
		t.Fatal(err)
	}

	wrote, err := writeAchievementSummary(archiveDir, gameDir, steamLink("1145360"))
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
	if _, err := SaveRecord(archiveDir, providerSteam, "1145360", "Hades", &ProviderRecord{Total: 54, Achievements: nUnlocked("steam", 46)}); err != nil {
		t.Fatal(err)
	}

	if _, err := writeAchievementSummary(archiveDir, gameDir, steamLink("1145360")); err != nil {
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

	wrote, err := writeAchievementSummary(archiveDir, gameDir, buildProviderLinks("4650", "1145360", ""))
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

// steamLink is the common single-provider case in these tests.
func steamLink(id string) []providerLink {
	return []providerLink{{Provider: providerSteam, ID: id}}
}

// Two platforms are two separate achievement sets. Summing them describes a
// set that doesn't exist (Persona 5 Royal is 53/53 on PS4 and 53/53 on Steam,
// not 106/106 of anything), so the per-provider numbers have to survive into
// the projection for the display to be able to split them.
func TestSummaryKeepsPerProviderBreakdown(t *testing.T) {
	archiveDir := t.TempDir()
	gameDir := t.TempDir()

	if _, err := SaveRecord(archiveDir, providerSteam, "1687950", "Persona 5 Royal",
		&ProviderRecord{Total: 53, Platform: "PC", Achievements: nUnlocked("steam", 53)}); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveRecord(archiveDir, providerPSN, "NPWR19151_00", "Persona 5 Royal",
		&ProviderRecord{Total: 53, Platform: "PS4", Achievements: nUnlocked("psn", 53)}); err != nil {
		t.Fatal(err)
	}

	links := []providerLink{
		{Provider: providerSteam, ID: "1687950"},
		{Provider: providerPSN, ID: "NPWR19151_00"},
	}
	if _, err := writeAchievementSummary(archiveDir, gameDir, links); err != nil {
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
