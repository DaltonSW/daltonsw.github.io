package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// defaultStaleDays is how long since a Steam game's last recorded session
// before it's worth asking about. A month is long enough that "still
// mid-playthrough" is unlikely, short enough that games aren't left hanging
// indefinitely.
const defaultStaleDays = 30

// finishedCompletionThreshold is the achievement completion above which a
// stale game is guessed "finished" rather than "dropped". Set high since
// most players don't 100% a game even when they've finished it — this is
// meant to catch clear cases, not draw a precise line. 100% itself is
// guessed "mastered" rather than "finished" — see findStaleCandidates.
const finishedCompletionThreshold = 0.9

// StaleCandidate is one Steam-linked game that's gone quiet, with the
// suggestion computed from whatever's archived for it.
type StaleCandidate struct {
	Game            GameSummary
	LastPlayed      string
	DaysSince       int
	Unlocked, Total int
	PlaytimeMins    int
	SuggestedStatus string // "mastered" | "finished" | "dropped"
	Confidence      string // "medium" | "low"
}

// runStale walks through every currently-"playing" Steam game that hasn't
// been touched in a while — draft or published — offering a computed
// finished/mastered/dropped guess to accept, adjust, or skip — mirroring
// gamelog suggest's own rule that a guess is reported, never applied, until
// a human says so.
func runStale(args []string) error {
	fs := flag.NewFlagSet("stale", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	days := fs.Int("days", defaultStaleDays, "days since last played before a game is worth asking about")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: gamelog stale [flags]\n\nWalks through Steam games marked \"playing\" that have gone quiet, suggesting\nfinished/dropped for you to accept, adjust, or skip.\n\nFlags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	gamesDir, err := findGamesDir()
	if err != nil {
		return err
	}
	archiveDir := findArchiveDir(gamesDir)

	games, err := ListGames(gamesDir)
	if err != nil {
		return err
	}
	candidates, err := findStaleCandidates(archiveDir, games, *days)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		fmt.Printf("No games have gone quiet for %d+ days.\n", *days)
		return nil
	}

	skipped := map[string]bool{}
	acted := 0
	for {
		next := nextStale(candidates, skipped)
		if next == nil {
			fmt.Printf("\nDone — %d updated, %d left as-is.\n", acted, len(skipped))
			return nil
		}
		fmt.Printf("\n--- %d of %d ---\n", acted+len(skipped)+1, len(candidates))
		cont, did, err := reviewStale(*next)
		if err != nil {
			return err
		}
		if did {
			acted++
		} else {
			skipped[next.Game.Slug] = true
		}
		if !cont {
			fmt.Printf("Stopping — %d updated this run.\n", acted)
			return nil
		}
	}
}

// findStaleCandidates considers any currently-"playing" Steam-linked game,
// draft or not — pre-classifying drafts before a `gamelog review` pass is
// exactly what this is for. The one-shot statuses (ongoing,
// multiplayer, software) are never "playing" by design, so they're naturally
// excluded — a finished/dropped guess is meaningless for all three.
func findStaleCandidates(archiveDir string, games []GameSummary, thresholdDays int) ([]StaleCandidate, error) {
	now := time.Now().In(siteLocation)
	var out []StaleCandidate
	for _, g := range games {
		if g.SteamAppID == "" || g.Status != "playing" {
			continue
		}
		rec, err := LoadRecord(archiveDir, providerSteam, g.SteamAppID)
		if err != nil {
			return nil, err
		}
		if rec == nil || rec.LastPlayed == "" {
			continue
		}
		lastPlayed, err := time.ParseInLocation("2006-01-02", rec.LastPlayed, siteLocation)
		if err != nil {
			continue
		}
		daysSince := int(now.Sub(lastPlayed).Hours() / 24)
		if daysSince < thresholdDays {
			continue
		}

		c := StaleCandidate{
			Game: g, LastPlayed: rec.LastPlayed, DaysSince: daysSince,
			Unlocked: rec.Unlocked, Total: rec.Total, PlaytimeMins: rec.PlaytimeMins,
		}
		if rec.Total > 0 {
			c.Confidence = "medium"
			switch {
			case rec.Unlocked == rec.Total:
				c.SuggestedStatus = "mastered"
			case float64(rec.Unlocked)/float64(rec.Total) >= finishedCompletionThreshold:
				c.SuggestedStatus = "finished"
			default:
				c.SuggestedStatus = "dropped"
			}
		} else {
			// No achievement signal at all — playtime alone doesn't prove
			// completion, so "dropped" is the safer default guess; low
			// confidence says as much.
			c.Confidence = "low"
			c.SuggestedStatus = "dropped"
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DaysSince > out[j].DaysSince })
	return out, nil
}

