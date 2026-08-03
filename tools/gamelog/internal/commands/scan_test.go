package commands

import (
	"strings"
	"testing"

	"go.dalton.dog/gamelog/internal/model"
)

func TestNormalizeTitle(t *testing.T) {
	cases := []struct{ a, b string }{
		{"Hades II", "hades-ii"},
		{"ELDEN RING", "Elden Ring"},
		{"Elden Ring®", "elden-ring"},
		{"The Binding of Isaac: Rebirth", "the-binding-of-isaac-rebirth"},
		{"Doc Louis's Punch-Out!!", "doc-louis-s-punch-out"},
	}
	for _, tc := range cases {
		if normalizeTitle(tc.a) != normalizeTitle(tc.b) {
			t.Errorf("%q and %q should normalise alike, got %q vs %q",
				tc.a, tc.b, normalizeTitle(tc.a), normalizeTitle(tc.b))
		}
	}
	if normalizeTitle("Hades") == normalizeTitle("Hades II") {
		t.Error("Hades and Hades II must not collide")
	}
}

// Existing games predate the external-ID fields, so a game already logged
// under a different-cased title must still be recognised — otherwise a scan
// offers to create a duplicate of something already on the site.
func TestLoggedIndex_MatchesByTitleWhenNoIDsSet(t *testing.T) {
	idx := NewLoggedIndex([]model.GameSummary{
		{Slug: "hades-ii", Title: "Hades II"},
		{Slug: "elden-ring", Title: "Elden Ring"},
	})

	if !idx.hasSteam("1245620", "ELDEN RING") {
		t.Error("ELDEN RING should match the logged elden-ring entry")
	}
	if !idx.hasSteam("1145350", "Hades II") {
		t.Error("Hades II should match the logged hades-ii entry")
	}
	if idx.hasSteam("1145360", "Hades") {
		t.Error("Hades (the first game) must not match the logged Hades II")
	}
}

func TestLoggedIndex_MatchesByExternalID(t *testing.T) {
	idx := NewLoggedIndex([]model.GameSummary{
		{Slug: "some-game", Title: "Renamed Locally", RAGameID: "104", SteamAppID: "1145360"},
	})

	if !idx.hasRA("104", "Kirby's Return to Dream Land") {
		t.Error("should match on RA ID even when the local title differs")
	}
	if !idx.hasSteam("1145360", "Hades") {
		t.Error("should match on Steam appid even when the local title differs")
	}
	if idx.hasRA("999", "Something Else") {
		t.Error("unrelated game should not match")
	}
}

func TestSortCandidates_FinishedFirstThenPlaytime(t *testing.T) {
	candidates := []Candidate{
		{Provider: "Steam", Title: "Small", PlaytimeMins: 300},
		{Provider: "Steam", Title: "Big", PlaytimeMins: 6000},
		{Provider: "RetroAchievements", Title: "Done", Finished: true},
	}
	SortCandidates(candidates)

	if !candidates[0].Finished {
		t.Errorf("finished games should sort first, got %q", candidates[0].Title)
	}
	if candidates[1].Title != "Big" || candidates[2].Title != "Small" {
		t.Errorf("unfinished games should sort by playtime desc, got %q then %q",
			candidates[1].Title, candidates[2].Title)
	}
}

func TestFormatScanReport_NamesTheAward(t *testing.T) {
	out := FormatScanReport([]Candidate{{
		Provider: "RetroAchievements", Title: "Animal Crossing: City Folk",
		Platform: "Wii", ID: "34566",
		Finished: true, AwardKind: "beaten-hardcore", FinishedOn: "2026-05-02",
		Status:        "finished",
		AchievementsA: 12, AchievementsB: 189,
	}}, 3, ScanOptions{MinHours: 5})

	// A partial achievement count next to "finished" is only coherent when
	// the award kind explains it.
	for _, want := range []string{
		"12/189 achievements",
		"-> finished 2026-05-02 (beaten-hardcore)",
		"draft: true",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q\n---\n%s", want, out)
		}
	}
}

