package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"go.dalton.dog/gamelog/internal/model"
)

// The picker's list is the stored campaign list joined with what's archived:
// a listed season with nothing archived still shows (it can be backfilled),
// and an archived season the stored list lacks isn't dropped.
func TestNadeoSeasons_JoinsStoredListWithArchive(t *testing.T) {
	root := t.TempDir()
	gamesDir := filepath.Join(root, "content", "games")
	gameDir := filepath.Join(gamesDir, "trackmania")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fm := "---\ntitle: Trackmania\nnadeo_account_id: acct\n---\n"
	if err := os.WriteFile(filepath.Join(gameDir, "_index.md"), []byte(fm), 0o644); err != nil {
		t.Fatal(err)
	}

	page := `{"itemCount":2,"campaignList":[
		{"seasonUid":"s-fall","name":"Fall 2020","startTimestamp":1601564400,"playlist":[{"mapUid":"a"},{"mapUid":"b"}]},
		{"seasonUid":"s-summer","name":"Summer 2020","startTimestamp":1592924400,"playlist":[{"mapUid":"c"}]}]}`
	rec := &model.NadeoRecord{
		Provider: model.ProviderNadeo, ID: "acct", Fetched: "2026-08-15",
		Raw: &model.NadeoRaw{Campaigns: []json.RawMessage{json.RawMessage(page)}},
		Campaigns: []model.NadeoCampaign{
			{SeasonUID: "s-summer", Name: "Summer 2020", Started: "2020-06-23", Tracks: []model.NadeoTrack{
				{MapUID: "c", PersonalBestMs: 30000},
			}},
			{SeasonUID: "s-old", Name: "Old Season", Started: "2019-01-01", Tracks: []model.NadeoTrack{
				{MapUID: "x"}, {MapUID: "y", PersonalBestMs: 41000},
			}},
		},
	}
	if _, err := model.SaveNadeoRecord(model.FindArchiveDir(gamesDir), "acct", rec); err != nil {
		t.Fatal(err)
	}

	got, err := NadeoSeasons(gamesDir)
	if err != nil {
		t.Fatal(err)
	}
	want := []NadeoSeason{
		{SeasonUID: "s-fall", Name: "Fall 2020", Started: "2020-10-01", Tracks: 2},
		{SeasonUID: "s-summer", Name: "Summer 2020", Started: "2020-06-23", Tracks: 1, Driven: 1, Archived: true},
		{SeasonUID: "s-old", Name: "Old Season", Started: "2019-01-01", Tracks: 2, Driven: 1, Archived: true},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d rows, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
