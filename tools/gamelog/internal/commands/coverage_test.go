package commands

import (
	"testing"
	"time"

	"go.dalton.dog/gamelog/internal/model"
	"go.dalton.dog/gamelog/internal/providers/retroachievements"
)

func TestLoggedSpansCovers(t *testing.T) {
	pf := logged(
		model.PlaythroughEntry{Sessions: []model.SessionEntry{{Started: "2018-09", Finished: "2018-10"}}},  // partial dates
		model.PlaythroughEntry{Status: "dropped", Sessions: []model.SessionEntry{{Started: "2020-01-01"}}}, // open, bounded by the next run
		model.PlaythroughEntry{Sessions: []model.SessionEntry{{Started: "2021-06-01", Finished: "2021-06-30"}}},
		model.PlaythroughEntry{Status: "planned", Sessions: []model.SessionEntry{{Started: "2025-01-01"}}}, // hasn't happened
		model.PlaythroughEntry{Sessions: []model.SessionEntry{{Started: "2023-02-01"}}},                    // open, newest: unbounded
	)
	spans := loggedSpans(pf)
	cases := map[string]bool{
		"2018-08-31": false,
		"2018-09-14": true,
		"2018-10-31": true,
		"2018-11-01": false,
		"2020-07-04": true,
		"2021-06-15": true,
		"2021-07-01": false, // the open 2020 run stopped when the 2021 one started
		"2022-12-31": false,
		"2025-01-02": true, // inside the open 2023 run, not the planned entry
		"2030-01-01": true,
	}
	for d, want := range cases {
		if got := spans.covers(d) >= 0; got != want {
			t.Errorf("covers(%s) = %v, want %v", d, got, want)
		}
	}
}

func TestWithCoverage_RAAwardInsideLoggedRunDowngrades(t *testing.T) {
	r := SuggestionReport{RA: ProviderResult{RA: &retroachievements.RASuggestion{
		Started: date("2020-01-02"), Finished: date("2020-01-20"), Confidence: "high", AwardKind: "mastered", OK: true,
		Unlocks: []time.Time{date("2020-01-02"), date("2020-01-19"), date("2022-05-05")},
	}}}
	pf := logged(model.PlaythroughEntry{Sessions: []model.SessionEntry{{Started: "2020-01-01", Finished: "2020-01-31"}}})

	got := WithCoverage(r, pf)
	s, c := got.RA.RA, got.RA.Coverage
	if c.Uncovered != 1 || c.Days != 4 {
		t.Errorf("coverage = %+v", c)
	}
	if s.Confidence != "medium" || s.AwardKind != "" || day(s.Started) != "2022-05-05" || day(s.Finished) != "2022-05-05" {
		t.Errorf("narrowed = %+v", s)
	}
	if r.RA.RA.Confidence != "high" {
		t.Error("input report was mutated")
	}
}

func TestWithCoverage_RAAwardOutsideLoggedRunsStaysHigh(t *testing.T) {
	r := SuggestionReport{RA: ProviderResult{RA: &retroachievements.RASuggestion{
		Started: date("2023-03-01"), Finished: date("2023-04-10"), Confidence: "high", AwardKind: "beaten-hardcore", OK: true,
		Unlocks: []time.Time{date("2019-02-02"), date("2023-03-01"), date("2023-04-09")},
	}}}
	pf := logged(model.PlaythroughEntry{Sessions: []model.SessionEntry{{Started: "2019-02-01", Finished: "2019-02-28"}}})

	s := WithCoverage(r, pf).RA.RA
	if s.Confidence != "high" || day(s.Started) != "2023-03-01" || day(s.Finished) != "2023-04-10" {
		t.Errorf("narrowed = %+v", s)
	}
}

func logged(entries ...model.PlaythroughEntry) *model.PlaythroughsFile {
	return &model.PlaythroughsFile{Playthroughs: entries}
}
