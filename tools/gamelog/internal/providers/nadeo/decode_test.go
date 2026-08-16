package nadeo

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.dalton.dog/gamelog/internal/model"
)

// These tests run against **real Nadeo response bodies**, captured with
// `gamelog nadeo fetch --season "Summer 2020" --dump-raw` and trimmed to three
// tracks. Nothing here is written from documentation — that is the whole point
// of the file, and the reason the fixtures should be re-captured rather than
// hand-edited if the API ever changes shape.
//
// The behavioural tests live in nadeo_test.go; these prove decoding.

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// fixtureStub answers every endpoint from the captured payloads.
func fixtureStub(t *testing.T) {
	t.Helper()
	nadeoStub(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/v2/authentication/token/basic"):
			authResponse(w)
		case strings.Contains(r.URL.Path, "/api/campaign/official"):
			// The capture's itemCount is 25 but the fixture holds 2; serve it
			// once and then an empty page, which is the same termination the
			// real paging loop relies on.
			if r.URL.Query().Get("offset") == "0" {
				w.Write(fixture(t, "campaigns_official.json"))
				return
			}
			w.Write([]byte(`{"itemCount":25,"campaignList":[]}`))
		case strings.Contains(r.URL.Path, "/maps/by-uid/"):
			w.Write(fixture(t, "maps_by_uid.json"))
		case strings.Contains(r.URL.Path, "/mapRecords"):
			if r.URL.Query().Get("seasonIdList") != "" {
				w.Write(fixture(t, "season_records.json"))
				return
			}
			w.Write(fixture(t, "alltime_records.json"))
		default:
			t.Errorf("unexpected request %s", r.URL)
		}
	})
}

func TestDecode_CampaignsFromRealResponse(t *testing.T) {
	var page struct {
		ItemCount    int        `json:"itemCount"`
		CampaignList []Campaign `json:"campaignList"`
	}
	if err := json.Unmarshal(fixture(t, "campaigns_official.json"), &page); err != nil {
		t.Fatal(err)
	}
	if page.ItemCount != 25 {
		t.Errorf("itemCount = %d, want 25", page.ItemCount)
	}

	var summer *Campaign
	for i, c := range page.CampaignList {
		if c.Name == "Summer 2020" {
			summer = &page.CampaignList[i]
		}
	}
	if summer == nil {
		t.Fatal("Summer 2020 not decoded")
	}
	if summer.SeasonUID != "3987d489-03ae-4645-9903-8f7679c3a418" {
		t.Errorf("seasonUid = %q", summer.SeasonUID)
	}
	if summer.ID == 0 || summer.StartTimestamp == 0 || summer.EndTimestamp == 0 {
		t.Errorf("campaign scalars not decoded: %+v", *summer)
	}
	if len(summer.Playlist) != 3 {
		t.Fatalf("playlist = %d entries, want the 3 kept in the fixture", len(summer.Playlist))
	}
	if summer.Playlist[0].MapUID == "" {
		t.Error("playlist mapUid not decoded")
	}
	// Positions are what the grid's column-major layout depends on.
	for i, p := range summer.Playlist {
		if p.Position != i {
			t.Errorf("playlist[%d].position = %d", i, p.Position)
		}
	}
}

func TestDecode_MapInfoFromRealResponse(t *testing.T) {
	var maps []MapInfo
	if err := json.Unmarshal(fixture(t, "maps_by_uid.json"), &maps); err != nil {
		t.Fatal(err)
	}
	if len(maps) != 3 {
		t.Fatalf("got %d maps, want 3", len(maps))
	}
	for _, m := range maps {
		if m.MapUID == "" || m.MapID == "" || m.Name == "" {
			t.Errorf("identity fields not decoded: %+v", m)
		}
		// Every medal threshold must be present, or Medal() silently degrades.
		if m.AuthorScore == 0 || m.GoldScore == 0 || m.SilverScore == 0 || m.BronzeScore == 0 {
			t.Errorf("medal thresholds not decoded for %s: %+v", m.Name, m)
		}
		// Thresholds must run fastest-first, which Medal() assumes.
		if !(m.AuthorScore < m.GoldScore && m.GoldScore < m.SilverScore && m.SilverScore < m.BronzeScore) {
			t.Errorf("thresholds out of order for %s: %+v", m.Name, m)
		}
		if m.ThumbnailURL == "" {
			t.Errorf("thumbnailUrl not decoded for %s", m.Name)
		}
	}
}

// The all-time pass is a separate request with a separate filter, and the
// scope it comes back under is the whole reason the two are kept apart.
func TestDecode_AllTimeRecordsFromRealResponse(t *testing.T) {
	var recs []AccountRecord
	if err := json.Unmarshal(fixture(t, "alltime_records.json"), &recs); err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 {
		t.Fatalf("got %d records, want 3", len(recs))
	}
	for _, r := range recs {
		if r.ScopeType != ScopePersonalBest {
			t.Errorf("scopeType = %q, want %q", r.ScopeType, ScopePersonalBest)
		}
		// PersonalBest entries carry a null scopeId, which must decode to empty
		// rather than blowing up the whole response.
		if r.ScopeID != "" {
			t.Errorf("scopeId = %q, want empty for an all-time record", r.ScopeID)
		}
		if !r.Usable() || r.RecordScore.Time <= 0 {
			t.Errorf("record unusable: %+v", r)
		}
	}
}

