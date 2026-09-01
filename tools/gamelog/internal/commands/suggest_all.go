package commands

import (
	"fmt"
	"time"

	"go.dalton.dog/gamelog/internal/model"
)

// SuggestSweepVerdict classifies one game's SuggestionReport for a
// whole-library sweep, given what its playthroughs.yaml already records. It
// returns actionable=true when a provider handed back a usable date range
// that the logged playthroughs don't already cover: nothing is logged at
// all, the logged entries carry no dates, or the newest provider activity
// falls after the newest date anywhere in the file.
//
// Pure and network-free on purpose. The live RetroAchievements/Steam calls
// happen in the caller (server.runSuggestAllJob, which reuses FetchRAResult /
// FetchSteamResult — the same fetches the single-game /suggest/{slug} page
// makes); this is only the "is it worth my attention" decision, kept
// separate so it stays unit-testable without fixtures.
func SuggestSweepVerdict(r SuggestionReport, pf *model.PlaythroughsFile) (actionable bool, note string) {
	suggested := latestSuggestedDate(r)
	if suggested == "" {
		return false, "no achievement dates from either provider"
	}

	logged := latestLoggedDate(pf)
	switch {
	case pf == nil || len(pf.Playthroughs) == 0:
		return true, "no playthrough logged — provider activity through " + suggested
	case logged == "":
		return true, "logged playthrough has no dates — provider activity through " + suggested
	case suggested > logged:
		return true, fmt.Sprintf("provider activity through %s, newest logged date %s", suggested, logged)
	default:
		return false, fmt.Sprintf("covered — newest logged date %s already at/after provider activity %s", logged, suggested)
	}
}

// latestSuggestedDate is the most recent calendar date (site timezone) across
// both providers' suggested ranges, or "" if neither produced one.
func latestSuggestedDate(r SuggestionReport) string {
	best := ""
	consider := func(t time.Time) {
		if t.IsZero() {
			return
		}
		if d := day(t); d > best {
			best = d
		}
	}
	if s := r.RA.RA; s != nil {
		consider(s.Started)
		consider(s.Finished)
	}
	if s := r.Steam.Steam; s != nil {
		consider(s.Started)
		consider(s.Finished)
	}
	return best
}

// latestLoggedDate is the lexicographically-latest YYYY-MM-DD found anywhere
// in the file — playthrough and session, started and finished. Dates are
// string-typed on purpose (see KNOWN-ISSUES.md); ISO order is date order, so
// a string compare is the right one here.
func latestLoggedDate(pf *model.PlaythroughsFile) string {
	if pf == nil {
		return ""
	}
	best := ""
	bump := func(s string) {
		if len(s) >= 7 && s > best {
			best = s
		}
	}
	for _, p := range pf.Playthroughs {
		bump(p.Started)
		bump(p.Finished)
		for _, sess := range p.Sessions {
			bump(sess.Started)
			bump(sess.Finished)
		}
	}
	return best
}
