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
		{"Assassins Creed Odyssey", "Assassin's Creed Odyssey"},
		{"Pokémon Red", "pokemon-red"},
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
// under a different-cased title must still be recognised — and offered as a
// link to that game, not as a duplicate or (as it used to be) silently hidden,
// which left the second service's history uncaptured.
func TestLoggedIndex_OffersLinkWhenTitleMatchesAndProviderUnlinked(t *testing.T) {
	idx := NewLoggedIndex([]model.GameSummary{
		{Slug: "hades-ii", Title: "Hades II"},
		{Slug: "balatro", Title: "Balatro", SteamAppID: "2379780"},
	})

	c := Candidate{Provider: model.ProviderGooglePlay, ID: "588451498103", Title: "BALATRO"}
	idx.place(&c)
	if c.LinkTo == nil || c.LinkTo.Slug != "balatro" {
		t.Errorf("Balatro on Google Play should offer to link to the Steam-logged balatro, got %+v", c.LinkTo)
	}
	c = Candidate{Provider: model.ProviderSteam, ID: "1145350", Title: "Hades II"}
	idx.place(&c)
	if c.LinkTo == nil || c.LinkTo.Slug != "hades-ii" {
		t.Errorf("Hades II should offer to link to the unlinked hades-ii, got %+v", c.LinkTo)
	}
	c = Candidate{Provider: model.ProviderSteam, ID: "1145360", Title: "Hades"}
	idx.place(&c)
	if c.LinkTo != nil || c.Note != "" {
		t.Errorf("Hades (the first game) must not match the logged Hades II, got %+v / %q", c.LinkTo, c.Note)
	}
}

// A title match on a game already linked to a *different* id on the same
// provider is either a second edition (PS4 vs PS5 trophy sets) or a different
// game with the same name. Only a human can say which, so it's a create row
// with a note — never a link, which would overwrite the existing id.
func TestLoggedIndex_NotesTitleClashWithDifferentID(t *testing.T) {
	idx := NewLoggedIndex([]model.GameSummary{
		{Slug: "persona-5-royal", Title: "Persona 5 Royal", PSNID: "NPWR19151_00"},
	})
	c := Candidate{Provider: model.ProviderPSN, ID: "PPSA01234_00", Title: "Persona 5 Royal"}
	idx.place(&c)
	if c.LinkTo != nil {
		t.Fatalf("must not offer to link over an existing psn_id, got %+v", c.LinkTo)
	}
	if !strings.Contains(c.Note, "NPWR19151_00") {
		t.Errorf("note should name the id already linked, got %q", c.Note)
	}
}

func TestLoggedIndex_CoversExternalIDs(t *testing.T) {
	idx := NewLoggedIndex([]model.GameSummary{
		{Slug: "some-game", Title: "Renamed Locally", RAGameID: "104", SteamAppID: "1145360", GooglePlayID: "588451498103", RASubsets: []string{"25709"}},
	})

	for _, tc := range []struct{ provider, id string }{
		{model.ProviderRA, "104"},
		{model.ProviderSteam, "1145360"},
		{model.ProviderGooglePlay, "588451498103"},
		{model.ProviderRA, "25709"},
	} {
		if !idx.covered(tc.provider, tc.id) {
			t.Errorf("%s %s should be covered", tc.provider, tc.id)
		}
	}
	if idx.covered(model.ProviderRA, "999") {
		t.Error("unrelated game should not be covered")
	}
	if idx.covered(model.ProviderSteam, "104") {
		t.Error("coverage is per-provider: an RA ID must not cover a Steam appid")
	}
}

func TestLoggedIndex_SkipsIgnoredGames(t *testing.T) {
	idx := NewLoggedIndex(nil).WithIgnored(model.IgnoredList{Games: []model.IgnoredGame{
		{Provider: model.ProviderSteam, ID: "440"},
		{Provider: model.ProviderXbox, ID: "1480659447"},
	}})

	if !idx.covered(model.ProviderSteam, "440") {
		t.Error("ignored Steam game should be filtered out of the scan")
	}
	if !idx.covered(model.ProviderXbox, "1480659447") {
		t.Error("ignored Xbox game should be filtered out of the scan")
	}
	if idx.covered(model.ProviderSteam, "1480659447") {
		t.Error("an ignore is per-provider")
	}
}

func TestSortCandidates_LinksThenFinishedThenPlaytime(t *testing.T) {
	candidates := []Candidate{
		{Provider: model.ProviderSteam, Title: "Small", PlaytimeMins: 300},
		{Provider: model.ProviderSteam, Title: "Big", PlaytimeMins: 6000},
		{Provider: model.ProviderRA, Title: "Done", Finished: true},
		{Provider: model.ProviderPSN, Title: "Known", LinkTo: &LinkTarget{Slug: "known"}},
	}
	SortCandidates(candidates)

	if !candidates[0].IsLink() {
		t.Errorf("links should sort first, got %q", candidates[0].Title)
	}
	if !candidates[1].Finished {
		t.Errorf("finished games should sort next, got %q", candidates[1].Title)
	}
	if candidates[2].Title != "Big" || candidates[3].Title != "Small" {
		t.Errorf("unfinished games should sort by playtime desc, got %q then %q",
			candidates[2].Title, candidates[3].Title)
	}
}

