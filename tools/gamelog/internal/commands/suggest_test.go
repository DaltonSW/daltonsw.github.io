package commands

import (
	"testing"
	"time"

	"go.dalton.dog/gamelog/internal/model"
	"go.dalton.dog/gamelog/internal/providers/retroachievements"
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
	t, err := time.ParseInLocation("2006-01-02", s, model.SiteLocation)
	if err != nil {
		panic(err)
	}
	return t
}

// An unlock late on a Chicago evening is the next day in UTC. The report must
// name the Chicago day, since that's the date that belongs in front matter and
// the date the site itself renders.
func TestDay_UsesSiteTimezoneNotUTC(t *testing.T) {
	evening := time.Date(2026, 1, 5, 21, 30, 0, 0, model.SiteLocation)
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
	parsed, err := retroachievements.ParseRADate("2026-01-06 03:00:00")
	if err != nil {
		t.Fatal(err)
	}
	if got := day(parsed); got != "2026-01-05" {
		t.Errorf("day() = %s, want 2026-01-05", got)
	}
}
