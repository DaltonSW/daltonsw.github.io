package commands

import (
	"sort"
	"time"

	"go.dalton.dog/gamelog/internal/forms"
	"go.dalton.dog/gamelog/internal/model"
)

// Backlog population, in two halves that answer two different questions:
//
//   - what's owned but never played — ScanSteam in ScanModeBacklog, the half
//     of the Steam library the ordinary scan throws away, turned into
//     `status: backlog` drafts;
//   - what's played but unfinished — FindUnfinished, computed entirely from
//     the archive already on disk.
//
// The second half deliberately makes no API calls and writes nothing: it's a
// reading list over data that's already captured, so it stays useful with no
// credentials configured at all.

// UnfinishedCandidate is one game/provider pair with achievements left to
// earn. One row per provider rather than per game: a game linked to both
// Steam and RetroAchievements has two unrelated denominators, and merging
// them would invent a number that isn't true of either service. A
// RetroAchievements subset is a third such denominator on the same game, so
// it gets its own row too — see ProviderLabel.
type UnfinishedCandidate struct {
	Game     model.GameSummary
	Provider string
	// Subset names the RetroAchievements subset this row is about, blank for
	// a game's own set. Two rows reading "retroachievements" with different
	// numbers would otherwise look like a bug.
	Subset          string
	Unlocked, Total int
	Remaining       int
	Pct             float64 // 0-100
	LastPlayed      string
	DaysSince       int // 0 when LastPlayed is empty or unparseable
	PlaytimeMins    int
}

// ProviderLabel names the achievement set this row is about, which is the
// provider alone until a game has subsets and two rows would otherwise carry
// the same label with different numbers.
func (c UnfinishedCandidate) ProviderLabel() string {
	if c.Subset == "" {
		return c.Provider
	}
	return c.Provider + " · " + c.Subset
}

// DefaultUnfinishedPct hides games barely started. Something at 4% isn't a
// game with a few achievements left, it's a game that was opened once — the
// backlog scan above is the honest place for those.
const DefaultUnfinishedPct = 50

type UnfinishedOptions struct {
	MinPct float64
	// IncludeDone keeps games whose status already says they're over
	// (finished/dropped/mastered). Off by default: a finished game with
	// achievements left is a deliberate decision, not an oversight.
	IncludeDone bool
}

// doneStatuses are the game-level statuses that assert the game is behind
// you. `paused` and `backlog` are excluded from this set on purpose — both
// mean "not now", not "not ever", so they belong in the list.
var doneStatuses = map[string]bool{"finished": true, "dropped": true, "mastered": true}

// FindUnfinished ranks logged games by how close they are to having every
// achievement. It reads only the archive on disk — no provider calls — so it
// costs nothing to render and works with no credentials configured.
//
// It walks games rather than globbing archive/*/*.json because the archive is
// keyed by provider ID and carries no slug: going game-first yields the title
// and link for free, and naturally skips records whose content entry is gone
// (which is exactly the case the archive living outside content/ exists to
// survive — those records are kept, not surfaced).
func FindUnfinished(archiveDir string, games []model.GameSummary, opts UnfinishedOptions) ([]UnfinishedCandidate, error) {
	now := time.Now().In(model.SiteLocation)
	var out []UnfinishedCandidate
	for _, g := range games {
		// ongoing/multiplayer/software have no completion to be short of.
		if forms.IsOneShot(g.Status) {
			continue
		}
		if !opts.IncludeDone && doneStatuses[g.Status] {
			continue
		}
		for _, link := range g.ProviderLinks() {
			rec, err := model.LoadRecord(archiveDir, link.Provider, link.ID)
			if err != nil {
				return nil, err
			}
			if rec == nil || rec.Total <= 0 || rec.Unlocked >= rec.Total {
				continue
			}
			pct := float64(rec.Unlocked) / float64(rec.Total) * 100
			if pct < opts.MinPct {
				continue
			}
			subset := ""
			if link.Subset {
				if _, name, ok := model.SplitRASubsetTitle(rec.Title); ok {
					subset = name
				} else {
					subset = link.ID
				}
			}
			c := UnfinishedCandidate{
				Game:         g,
				Provider:     link.Provider,
				Subset:       subset,
				Unlocked:     rec.Unlocked,
				Total:        rec.Total,
				Remaining:    rec.Total - rec.Unlocked,
				Pct:          pct,
				LastPlayed:   rec.LastPlayed,
				PlaytimeMins: rec.PlaytimeMins,
			}
			if t, err := time.ParseInLocation("2006-01-02", rec.LastPlayed, model.SiteLocation); err == nil {
				c.DaysSince = int(now.Sub(t).Hours() / 24)
			}
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Pct != b.Pct {
			return a.Pct > b.Pct
		}
		if a.Remaining != b.Remaining {
			return a.Remaining < b.Remaining
		}
		return a.Game.Title < b.Game.Title
	})
	return out, nil
}