// A partial achievement count next to "finished" is only coherent when the
// award kind explains it, so Outcome has to name the award.
func TestCandidateOutcome_NamesTheAward(t *testing.T) {
	c := Candidate{
		Provider: model.ProviderRA, Title: "Animal Crossing: City Folk",
		Platform: "Wii", ID: "34566",
		Finished: true, AwardKind: "beaten-hardcore", FinishedOn: "2026-05-02",
		Status:        "finished",
		AchievementsA: 12, AchievementsB: 189,
	}

	if got, want := c.Outcome(), "finished 2026-05-02 (beaten-hardcore)"; got != want {
		t.Errorf("Outcome() = %q, want %q", got, want)
	}
	if got, want := c.Evidence(), "12/189 achievements"; got != want {
		t.Errorf("Evidence() = %q, want %q", got, want)
	}
}

// A "mastered"/"completed" award is a stronger signal than a plain beaten
// award, so the row — and the status it suggests — should say so.
func TestCandidateOutcome_NamesMasteredSeparatelyFromFinished(t *testing.T) {
	c := Candidate{
		Provider: model.ProviderRA, Title: "Earthbound",
		Platform: "SNES", ID: "264",
		Finished: true, AwardKind: "mastered", FinishedOn: "2026-05-02",
		Status:        "mastered",
		AchievementsA: 79, AchievementsB: 79,
	}

	if got, want := c.Outcome(), "mastered 2026-05-02 (mastered)"; got != want {
		t.Errorf("Outcome() = %q, want %q", got, want)
	}
}

// An unfinished candidate has no outcome to claim — the template falls back
// to the suggested status rather than printing a half-built phrase.
func TestCandidateOutcome_EmptyWhenUnfinished(t *testing.T) {
	c := Candidate{Provider: model.ProviderSteam, Title: "Hades", ID: "1145360", Status: "playing", PlaytimeMins: 90, LastActivity: "2026-05-02"}
	if got := c.Outcome(); got != "" {
		t.Errorf("Outcome() = %q, want empty for an unfinished candidate", got)
	}
	if got, want := c.Evidence(), "1.5h · last active 2026-05-02"; got != want {
		t.Errorf("Evidence() = %q, want %q", got, want)
	}
}

// Nothing known means nothing shown — Evidence must not render a bare
// separator or a stray "0h".
func TestCandidateEvidence_EmptyWhenProviderOfferedNothing(t *testing.T) {
	c := Candidate{Provider: model.ProviderSteam, Title: "Never Launched", ID: "1", Status: "backlog"}
	if got := c.Evidence(); got != "" {
		t.Errorf("Evidence() = %q, want empty", got)
	}
}

// "Create as playing instead" on a game the scan thought was finished has to
// drop the inferred finish date too — the date only existed to justify the
// guess being overridden, and keeping it would write a contradiction.
func TestCandidateWithStatus_DropsTheFinishWhenOverriddenToOpenStatus(t *testing.T) {
	c := Candidate{
		Provider: model.ProviderRA, Title: "Banjo-Kazooie", ID: "10210",
		Finished: true, AwardKind: "beaten-hardcore", FinishedOn: "2024-12-04",
		Status: "finished",
	}

	playing := c.WithStatus("playing")
	if playing.FinishedOn != "" || playing.Finished {
		t.Errorf("overriding to playing should drop the finish, got finished=%v on %q", playing.Finished, playing.FinishedOn)
	}
	if got := playing.NewGameFields().Finished; got != "" {
		t.Errorf("front matter finished = %q, want empty", got)
	}

	// mastered still asserts the game is over, so the date it was earned on
	// survives the correction.
	mastered := c.WithStatus("mastered")
	if mastered.FinishedOn != "2024-12-04" || !mastered.Finished {
		t.Errorf("mastered should keep the finish, got %q", mastered.FinishedOn)
	}

	// The original must be untouched: rows are re-derived per request, but a
	// value receiver that mutated shared state would still be a trap.
	if c.Status != "finished" || c.FinishedOn != "2024-12-04" {
		t.Error("WithStatus must not mutate the candidate it was called on")
	}
	if blank := c.WithStatus(""); blank.Status != "finished" {
		t.Errorf("a blank status means keep the guess, got %q", blank.Status)
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
				Provider: model.ProviderRA, Title: "Banjo-Kazooie",
				Platform: "Nintendo 64", ID: "10210",
				Finished: true, AwardKind: "beaten-hardcore",
				FinishedOn: "2024-12-04", Status: "finished",
			},
			wantRA: "10210", wantPlatform: "Nintendo 64",
		},
		{
			name: "steam",
			candidate: Candidate{
				Provider: model.ProviderSteam, Title: "Hollow Knight",
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
	c := Candidate{Provider: model.ProviderSteam, Title: "Hollow Knight", Platform: "PC", ID: "367520", Status: "playing"}
	if _, err := model.CreateGameFile(dir, model.Slugify(c.Title), c.NewGameFields()); err != nil {
		t.Fatal(err)
	}
	if _, err := model.CreateGameFile(dir, model.Slugify(c.Title), c.NewGameFields()); err == nil {
		t.Fatal("expected the second create to refuse to overwrite")
	}
}