// A "mastered"/"completed" award is a stronger signal than a plain beaten
// award, so the report — and the status it suggests — should say so.
func TestFormatScanReport_NamesMasteredSeparatelyFromFinished(t *testing.T) {
	out := FormatScanReport([]Candidate{{
		Provider: "RetroAchievements", Title: "Earthbound",
		Platform: "SNES", ID: "264",
		Finished: true, AwardKind: "mastered", FinishedOn: "2026-05-02",
		Status:        "mastered",
		AchievementsA: 79, AchievementsB: 79,
	}}, 3, ScanOptions{MinHours: 5})

	if !strings.Contains(out, "-> mastered 2026-05-02 (mastered)") {
		t.Errorf("report should name the mastered award, got:\n%s", out)
	}
}

func TestStatusForAward(t *testing.T) {
	cases := map[string]string{
		"mastered":        "mastered",
		"completed":       "mastered",
		"beaten-hardcore": "finished",
		"beaten-softcore": "finished",
	}
	for kind, want := range cases {
		if got := statusForAward(kind); got != want {
			t.Errorf("statusForAward(%q) = %q, want %q", kind, got, want)
		}
	}
}

func TestFormatScanReport_NoCandidates(t *testing.T) {
	out := FormatScanReport(nil, 3, ScanOptions{MinHours: 5})
	if !strings.Contains(out, "No unlogged games found") {
		t.Errorf("unexpected empty report: %s", out)
	}
	if strings.Contains(out, "draft: true") {
		t.Error("should not mention creation when there's nothing to create")
	}
}

// A generated file must be valid enough for the rest of the tool to load it,
// and must carry the external ID so `suggest` works on it immediately.
func TestCandidate_CreateFileRoundTrips(t *testing.T) {
	cases := []struct {
		name         string
		candidate    Candidate
		wantRA       string
		wantSteam    string
		wantPlatform string
	}{
		{
			name: "retroachievements",
			candidate: Candidate{
				Provider: "RetroAchievements", Title: "Banjo-Kazooie",
				Platform: "Nintendo 64", ID: "10210",
				Finished: true, AwardKind: "beaten-hardcore",
				FinishedOn: "2024-12-04", Status: "finished",
			},
			wantRA: "10210", wantPlatform: "Nintendo 64",
		},
		{
			name: "steam",
			candidate: Candidate{
				Provider: "Steam", Title: "Hollow Knight",
				Platform: "PC", ID: "367520", Status: "playing",
			},
			wantSteam: "367520", wantPlatform: "PC",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			slug := model.Slugify(tc.candidate.Title)
			path, err := model.CreateGameFile(dir, slug, tc.candidate.NewGameFields())
			if err != nil {
				t.Fatal(err)
			}

			doc, err := model.LoadDoc(path)
			if err != nil {
				t.Fatalf("generated file does not parse: %v", err)
			}
			raID, steamID := doc.ExternalIDs()
			if raID != tc.wantRA {
				t.Errorf("retroachievements_id = %q, want %q", raID, tc.wantRA)
			}
			if steamID != tc.wantSteam {
				t.Errorf("steam_appid = %q, want %q", steamID, tc.wantSteam)
			}
			if got := doc.FM.Title; got != tc.candidate.Title {
				t.Errorf("title = %q, want %q", got, tc.candidate.Title)
			}
			if got := doc.FM.Platform; got != tc.wantPlatform {
				t.Errorf("platform = %q, want %q", got, tc.wantPlatform)
			}
			if got := doc.FM.Status; got != tc.candidate.Status {
				t.Errorf("status = %q, want %q", got, tc.candidate.Status)
			}
			if got := doc.FM.Finished; got != tc.candidate.FinishedOn {
				t.Errorf("finished = %q, want %q", got, tc.candidate.FinishedOn)
			}
			if !doc.FM.Draft {
				t.Error("a scanned entry must be created as a draft")
			}
		})
	}
}

// Creating must never clobber an existing entry.
func TestCandidate_CreateFileRefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	c := Candidate{Provider: "Steam", Title: "Hollow Knight", Platform: "PC", ID: "367520", Status: "playing"}
	if _, err := model.CreateGameFile(dir, model.Slugify(c.Title), c.NewGameFields()); err != nil {
		t.Fatal(err)
	}
	if _, err := model.CreateGameFile(dir, model.Slugify(c.Title), c.NewGameFields()); err == nil {
		t.Fatal("expected the second create to refuse to overwrite")
	}
}
