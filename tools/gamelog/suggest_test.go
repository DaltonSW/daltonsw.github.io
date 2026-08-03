package main

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCredentials_Configured(t *testing.T) {
	cases := []struct {
		name string
		c    Credentials
		ra   bool
		st   bool
	}{
		{"none set", Credentials{}, false, false},
		{"ra only", Credentials{RAUsername: "u", RAAPIKey: "k"}, true, false},
		{"ra partial", Credentials{RAUsername: "u"}, false, false},
		{"steam only", Credentials{SteamAPIKey: "k", SteamID: "1"}, false, true},
		{"steam partial", Credentials{SteamAPIKey: "k"}, false, false},
		{"both", Credentials{RAUsername: "u", RAAPIKey: "k", SteamAPIKey: "k", SteamID: "1"}, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.c.RAConfigured(); got != tc.ra {
				t.Errorf("RAConfigured() = %v, want %v", got, tc.ra)
			}
			if got := tc.c.SteamConfigured(); got != tc.st {
				t.Errorf("SteamConfigured() = %v, want %v", got, tc.st)
			}
		})
	}
}

// date builds midnight on the given day in the site's timezone, so a report
// built from it reads back as that same day.
func date(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02", s, siteLocation)
	if err != nil {
		panic(err)
	}
	return t
}

// An unlock late on a Chicago evening is the next day in UTC. The report must
// name the Chicago day, since that's the date that belongs in front matter and
// the date the site itself renders.
func TestDay_UsesSiteTimezoneNotUTC(t *testing.T) {
	evening := time.Date(2026, 1, 5, 21, 30, 0, 0, siteLocation)
	if got := evening.UTC().Format("2006-01-02"); got != "2026-01-06" {
		t.Fatalf("precondition: expected this instant to be 2026-01-06 in UTC, got %s", got)
	}
	if got := day(evening); got != "2026-01-05" {
		t.Errorf("day() = %s, want 2026-01-05 (the Chicago day)", got)
	}
}

// RA hands back zoneless UTC strings; those must also land on the Chicago day.
func TestDay_ConvertsParsedRADate(t *testing.T) {
	// 03:00 UTC is still the previous evening in Chicago.
	parsed, err := parseRADate("2026-01-06 03:00:00")
	if err != nil {
		t.Fatal(err)
	}
	if got := day(parsed); got != "2026-01-05" {
		t.Errorf("day() = %s, want 2026-01-05", got)
	}
}

func TestFormatSuggestionReport_BothProvidersSucceed(t *testing.T) {
	r := SuggestionReport{
		Title: "Hades II", Slug: "hades-ii", NumPlaythroughs: 2,
		RAGameID: "4650", RAUsername: "daltonsw",
		RA: ProviderResult{RA: &RASuggestion{
			Started: date("2026-01-05"), Finished: date("2026-03-20"),
			Confidence: "high", AwardKind: "mastered", OK: true,
		}},
		SteamAppID: "1145360",
		Steam: ProviderResult{
			Steam: &SteamSuggestion{
				Started: date("2026-01-04"), Finished: date("2026-03-19"),
				UnlockedCount: 12, TotalCount: 40, OK: true,
			},
			SteamPlaytimeMinutes: 3660, SteamPlaytimeKnown: true,
		},
	}
	out := formatSuggestionReport(r)

	for _, want := range []string{
		`Suggestions for "Hades II" (hades-ii) — 2 playthroughs logged`,
		"RetroAchievements (game 4650, user daltonsw)",
		"confidence: high (mastered)",
		"suggested started : 2026-01-05",
		"suggested finished: 2026-03-20",
		"Steam (appid 1145360)",
		"confidence: low (guess from achievement unlock times, not a real playtime timeline)",
		"achievements unlocked        : 12 / 40",
		"total playtime (all-time)    : 61.0h",
		"suggested finished: 2026-03-19  (weak — not all achievements earned, may still be playing)",
		"this game has multiple playthroughs logged",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q\n---\n%s", want, out)
		}
	}
}

func TestFormatSuggestionReport_SkippedForMissingID(t *testing.T) {
	r := SuggestionReport{
		Title: "Solo Game", Slug: "solo-game", NumPlaythroughs: 1,
		RA:    ProviderResult{Skipped: true, Reason: "no retroachievements_id set on this game"},
		Steam: ProviderResult{Skipped: true, Reason: "no steam_appid set on this game"},
	}
	out := formatSuggestionReport(r)
	if !strings.Contains(out, "no retroachievements_id set on this game, skipping.") {
		t.Errorf("missing RA skip message:\n%s", out)
	}
	if !strings.Contains(out, "no steam_appid set on this game, skipping.") {
		t.Errorf("missing Steam skip message:\n%s", out)
	}
	if strings.Contains(out, "multiple playthroughs") {
		t.Errorf("should not warn about multiple playthroughs with only 1 logged:\n%s", out)
	}
}

func TestFormatSuggestionReport_SkippedForMissingCreds(t *testing.T) {
	r := SuggestionReport{
		Title: "Solo Game", Slug: "solo-game", NumPlaythroughs: 1,
		RAGameID: "123",
		RA:       ProviderResult{Skipped: true, Reason: "RA_USERNAME/RA_API_KEY not set"},
	}
	out := formatSuggestionReport(r)
	if !strings.Contains(out, "RA_USERNAME/RA_API_KEY not set, skipping.") {
		t.Errorf("missing creds skip message:\n%s", out)
	}
}

func TestFormatSuggestionReport_ProviderErrored(t *testing.T) {
	r := SuggestionReport{
		Title: "Solo Game", Slug: "solo-game", NumPlaythroughs: 1,
		RAGameID: "123",
		RA:       ProviderResult{Errored: true, Err: errors.New("network unreachable")},
	}
	out := formatSuggestionReport(r)
	if !strings.Contains(out, "request failed (network unreachable), skipping.") {
		t.Errorf("missing errored message:\n%s", out)
	}
}
