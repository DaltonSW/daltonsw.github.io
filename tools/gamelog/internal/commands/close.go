package commands

import (
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"go.dalton.dog/gamelog/internal/model"
	"go.dalton.dog/gamelog/internal/mutate"
)

// OpenEntry is one playthrough entry left without a closing date, paired with
// the date we'd close it on.
type OpenEntry struct {
	Game      model.GameSummary
	Index     int    // which playthrough entry within the file
	CloseOn   string // the date the trailing session/entry would get
	Source    string // where CloseOn came from, shown so a wrong date is obvious
	LastKnown string // last activity, for the quiet-time display
	DaysSince int
}

// FindOpenEntries collects every playthrough whose trailing date is blank and
// whose last known activity is at least thresholdDays old.
//
// Games still marked "playing" are skipped unless includePlaying: an open date
// on a game you're actually playing is correct, and `gamelog stale` is where
// "should this still be playing?" gets asked.
func FindOpenEntries(archiveDir, gamesDir string, games []model.GameSummary, thresholdDays int, includePlaying bool) ([]OpenEntry, error) {
	now := time.Now().In(model.SiteLocation)
	var out []OpenEntry
	for _, g := range games {
		if g.Status == "playing" && !includePlaying {
			continue
		}
		// "paused" means on hold, not abandoned — you mean to come back to it.
		// Setting a closing date on one is exactly what reviewStale refuses to
		// do, and an open-ended bar is the honest render for a game you intend
		// to resume. Skipped unconditionally; --all doesn't reach these.
		if g.Status == "paused" {
			continue
		}
		// "planned" means not started yet — skip these even if they have an
		// open entry (shouldn't happen, but be defensive).
		if g.Status == "planned" {
			continue
		}
		pf, err := model.LoadPlaythroughs(filepath.Dir(g.Path))
		if err != nil {
			return nil, err
		}
		for _, p := range pf.Views() {
			if !p.IsOpen() {
				continue
			}
			// The last day we can prove it was played. The archive is
			// preferred — a refreshed last_played is more recent than any
			// hand-logged session — but a game with no archived date still
			// has the day the open session started, which for a one-evening
			// party game is exactly the right answer.
			lastLogged := p.Started
			if p.HasSessions() {
				lastLogged = p.Sessions[len(p.Sessions)-1].Started
			}
			closeOn, source := lastLogged, "logged start"
			if d := archiveLastPlayed(archiveDir, g); d > closeOn {
				closeOn, source = d, "archive"
			}
			if closeOn == "" {
				// Nothing anywhere says when this was played; inventing a
				// date would be worse than leaving it open.
				continue
			}
			last, err := time.ParseInLocation("2006-01-02", closeOn, model.SiteLocation)
			if err != nil {
				continue
			}
			daysSince := int(now.Sub(last).Hours() / 24)
			if daysSince < thresholdDays {
				continue
			}
			out = append(out, OpenEntry{
				Game: g, Index: p.Index, CloseOn: closeOn, Source: source,
				LastKnown: closeOn, DaysSince: daysSince,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DaysSince > out[j].DaysSince })
	return out, nil
}

// archiveLastPlayed is the most recent last_played across every provider a
// game is linked to, or "" when nothing is archived for it.
func archiveLastPlayed(archiveDir string, g model.GameSummary) string {
	best := ""
	for _, link := range g.ProviderLinks() {
		if link.ID == "" {
			continue
		}
		rec, err := model.LoadRecord(archiveDir, link.Provider, link.ID)
		if err != nil || rec == nil {
			continue
		}
		if rec.LastPlayed > best {
			best = rec.LastPlayed
		}
	}
	return best
}

// CloseEntry writes the closing date onto one entry's trailing session, or
// onto the entry itself when it has no sessions. Status is left alone — an
// entry going from open to closed says when play stopped, not that the game
// was completed, and for the ongoing/multiplayer/software games this
// mostly targets there is no completion to claim.
func CloseEntry(e OpenEntry) (bool, error) {
	pf, err := model.LoadPlaythroughs(filepath.Dir(e.Game.Path))
	if err != nil {
		return false, err
	}
	if e.Index < 0 || e.Index >= len(pf.Playthroughs) {
		return false, fmt.Errorf("playthrough %d no longer exists", e.Index+1)
	}
	entry := &pf.Playthroughs[e.Index]

	if len(entry.Sessions) > 0 {
		last := &entry.Sessions[len(entry.Sessions)-1]
		if last.Finished != "" {
			return false, nil // already closed since the list was built
		}
		last.Finished = e.CloseOn
	} else {
		if entry.Finished != "" {
			return false, nil
		}
		entry.Finished = e.CloseOn
	}

	// Same pre-write proof every other playthrough write makes: nothing that
	// was on disk may vanish. Only a date is being added, so no removal is
	// ever legitimate here and none is declared.
	if err := mutate.VerifyNoLoss(pf, nil); err != nil {
		return false, err
	}
	return true, pf.Save()
}
