package model

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// track builds a driven track with the medal thresholds used throughout these
// tests: author 20000, gold 21000, silver 23000, bronze 29000.
func track(pos int, uid string, pb int) NadeoTrack {
	return NadeoTrack{
		Position: pos, MapUID: uid, Name: uid,
		AuthorMs: 20000, GoldMs: 21000, SilverMs: 23000, BronzeMs: 29000,
		PersonalBestMs: pb,
	}
}

func season(uid, name string, tracks ...NadeoTrack) NadeoCampaign {
	return NadeoCampaign{SeasonUID: uid, Name: name, Tracks: tracks}
}

func record(campaigns ...NadeoCampaign) *NadeoRecord {
	return &NadeoRecord{
		Provider: ProviderNadeo, Title: "Trackmania", ID: "acct-1",
		Fetched: "2026-08-15", Campaigns: campaigns,
	}
}

// findTrack is the assertion helper: locate a track by season+map so tests
// never depend on slice ordering.
func findTrack(t *testing.T, rec *NadeoRecord, seasonUID, mapUID string) NadeoTrack {
	t.Helper()
	for _, c := range rec.Campaigns {
		if c.SeasonUID != seasonUID {
			continue
		}
		for _, tr := range c.Tracks {
			if tr.MapUID == mapUID {
				return tr
			}
		}
		t.Fatalf("season %s has no track %s", seasonUID, mapUID)
	}
	t.Fatalf("record has no season %s", seasonUID)
	return NadeoTrack{}
}

func TestMedal_ThresholdsAreInclusive(t *testing.T) {
	for _, tc := range []struct {
		pb   int
		want string
	}{
		{0, ""}, // never driven — distinct from "none"
		{19000, "author"},
		{20000, "author"}, // exactly the author time still earns it
		{20001, "gold"},
		{21000, "gold"},
		{22000, "silver"},
		{29000, "bronze"},
		{29001, "none"}, // driven, beat nothing
	} {
		if got := track(0, "m", tc.pb).Medal(); got != tc.want {
			t.Errorf("pb %d: Medal() = %q, want %q", tc.pb, got, tc.want)
		}
	}
}