func nextStale(candidates []StaleCandidate, skipped map[string]bool) *StaleCandidate {
	for i := range candidates {
		if !skipped[candidates[i].Game.Slug] {
			return &candidates[i]
		}
	}
	return nil
}

// reviewStale handles one candidate: shows its card, asks what to do, and
// applies it. cont reports whether the loop should continue; did reports
// whether this game was resolved (vs. skipped, which just leaves it
// "playing" — it'll show up again next run if it's still stale then).
func reviewStale(c StaleCandidate) (cont, did bool, err error) {
	doc, err := LoadDoc(c.Game.Path)
	if err != nil {
		return false, false, err
	}
	pf, err := LoadPlaythroughs(filepath.Dir(c.Game.Path))
	if err != nil {
		return false, false, err
	}

	fmt.Print(formatStaleCard(c))
	action, err := SelectStaleAction(c.SuggestedStatus)
	if err != nil {
		return false, false, err
	}

	switch {
	case action == staleAccept, strings.HasPrefix(action, staleMarkPrefix):
		applyStaleAction(doc, c, action)
		return true, true, confirmAndWriteFrontMatter(doc, pf)
	case action == staleEdit:
		doc.FM.Status = c.SuggestedStatus
		doc.FM.Finished = c.LastPlayed
		updated, err := EditGameForm(doc.FM)
		if err != nil {
			return false, false, err
		}
		doc.FM = updated
		return true, true, confirmAndWriteFrontMatter(doc, pf)
	case action == staleSkip:
		fmt.Println("Left as-is — will show again next run if still quiet.")
		return true, false, nil
	case action == staleQuit:
		return false, false, nil
	}
	return true, false, nil
}

// applyStaleAction decides what a stale-action decision means for doc's
// front matter — accepting the suggestion, or correcting it to a different
// terminal status — and sets it. It does not write anything: the TUI still
// wants its own confirm-and-write pass (confirmAndWriteFrontMatter, prompt
// included) right after calling this, while the web UI's already-submitted
// form is its confirmation, so it goes straight to the no-prompt
// writeFrontMatter instead. "Edit before applying" isn't handled here: it
// needs a whole extra form's worth of input first (see reviewStale and, for
// the web UI, the regular game-info edit page instead of a stale-specific
// one), and "skip"/"quit" change nothing.
func applyStaleAction(doc *Doc, c StaleCandidate, action string) {
	status := c.SuggestedStatus
	if s, ok := strings.CutPrefix(action, staleMarkPrefix); ok {
		status = s
	}
	doc.FM.Status = status
	// "paused" isn't a finish — the game's still meant to be played, just
	// not right now, so it gets no finished date.
	if status != "paused" {
		doc.FM.Finished = c.LastPlayed
	}
}

func formatStaleCard(c StaleCandidate) string {
	draftTag := ""
	if c.Game.Draft {
		draftTag = "  [draft]"
	}
	s := fmt.Sprintf("%s  [Steam %s]%s\n", c.Game.Title, c.Game.SteamAppID, draftTag)
	s += fmt.Sprintf("  last played %s (%d days ago)\n", c.LastPlayed, c.DaysSince)
	if c.Total > 0 {
		pct := float64(c.Unlocked) / float64(c.Total) * 100
		s += fmt.Sprintf("  achievements: %d/%d (%.0f%%)\n", c.Unlocked, c.Total, pct)
	} else {
		s += "  achievements: none tracked\n"
	}
	if c.PlaytimeMins > 0 {
		s += fmt.Sprintf("  playtime: %s\n", formatHours(c.PlaytimeMins))
	}
	s += fmt.Sprintf("  -> suggested: %s (%s confidence)\n", c.SuggestedStatus, c.Confidence)
	return s
}