func TestDecode_AccountRecordsFromRealResponse(t *testing.T) {
	var recs []AccountRecord
	if err := json.Unmarshal(fixture(t, "season_records.json"), &recs); err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 {
		t.Fatalf("got %d records, want 3", len(recs))
	}
	for _, r := range recs {
		if r.MapID == "" || r.MapRecordID == "" {
			t.Errorf("identity fields not decoded: %+v", r)
		}
		if r.RecordScore.Time <= 0 {
			t.Errorf("recordScore.time not decoded: %+v", r)
		}
		if r.Timestamp == "" || recordDay(r.Timestamp) == "" {
			t.Errorf("timestamp not decoded/parseable: %q", r.Timestamp)
		}
		if r.URL == "" {
			t.Errorf("replay url not decoded: %+v", r)
		}
		if !r.Usable() {
			t.Errorf("real record reported unusable: %+v", r)
		}
		// Confirmed against the live capture: campaign maps are TimeAttack, and
		// records fetched by seasonIdList come back scoped to that season.
		if r.GameMode != "TimeAttack" {
			t.Errorf("gameMode = %q, want TimeAttack", r.GameMode)
		}
		if r.ScopeType != "Season" || r.ScopeID != "3987d489-03ae-4645-9903-8f7679c3a418" {
			t.Errorf("scope = %q/%q, want Season scoped to the requested campaign", r.ScopeType, r.ScopeID)
		}
	}
}

// The check that matters most: our medal arithmetic against Nadeo's own grade.
// Across the full 25-track capture these agreed on every single track, which is
// what licenses deriving the medal from thresholds rather than trusting the
// provider's integer.
func TestDecode_DerivedMedalMatchesNadeosOwnGrade(t *testing.T) {
	var maps []MapInfo
	var recs []AccountRecord
	if err := json.Unmarshal(fixture(t, "maps_by_uid.json"), &maps); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(fixture(t, "season_records.json"), &recs); err != nil {
		t.Fatal(err)
	}

	// Nadeo's medal integer, decoded from the live capture.
	nadeoMedals := map[int]string{0: "none", 1: "bronze", 2: "silver", 3: "gold", 4: "author"}

	byID := map[string]MapInfo{}
	for _, m := range maps {
		byID[m.MapID] = m
	}
	checked := 0
	for _, r := range recs {
		m, ok := byID[r.MapID]
		if !ok {
			t.Fatalf("no map for record %s", r.MapID)
		}
		track := model.NadeoTrack{
			AuthorMs: m.AuthorScore, GoldMs: m.GoldScore,
			SilverMs: m.SilverScore, BronzeMs: m.BronzeScore,
			PersonalBestMs: r.RecordScore.Time,
		}
		want, ok := nadeoMedals[r.Medal]
		if !ok {
			t.Fatalf("unmapped medal grade %d — the scale changed", r.Medal)
		}
		if got := track.Medal(); got != want {
			t.Errorf("%s: derived %q, Nadeo says %q (pb %d, author %d, gold %d)",
				m.Name, got, want, r.RecordScore.Time, m.AuthorScore, m.GoldScore)
		}
		checked++
	}
	if checked != 3 {
		t.Fatalf("checked %d records, want 3", checked)
	}
}

// End to end over the captured payloads: the shape the archive actually gets.
func TestFetchRecord_AgainstCapturedResponses(t *testing.T) {
	fixtureStub(t)

	rec, err := FetchRecord(context.Background(), testClient(), FetchOptions{Seasons: []string{"Summer 2020"}})
	if err != nil {
		t.Fatal(err)
	}
	if rec.LastError != "" {
		t.Fatalf("LastError = %q", rec.LastError)
	}
	if len(rec.Campaigns) != 1 {
		t.Fatalf("got %d campaigns, want just the one asked for", len(rec.Campaigns))
	}

	camp := rec.Campaigns[0]
	if camp.Name != "Summer 2020" || camp.Started != "2020-06-23" {
		t.Errorf("campaign = %q started %q", camp.Name, camp.Started)
	}
	if len(camp.Tracks) != 3 {
		t.Fatalf("got %d tracks, want 3", len(camp.Tracks))
	}

	// Summer 2020 - 01: 21.752 against a 21.794 author time — the author medal
	// this archive's very first real capture produced, and the one the
	// hand-written session note for that season also claims.
	first := camp.Tracks[0]
	if first.Name != "Summer 2020 - 01" {
		t.Errorf("name = %q", first.Name)
	}
	if first.SeasonBestMs != 21752 || first.AuthorMs != 21794 {
		t.Errorf("times = season %d / author %d, want 21752 / 21794", first.SeasonBestMs, first.AuthorMs)
	}
	if first.SeasonMedal() != "author" {
		t.Errorf("SeasonMedal() = %q, want author", first.SeasonMedal())
	}
	if first.SeasonDrivenAt != "2020-08-23" {
		t.Errorf("SeasonDrivenAt = %q, want 2020-08-23", first.SeasonDrivenAt)
	}
	if first.MapID == "" || first.RecordID == "" || first.ReplayURL == "" {
		t.Errorf("run metadata missing: %+v", first)
	}

	// In the real capture the two scopes agree on every track of this season —
	// it was never revisited after 2020. That's the equal-time path: the same
	// lap seen through both endpoints, so nothing is reported as improved and
	// the all-time run's identity is what lands on the track.
	if first.PersonalBestMs != first.SeasonBestMs {
		t.Errorf("scopes disagree: all-time %d vs season %d", first.PersonalBestMs, first.SeasonBestMs)
	}
	if first.ImprovedSinceSeason() {
		t.Error("improved reported for a track whose medal never moved")
	}
	if first.Medal() != first.SeasonMedal() {
		t.Errorf("medals disagree: %q vs %q", first.Medal(), first.SeasonMedal())
	}
}
