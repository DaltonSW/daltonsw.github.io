package commands

import (
	"testing"

	"go.dalton.dog/gamelog/internal/model"
	"go.dalton.dog/gamelog/internal/providers/retroachievements"
	"go.dalton.dog/gamelog/internal/providers/steam"
)

// date is defined in suggest_test.go — midnight on the given day, site tz.

func steamReport(started, finished string) SuggestionReport {
	return SuggestionReport{Steam: ProviderResult{Steam: &steam.SteamSuggestion{
		Started: date(started), Finished: date(finished), OK: true,
	}}}
}

func logged(entries ...model.PlaythroughEntry) *model.PlaythroughsFile {
	return &model.PlaythroughsFile{Playthroughs: entries}
}

func TestSuggestSweepVerdict(t *testing.T) {
	cases := []struct {
		name       string
		report     SuggestionReport
		pf         *model.PlaythroughsFile
		actionable bool
	}{
		{
			name:       "neither provider produced a range",
			report:     SuggestionReport{},
			pf:         logged(model.PlaythroughEntry{Started: "2024-01-01", Finished: "2024-02-01"}),
			actionable: false,
		},
		{
			name:       "provider range but nothing logged",
			report:     steamReport("2024-03-01", "2024-04-01"),
			pf:         logged(),
			actionable: true,
		},
		{
			name:       "provider range but nil playthroughs file",
			report:     steamReport("2024-03-01", "2024-04-01"),
			pf:         nil,
			actionable: true,
		},
		{
			name:       "logged entries carry no dates",
			report:     steamReport("2024-03-01", "2024-04-01"),
			pf:         logged(model.PlaythroughEntry{Status: "finished"}),
			actionable: true,
		},
		{
			name:       "provider activity newer than newest logged date",
			report:     steamReport("2024-03-01", "2024-06-01"),
			pf:         logged(model.PlaythroughEntry{Started: "2024-03-01", Finished: "2024-04-01"}),
			actionable: true,
		},
		{
			name:       "provider activity already covered by a logged date",
			report:     steamReport("2024-03-01", "2024-04-01"),
			pf:         logged(model.PlaythroughEntry{Started: "2024-03-01", Finished: "2024-12-31"}),
			actionable: false,
		},
		{
			name:   "session dates count as logged coverage",
			report: steamReport("2024-03-01", "2024-05-01"),
			pf: logged(model.PlaythroughEntry{Sessions: []model.SessionEntry{
				{Started: "2024-02-01", Finished: "2024-02-15"},
				{Started: "2024-05-20", Finished: "2024-06-01"},
			}}),
			actionable: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, note := SuggestSweepVerdict(tc.report, tc.pf)
			if got != tc.actionable {
				t.Fatalf("actionable = %v, want %v (note: %q)", got, tc.actionable, note)
			}
			if note == "" {
				t.Error("expected a non-empty note")
			}
		})
	}
}

func TestSuggestSweepVerdict_UsesLatestAcrossProviders(t *testing.T) {
	// RA range ends earlier than the newest logged date, but Steam's runs
	// past it — the later of the two is what decides.
	r := SuggestionReport{
		RA: ProviderResult{RA: &retroachievements.RASuggestion{
			Started: date("2024-01-01"), Finished: date("2024-02-01"), OK: true,
		}},
		Steam: ProviderResult{Steam: &steam.SteamSuggestion{
			Started: date("2024-06-01"), Finished: date("2024-07-01"), OK: true,
		}},
	}
	pf := logged(model.PlaythroughEntry{Started: "2024-01-01", Finished: "2024-03-01"})

	got, note := SuggestSweepVerdict(r, pf)
	if !got {
		t.Fatalf("expected actionable when Steam activity (2024-07-01) postdates newest logged (2024-03-01); note: %q", note)
	}
}
