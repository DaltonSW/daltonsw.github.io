package main

import (
	"strings"
	"testing"
	"time"
)

func daysAgo(n int) string {
	return time.Now().In(siteLocation).AddDate(0, 0, -n).Format("2006-01-02")
}

// A game qualifies once it's marked "playing", linked to Steam, and quiet
// past the threshold — draft status doesn't exclude it (stale is meant to
// help pre-classify the draft backlog too), but every other state has
// nothing to suggest, including ongoing games, which are supposed to
// stay "playing" indefinitely by design.
func TestFindStaleCandidates_FiltersToEligibleGames(t *testing.T) {
	archiveDir := t.TempDir()
	if _, err := SaveRecord(archiveDir, providerSteam, "1", "Quiet Game", &ProviderRecord{
		Total: 10, Achievements: nUnlocked("a", 3), PlaytimeMins: 120, LastPlayed: daysAgo(45),
	}); err != nil {
		t.Fatal(err)
	}

	games := []GameSummary{
		{Slug: "quiet", Title: "Quiet Game", Status: "playing", SteamAppID: "1"},
		{Slug: "draft-game", Title: "Draft Game", Status: "playing", SteamAppID: "1", Draft: true},
		{Slug: "finished-game", Title: "Finished Game", Status: "finished", SteamAppID: "1"},
		{Slug: "session-game", Title: "Session Game", Status: "ongoing", SteamAppID: "1"},
		{Slug: "ra-only", Title: "RA Only Game", Status: "playing", RAGameID: "4650"},
	}

	got, err := findStaleCandidates(archiveDir, games, 30)
	if err != nil {
		t.Fatal(err)
	}
	gotSlugs := map[string]bool{}
	for _, c := range got {
		gotSlugs[c.Game.Slug] = true
	}
	want := map[string]bool{"quiet": true, "draft-game": true}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %+v", want, got)
	}
	for slug := range want {
		if !gotSlugs[slug] {
			t.Errorf("expected %q among candidates, got %+v", slug, got)
		}
	}
}

func TestFindStaleCandidates_RecentlyPlayedIsExcluded(t *testing.T) {
	archiveDir := t.TempDir()
	if _, err := SaveRecord(archiveDir, providerSteam, "1", "Recent Game", &ProviderRecord{
		Total: 10, Achievements: nUnlocked("a", 3), LastPlayed: daysAgo(5),
	}); err != nil {
		t.Fatal(err)
	}
	games := []GameSummary{{Slug: "recent", Status: "playing", SteamAppID: "1"}}

	got, err := findStaleCandidates(archiveDir, games, 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected a recently played game to be excluded, got %+v", got)
	}
}

// High completion suggests finished at medium confidence; partial
// completion suggests dropped; no achievement data at all still suggests
// dropped (the safer default) but at low confidence, since playtime alone
// can't prove completion.
func TestFindStaleCandidates_SuggestsByCompletion(t *testing.T) {
	cases := []struct {
		name            string
		total, unlocked int
		wantStatus      string
		wantConfidence  string
	}{
		{"100% completion", 10, 10, "mastered", "medium"},
		{"high but not full completion", 10, 9, "finished", "medium"},
		{"partial completion", 10, 3, "dropped", "medium"},
		{"no achievements tracked", 0, 0, "dropped", "low"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			archiveDir := t.TempDir()
			if _, err := SaveRecord(archiveDir, providerSteam, "1", "Game", &ProviderRecord{
				Total: tc.total, Achievements: nUnlocked("a", tc.unlocked), LastPlayed: daysAgo(45),
			}); err != nil {
				t.Fatal(err)
			}
			games := []GameSummary{{Slug: "g", Status: "playing", SteamAppID: "1"}}

			got, err := findStaleCandidates(archiveDir, games, 30)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 {
				t.Fatalf("expected 1 candidate, got %d", len(got))
			}
			if got[0].SuggestedStatus != tc.wantStatus || got[0].Confidence != tc.wantConfidence {
				t.Errorf("got status=%s confidence=%s, want %s/%s",
					got[0].SuggestedStatus, got[0].Confidence, tc.wantStatus, tc.wantConfidence)
			}
		})
	}
}

func TestFormatStaleCard_ShowsSuggestionAndConfidence(t *testing.T) {
	c := StaleCandidate{
		Game:       GameSummary{Title: "Hades", SteamAppID: "1145360"},
		LastPlayed: "2026-01-01", DaysSince: 47,
		Unlocked: 12, Total: 40, PlaytimeMins: 300,
		SuggestedStatus: "dropped", Confidence: "medium",
	}
	card := formatStaleCard(c)
	for _, want := range []string{"Hades", "Steam 1145360", "47 days ago", "12/40", "5.0h", "dropped (medium confidence)"} {
		if !strings.Contains(card, want) {
			t.Errorf("card missing %q, got:\n%s", want, card)
		}
	}
	if strings.Contains(card, "[draft]") {
		t.Errorf("published game's card shouldn't be tagged [draft], got:\n%s", card)
	}
}

// A draft candidate is tagged so it's clear during the loop that accepting
// a suggestion here pre-classifies a backlog entry, not a published game.
func TestFormatStaleCard_TagsDraftGames(t *testing.T) {
	c := StaleCandidate{
		Game:            GameSummary{Title: "Hades", SteamAppID: "1145360", Draft: true},
		SuggestedStatus: "dropped", Confidence: "medium",
	}
	if card := formatStaleCard(c); !strings.Contains(card, "[draft]") {
		t.Errorf("draft game's card should be tagged [draft], got:\n%s", card)
	}
}
