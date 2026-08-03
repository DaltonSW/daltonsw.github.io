package commands

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/charmbracelet/huh"

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

// runClose walks every playthrough left open past the quiet threshold and
// offers to cap it at the last day it was actually played.
//
// This is deliberately not part of `gamelog stale`, which asks a different
// question: stale looks at games still marked "playing" and guesses a terminal
// status (finished/dropped/mastered) from achievement completion. The
// open-ended entries this handles are mostly ongoing/multiplayer/software
// games whose status is already correct and must not change — the only thing
// wrong with them is a trailing `finished: ""` that renders as "ongoing"
// forever. Closing a date and picking a status are separate decisions, so they
// get separate commands.
//
// The whole list is presented as one pre-selected multi-select rather than a
// game-at-a-time loop: "cap it at the last day I played" is right for most of
// them, so the cheap interaction should be accepting them in bulk and
// deselecting the exceptions, not confirming each one.
func RunClose(args []string) error {
	fs := flag.NewFlagSet("close", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	days := fs.Int("days", DefaultStaleDays, "days since last played before an open entry is worth asking about")
	all := fs.Bool("all", false, "include entries still marked \"playing\" at the game level")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: gamelog close [flags]\n\nFinds playthroughs left without a closing date and offers to cap each one at\nthe last day it was actually played. Statuses are never changed.\n\nFlags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	gamesDir, err := model.FindGamesDir()
	if err != nil {
		return err
	}
	archiveDir := model.FindArchiveDir(gamesDir)

	games, err := model.ListGames(gamesDir)
	if err != nil {
		return err
	}
	open, err := FindOpenEntries(archiveDir, gamesDir, games, *days, *all)
	if err != nil {
		return err
	}
	if len(open) == 0 {
		fmt.Printf("No playthroughs have been left open for %d+ days.\n", *days)
		return nil
	}

	chosen, err := SelectEntriesToClose(open)
	if err != nil {
		return err
	}
	if len(chosen) == 0 {
		fmt.Println("Nothing selected — no changes made.")
		return nil
	}

	closed := 0
	for _, i := range chosen {
		e := open[i]
		did, err := CloseEntry(e)
		if err != nil {
			return fmt.Errorf("%s: %w", e.Game.Slug, err)
		}
		if did {
			fmt.Printf("  %s → %s\n", e.Game.Title, e.CloseOn)
			closed++
		}
	}
	fmt.Printf("\nClosed %d of %d.\n", closed, len(open))
	return nil
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

// SelectEntriesToClose shows every open entry pre-selected, since capping at
// the last day played is the expected answer; deselect the ones that are
// genuinely still in progress.
func SelectEntriesToClose(open []OpenEntry) ([]int, error) {
	options := make([]huh.Option[int], len(open))
	for i, e := range open {
		label := fmt.Sprintf("%-42s %-14s close %s  (%s, quiet %s)",
			truncate(e.Game.Title, 42), e.Game.Status, e.CloseOn, e.Source, quietFor(e.DaysSince))
		options[i] = huh.NewOption(label, i).Selected(true)
	}
	chosen := make([]int, 0, len(open))
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[int]().
				Title(fmt.Sprintf("Close %d open playthrough(s)?", len(open))).
				Description("all pre-selected — space to deselect anything still in progress, enter to confirm.\nstatuses are not changed; only the trailing date is filled in.").
				Options(options...).
				Filterable(true).
				Height(20).
				Value(&chosen),
		),
	).Run()
	return chosen, err
}

func quietFor(days int) string {
	if days >= 365 {
		return fmt.Sprintf("%.1fy", float64(days)/365)
	}
	return fmt.Sprintf("%dd", days)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