func TestImprovedSinceSeason(t *testing.T) {
	base := func(season, alltime int) NadeoTrack {
		tr := track(0, "m", alltime)
		tr.SeasonBestMs = season
		return tr
	}
	for _, tc := range []struct {
		name           string
		season, alltim int
		want           bool
	}{
		// 20000 author / 21000 gold / 23000 silver / 29000 bronze.
		{"silver became gold", 22000, 20500, true},
		{"gold became author", 20500, 19000, true},
		{"faster but same medal", 20900, 20100, false},
		{"never revisited", 20500, 20500, false},
		{"no season figure", 0, 20500, false},
		{"never driven", 0, 0, false},
		// A track first driven after its campaign closed has no season time.
		// That's not an improvement — there was nothing to improve on.
		{"driven only after the season", 0, 19000, false},
	} {
		if got := base(tc.season, tc.alltim).ImprovedSinceSeason(); got != tc.want {
			t.Errorf("%s: ImprovedSinceSeason = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestMergeNadeoRecord_SeasonBestMergesOnItsOwnAxis(t *testing.T) {
	archived := track(0, "m1", 20500)
	archived.SeasonBestMs = 22000
	archived.SeasonDrivenAt = "2020-08-23"
	old := record(season("s1", "Fall 2024", archived))

	// A refresh that reached only the all-time endpoint reports no season time.
	partial := track(0, "m1", 20100)
	fresh := record(season("s1", "Fall 2024", partial))

	got := findTrack(t, mergeNadeoRecord(old, fresh), "s1", "m1")
	if got.PersonalBestMs != 20100 {
		t.Errorf("PersonalBestMs = %d, want the improved 20100", got.PersonalBestMs)
	}
	if got.SeasonBestMs != 22000 || got.SeasonDrivenAt != "2020-08-23" {
		t.Errorf("season figures blanked by a fetch that didn't report them: %+v", got)
	}
}

// The rule that is inverted from mergeProvider: for a lap time, lower wins.
func TestMergeNadeoRecord_NeverRegressesAPersonalBest(t *testing.T) {
	old := record(season("s1", "Fall 2024", track(0, "m1", 19976)))
	fresh := record(season("s1", "Fall 2024", track(0, "m1", 24500)))

	got := mergeNadeoRecord(old, fresh)
	if pb := findTrack(t, got, "s1", "m1").PersonalBestMs; pb != 19976 {
		t.Errorf("PersonalBestMs = %d, want the faster 19976 — a slower time must never win", pb)
	}
}

func TestMergeNadeoRecord_TakesAnImprovedTime(t *testing.T) {
	old := record(season("s1", "Fall 2024", track(0, "m1", 24500)))
	improved := track(0, "m1", 19976)
	improved.WorldRank = 7062
	fresh := record(season("s1", "Fall 2024", improved))

	got := findTrack(t, mergeNadeoRecord(old, fresh), "s1", "m1")
	if got.PersonalBestMs != 19976 {
		t.Errorf("PersonalBestMs = %d, want 19976", got.PersonalBestMs)
	}
	if got.WorldRank != 7062 {
		t.Errorf("WorldRank = %d, want 7062 — rank belongs to the run that set the time", got.WorldRank)
	}
}

// The two fetch paths report different halves of a run's metadata: the Core
// records endpoint knows the date and replay, the Live leaderboard knows the
// zone rank. An identical time means the same lap seen twice, so the halves
// must combine rather than overwrite.
func TestMergeNadeoRecord_IdenticalTimeBackfillsFromTheOtherEndpoint(t *testing.T) {
	// Archived via the leaderboard: rank, no date.
	archived := track(0, "m1", 19976)
	archived.WorldRank = 7062
	old := record(season("s1", "Fall 2024", archived))

	// Refetched via account records: same time, date and replay, no rank.
	refetched := track(0, "m1", 19976)
	refetched.DrivenAt = "2024-10-12"
	refetched.RecordID = "run-1"
	refetched.ReplayURL = "https://example.invalid/replay"
	fresh := record(season("s1", "Fall 2024", refetched))

	got := findTrack(t, mergeNadeoRecord(old, fresh), "s1", "m1")
	if got.WorldRank != 7062 {
		t.Errorf("WorldRank = %d, want the archived 7062 kept", got.WorldRank)
	}
	if got.DrivenAt != "2024-10-12" || got.RecordID != "run-1" {
		t.Errorf("run metadata not backfilled: %+v", got)
	}
}

// A better lap is a different lap: its date, rank and replay replace the old
// run's outright, blanks included. Keeping the superseded run's date against a
// new time would be a false statement, not preserved history.
func TestMergeNadeoRecord_ImprovedTimeReplacesRunMetadata(t *testing.T) {
	archived := track(0, "m1", 24500)
	archived.WorldRank = 40000
	archived.DrivenAt = "2021-01-01"
	archived.RecordID = "old-run"
	old := record(season("s1", "Fall 2024", archived))

	// A faster time from the leaderboard path, which carries no date.
	improved := track(0, "m1", 19976)
	improved.WorldRank = 7062
	fresh := record(season("s1", "Fall 2024", improved))

	got := findTrack(t, mergeNadeoRecord(old, fresh), "s1", "m1")
	if got.PersonalBestMs != 19976 || got.WorldRank != 7062 {
		t.Errorf("improved run not taken: %+v", got)
	}
	if got.DrivenAt != "" {
		t.Errorf("DrivenAt = %q, want cleared — that date belonged to the 24.500 run", got.DrivenAt)
	}
	if got.RecordID != "" {
		t.Errorf("RecordID = %q, want cleared — it identifies the superseded lap", got.RecordID)
	}
}

func TestMergeNadeoRecord_KeepsATimeTheProviderNoLongerReports(t *testing.T) {
	old := record(season("s1", "Fall 2024", track(0, "m1", 19976)))
	// The leaderboard call came back with no entry for this map.
	fresh := record(season("s1", "Fall 2024", track(0, "m1", 0)))

	if pb := findTrack(t, mergeNadeoRecord(old, fresh), "s1", "m1").PersonalBestMs; pb != 19976 {
		t.Errorf("PersonalBestMs = %d, want 19976 kept", pb)
	}
}

// A --season fetch reports only the season it was asked for. That must not read
// as "every other season was deleted".
func TestMergeNadeoRecord_PartialFetchKeepsOtherSeasons(t *testing.T) {
	old := record(
		season("s1", "Fall 2024", track(0, "m1", 19976)),
		season("s2", "Summer 2024", track(0, "m2", 24117)),
	)
	fresh := record(season("s1", "Fall 2024", track(0, "m1", 19500)))

	got := mergeNadeoRecord(old, fresh)
	if len(got.Campaigns) != 2 {
		t.Fatalf("got %d campaigns, want 2 — a single-season fetch must not drop the others", len(got.Campaigns))
	}
	if pb := findTrack(t, got, "s2", "m2").PersonalBestMs; pb != 24117 {
		t.Errorf("untouched season's time = %d, want 24117", pb)
	}
}

func TestMergeNadeoRecord_KeepsATrackMissingFromTheResponse(t *testing.T) {
	old := record(season("s1", "Fall 2024", track(0, "m1", 19976), track(1, "m2", 31204)))
	fresh := record(season("s1", "Fall 2024", track(0, "m1", 19976)))

	got := mergeNadeoRecord(old, fresh)
	if n := len(got.Campaigns[0].Tracks); n != 2 {
		t.Fatalf("got %d tracks, want 2 — a track missing from a response is not a deleted track", n)
	}
	if pb := findTrack(t, got, "s1", "m2").PersonalBestMs; pb != 31204 {
		t.Errorf("dropped track's time = %d, want 31204", pb)
	}
}

func TestMergeNadeoRecord_FailedFetchChangesOnlyAttemptMetadata(t *testing.T) {
	old := record(season("s1", "Fall 2024", track(0, "m1", 19976)))
	old.LastAttempt = "2026-08-01"
	fresh := &NadeoRecord{
		Provider: ProviderNadeo, ID: "acct-1",
		LastAttempt: "2026-08-15", LastError: "429 Too Many Requests",
	}

	got := mergeNadeoRecord(old, fresh)
	if got.LastError != "429 Too Many Requests" || got.LastAttempt != "2026-08-15" {
		t.Errorf("attempt metadata not recorded: %+v", got)
	}
	if len(got.Campaigns) != 1 {
		t.Fatalf("got %d campaigns, want the archived 1", len(got.Campaigns))
	}
	if pb := findTrack(t, got, "s1", "m1").PersonalBestMs; pb != 19976 {
		t.Errorf("PersonalBestMs = %d, want 19976 — a failed fetch must not touch data", pb)
	}
	if got.Fetched != "2026-08-15" {
		// Fetched came from the archived record; a failure must not advance it.
		t.Logf("Fetched = %q", got.Fetched)
	}
}

func TestMergeNadeoRecord_KeepsMetadataAZeroFetchDidNotReport(t *testing.T) {
	old := record(season("s1", "Fall 2024", track(0, "m1", 19976)))
	bare := NadeoTrack{Position: 0, MapUID: "m1", PersonalBestMs: 19976}
	fresh := record(season("s1", "Fall 2024", bare))

	got := findTrack(t, mergeNadeoRecord(old, fresh), "s1", "m1")
	if got.AuthorMs != 20000 || got.BronzeMs != 29000 {
		t.Errorf("medal thresholds lost: %+v", got)
	}
	if got.Name != "m1" {
		t.Errorf("Name = %q, want the archived name kept", got.Name)
	}
}

// Two sequential single-season fetches through the real save path — this is
// what `gamelog nadeo fetch --season` does twice, and the union is the only
// thing making it safe.
func TestSaveNadeoRecord_SequentialSeasonFetchesAccumulate(t *testing.T) {
	dir := t.TempDir()

	if _, err := SaveNadeoRecord(dir, "acct-1", record(season("s1", "Fall 2024", track(0, "m1", 19976)))); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveNadeoRecord(dir, "acct-1", record(season("s2", "Summer 2024", track(0, "m2", 24117)))); err != nil {
		t.Fatal(err)
	}

	got, err := LoadNadeoRecord(dir, "acct-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Campaigns) != 2 {
		t.Fatalf("got %d campaigns, want 2", len(got.Campaigns))
	}
	if findTrack(t, got, "s1", "m1").PersonalBestMs != 19976 {
		t.Error("first season's time lost by the second fetch")
	}
	if findTrack(t, got, "s2", "m2").PersonalBestMs != 24117 {
		t.Error("second season's time not saved")
	}
}

func TestLoadNadeoRecord_MissingFileIsNotAnError(t *testing.T) {
	got, err := LoadNadeoRecord(t.TempDir(), "acct-1")
	if err != nil {
		t.Fatalf("nothing captured yet is not an error: %v", err)
	}
	if got != nil {
		t.Errorf("got %+v, want nil", got)
	}
}

func TestWriteCampaignSummary_CountsMedalsAndSkipsUndrivenTracks(t *testing.T) {
	archiveDir, gameDir := t.TempDir(), t.TempDir()
	rec := record(season("s1", "Fall 2024",
		track(0, "m1", 19976), // author
		track(1, "m2", 20500), // gold
		track(2, "m3", 22000), // silver
		track(3, "m4", 40000), // none
		track(4, "m5", 0),     // never driven
	))
	if _, err := SaveNadeoRecord(archiveDir, "acct-1", rec); err != nil {
		t.Fatal(err)
	}

	wrote, err := WriteCampaignSummary(archiveDir, gameDir, "acct-1")
	if err != nil || !wrote {
		t.Fatalf("WriteCampaignSummary = %v, %v", wrote, err)
	}

	got, err := LoadCampaignSummary(gameDir)
	if err != nil {
		t.Fatal(err)
	}
	if got.TracksTotal != 5 || got.TracksDriven != 4 {
		t.Errorf("driven/total = %d/%d, want 4/5", got.TracksDriven, got.TracksTotal)
	}
	want := MedalCounts{Author: 1, Gold: 1, Silver: 1, None: 1}
	if got.Medals != want {
		t.Errorf("medals = %+v, want %+v", got.Medals, want)
	}
	if n := got.Seasons[0].Tracks[0].Number; n != "01" {
		t.Errorf("track number = %q, want %q — the label the game shows", n, "01")
	}
	if got.Seasons[0].Tracks[4].Medal != "" {
		t.Errorf("undriven track medal = %q, want empty", got.Seasons[0].Tracks[4].Medal)
	}
}

// The projection exists so Hugo never reads archive/ — including `raw`.
func TestWriteCampaignSummary_ExcludesRawPayloads(t *testing.T) {
	archiveDir, gameDir := t.TempDir(), t.TempDir()
	rec := record(season("s1", "Fall 2024", track(0, "m1", 19976)))
	rec.Raw = &NadeoRaw{
		Campaigns: []json.RawMessage{json.RawMessage(`{"secret":"campaign-list-blob"}`)},
		Maps:      []json.RawMessage{json.RawMessage(`{"secret":"map-blob"}`)},
		Records:   []json.RawMessage{json.RawMessage(`{"secret":"record-blob"}`)},
	}
	if _, err := SaveNadeoRecord(archiveDir, "acct-1", rec); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteCampaignSummary(archiveDir, gameDir, "acct-1"); err != nil {
		t.Fatal(err)
	}

	blob, err := os.ReadFile(filepath.Join(gameDir, campaignSummaryFilename))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(blob, []byte("secret")) {
		t.Errorf("raw payload leaked into the projection:\n%s", blob)
	}
	var round CampaignSummary
	if err := yaml.Unmarshal(blob, &round); err != nil {
		t.Fatalf("projection is not valid YAML: %v", err)
	}
}

func TestWriteCampaignSummary_RemovesAStaleFile(t *testing.T) {
	archiveDir, gameDir := t.TempDir(), t.TempDir()
	path := filepath.Join(gameDir, campaignSummaryFilename)
	if err := os.WriteFile(path, []byte("account_id: gone\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	wrote, err := WriteCampaignSummary(archiveDir, gameDir, "acct-1")
	if err != nil {
		t.Fatal(err)
	}
	if wrote {
		t.Error("wrote = true with nothing archived")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("stale campaigns.yaml not removed")
	}
}
