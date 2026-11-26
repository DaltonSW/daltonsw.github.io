package commands

import (
	"sort"
	"strings"
	"time"

	"go.dalton.dog/gamelog/internal/model"
)

// Coverage records how much of one provider's activity playthroughs.yaml
// already accounts for. Set by WithCoverage; nil means it wasn't checked.
type Coverage struct {
	Days      int    // distinct activity days the provider reported
	Uncovered int    // of those, the days no logged playthrough or session contains
	Latest    string // newest activity day, covered or not

	// LatestOpenIn is the playthrough index whose open-ended span (no finish
	// date) contains Latest, or -1. That activity is a finish-date candidate
	// for an existing run, not a new playthrough.
	LatestOpenIn int

	// FinishCovered means the provider's finish signal (an RA award date, or
	// the latest unlock) falls inside a logged span, so it must not be
	// offered as the finish of a new playthrough.
	FinishCovered bool
}

// WithCoverage narrows each provider's suggestion to the activity
// playthroughs.yaml doesn't already account for. A provider only exposes one
// history per game, so its raw earliest→latest range spans every run ever
// played; offered as-is it suggests a "new" playthrough overlapping all the
// logged ones.
//
// Started/Finished are rewritten to the earliest/latest uncovered instant.
// When every day is covered they're left alone and Coverage.Uncovered is 0 —
// callers show "nothing to log" rather than a form. The input is not mutated.
func WithCoverage(r SuggestionReport, pf *model.PlaythroughsFile) SuggestionReport {
	spans := loggedSpans(pf)
	if s := r.RA.RA; s != nil {
		c := *s
		dates := activityDates(c.Unlocks, c.Started, c.Finished)
		award := c.Confidence == "high"
		if award {
			dates = append(dates, c.Finished)
		}
		cov, first, last := coverageOf(dates, spans)
		if award {
			cov.FinishCovered = spans.covers(day(c.Finished)) >= 0
		}
		if cov.Uncovered > 0 {
			c.Started = first
			if award && cov.FinishCovered {
				// The award belongs to a logged run; whatever is left is
				// later or earlier activity with no finish signal of its own.
				c.Confidence, c.AwardKind = "medium", ""
			}
			if !award || cov.FinishCovered {
				c.Finished = last
			}
		}
		r.RA.RA, r.RA.Coverage = &c, cov
	}
	if s := r.Steam.Steam; s != nil {
		c := *s
		cov, first, last := coverageOf(activityDates(c.Unlocks, c.Started, c.Finished), spans)
		if cov.Uncovered > 0 {
			c.Started, c.Finished = first, last
		}
		r.Steam.Steam, r.Steam.Coverage = &c, cov
	}
	return r
}

// activityDates falls back to the range endpoints for a suggestion built
// without per-unlock instants.
func activityDates(unlocks []time.Time, started, finished time.Time) []time.Time {
	if len(unlocks) > 0 {
		return append([]time.Time(nil), unlocks...)
	}
	var out []time.Time
	for _, t := range []time.Time{started, finished} {
		if !t.IsZero() {
			out = append(out, t)
		}
	}
	return out
}

// coverageOf checks each activity day against the logged spans, returning the
// earliest and latest uncovered instants alongside the tally.
func coverageOf(dates []time.Time, spans spanList) (cov *Coverage, first, last time.Time) {
	cov = &Coverage{LatestOpenIn: -1}
	seen := map[string]bool{}
	for _, t := range dates {
		d := day(t)
		if d > cov.Latest {
			cov.Latest = d
		}
		if spans.covers(d) < 0 {
			if first.IsZero() || t.Before(first) {
				first = t
			}
			if last.IsZero() || t.After(last) {
				last = t
			}
		}
		if seen[d] {
			continue
		}
		seen[d] = true
		cov.Days++
		if spans.covers(d) < 0 {
			cov.Uncovered++
		}
	}
	if cov.Latest != "" {
		if i := spans.covers(cov.Latest); i >= 0 && spans[i].open {
			cov.LatestOpenIn = spans[i].pth
		}
		cov.FinishCovered = spans.covers(cov.Latest) >= 0
	}
	return cov, first, last
}

// loggedSpan is one stretch of logged play. Dates stay strings (see
// INVARIANTS.md) and may be partial ("2018-09"); comparisons are by prefix.
type loggedSpan struct {
	start, end string
	// open: no finish date. The span then runs until the next logged span
	// starts (bound), or indefinitely if none does — a run with no end
	// recorded can't be said to have stopped before any given activity.
	open  bool
	bound string
	pth   int
}

type spanList []loggedSpan

// loggedSpans flattens a playthroughs file into spans: each session, or the
// playthrough's own started/finished when it has no sessions. Planned entries
// haven't happened and cover nothing.
func loggedSpans(pf *model.PlaythroughsFile) spanList {
	if pf == nil {
		return nil
	}
	var out spanList
	add := func(pth int, started, finished string) {
		if started == "" {
			started = finished
		}
		if started == "" {
			return
		}
		out = append(out, loggedSpan{start: started, end: finished, open: finished == "", pth: pth})
	}
	for _, p := range pf.Views() {
		if p.Status == "planned" {
			continue
		}
		for _, s := range p.Sessions {
			add(p.Index, s.Started, s.Finished)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].start < out[j].start })
	for i := range out {
		if !out[i].open {
			continue
		}
		for _, next := range out[i+1:] {
			if next.start > out[i].start {
				out[i].bound = next.start
				break
			}
		}
	}
	return out
}

// covers returns the index of the first span containing day (YYYY-MM-DD), or
// -1. A closed span that contains it wins over an open one, since its
// playthrough is the more specific match.
func (l spanList) covers(d string) int {
	open := -1
	for i, s := range l {
		if prefixCmp(d, s.start) < 0 {
			continue
		}
		if !s.open {
			if prefixCmp(d, s.end) <= 0 {
				return i
			}
			continue
		}
		if open < 0 && (s.bound == "" || prefixCmp(d, s.bound) < 0) {
			open = i
		}
	}
	return open
}

// prefixCmp compares a full day against a possibly-partial logged date at the
// logged date's precision, so "2018-09-14" is neither before nor after
// "2018-09".
func prefixCmp(d, logged string) int {
	n := min(len(d), len(logged))
	return strings.Compare(d[:n], logged[:n])
}
