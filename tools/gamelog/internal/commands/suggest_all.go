package commands

import (
	"fmt"
	"time"

	"go.dalton.dog/gamelog/internal/model"
)

// SuggestSweepVerdict classifies one game's SuggestionReport for a
// whole-library sweep, given what its playthroughs.yaml already records. It
// returns actionable=true when a provider reports activity on a day no
// logged playthrough or session contains (see WithCoverage) — including a
// gap between logged runs, not just activity after the newest one.
//
// Pure and network-free on purpose. The live RetroAchievements/Steam calls
// happen in the caller (server.runSuggestAllJob, which reuses FetchRAResult /
// FetchSteamResult — the same fetches the game page's suggest panel makes);
// this is only the "is it worth my attention" decision, kept separate so it
// stays unit-testable without fixtures.
func SuggestSweepVerdict(r SuggestionReport, pf *model.PlaythroughsFile) (actionable bool, note string) {
	r = WithCoverage(r, pf)

	var days, uncovered int
	var first, last time.Time
	tally := func(cov *Coverage, started, finished time.Time) {
		if cov == nil {
			return
		}
		days += cov.Days
		if cov.Uncovered == 0 {
			return
		}
		uncovered += cov.Uncovered
		if first.IsZero() || started.Before(first) {
			first = started
		}
		if finished.After(last) {
			last = finished
		}
	}
	if s := r.RA.RA; s != nil {
		tally(r.RA.Coverage, s.Started, s.Finished)
	}
	if s := r.Steam.Steam; s != nil {
		tally(r.Steam.Coverage, s.Started, s.Finished)
	}

	switch {
	case days == 0:
		return false, "no achievement dates from either provider"
	case uncovered == 0:
		return false, "covered — every provider activity day falls inside a logged playthrough"
	case pf == nil || len(pf.Playthroughs) == 0:
		return true, fmt.Sprintf("no playthrough logged — provider activity %s → %s", day(first), day(last))
	default:
		return true, fmt.Sprintf("provider activity on %d day(s) outside any logged playthrough, %s → %s",
			uncovered, day(first), day(last))
	}
}
