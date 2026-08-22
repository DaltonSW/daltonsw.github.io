package commands

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"go.dalton.dog/gamelog/internal/forms"
	"go.dalton.dog/gamelog/internal/model"
)

// DefaultStaleDays is how long since a Steam game's last recorded session
// before it's worth asking about. A month is long enough that "still
// mid-playthrough" is unlikely, short enough that games aren't left hanging
// indefinitely.
const DefaultStaleDays = 30

// finishedCompletionThreshold is the achievement completion above which a
// stale game is guessed "finished" rather than "dropped". Set high since
// most players don't 100% a game even when they've finished it — this is
// meant to catch clear cases, not draw a precise line. 100% itself is
// guessed "mastered" rather than "finished" — see FindStaleCandidates.
const finishedCompletionThreshold = 0.9

// StaleCandidate is one Steam-linked game that's gone quiet, with the
// suggestion computed from whatever's archived for it.
type StaleCandidate struct {
	Game            model.GameSummary
	LastPlayed      string
	DaysSince       int
	Unlocked, Total int
	PlaytimeMins    int
	SuggestedStatus string // "mastered" | "finished" | "dropped" | "unfinished"
	Confidence      string // "medium" | "low"
}

// FindStaleCandidates considers any currently-"playing" Steam-linked game,
// draft or not — pre-classifying drafts before a `gamelog review` pass is
// exactly what this is for. The one-shot statuses (endless,
// multiplayer, software) are never "playing" by design, so they're naturally
// excluded — a finished/dropped guess is meaningless for all three.
func FindStaleCandidates(archiveDir string, games []model.GameSummary, thresholdDays int) ([]StaleCandidate, error) {
	now := time.Now().In(model.SiteLocation)
	var out []StaleCandidate
	for _, g := range games {
		if g.SteamAppID == "" || g.Status != "playing" {
			continue
		}
		rec, err := model.LoadRecord(archiveDir, model.ProviderSteam, g.SteamAppID)
		if err != nil {
			return nil, err
		}
		if rec == nil || rec.LastPlayed == "" {
			continue
		}
		lastPlayed, err := time.ParseInLocation("2006-01-02", rec.LastPlayed, model.SiteLocation)
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
			// completion, and with nothing to go on there's no basis to guess
			// "dropped" over "paused" either, so "unfinished" is the honest
			// default guess; low confidence says as much.
			c.Confidence = "low"
			c.SuggestedStatus = "unfinished"
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DaysSince > out[j].DaysSince })
	return out, nil
}

// ApplyStaleAction decides what a stale-action decision means for doc's
// front matter — accepting the suggestion, or correcting it to a different
// terminal status — and sets it. It does not write anything: the TUI still
// wants its own confirm-and-write pass (confirmAndWriteFrontMatter, prompt
// included) right after calling this, while the web UI's already-submitted
// form is its confirmation, so it goes straight to the no-prompt
// writeFrontMatter instead. "Edit before applying" isn't handled here: it
// needs a whole extra form's worth of input first (see reviewStale and, for
// the web UI, the regular game-info edit page instead of a stale-specific
// one), and "skip"/"quit" change nothing.
func ApplyStaleAction(doc *model.Doc, c StaleCandidate, action string) {
	status := c.SuggestedStatus
	if s, ok := strings.CutPrefix(action, forms.StaleMarkPrefix); ok {
		status = s
	}
	doc.FM.Status = status
	// "paused" isn't a finish — the game's still meant to be played, just
	// not right now, so it gets no finished date.
	if status != "paused" {
		doc.FM.Finished = c.LastPlayed
	}
}

func FormatStaleCard(c StaleCandidate) string {
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
		s += fmt.Sprintf("  playtime: %s\n", FormatHours(c.PlaytimeMins))
	}
	s += fmt.Sprintf("  -> suggested: %s (%s confidence)\n", c.SuggestedStatus, c.Confidence)
	return s
}
