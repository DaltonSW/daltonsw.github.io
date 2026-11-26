package commands

import (
	"fmt"
	"path/filepath"

	"go.dalton.dog/gamelog/internal/model"
	"go.dalton.dog/gamelog/internal/mutate"
)

// ProjectResult counts what Project rebuilt. Errors names each game that
// failed and why; one game failing doesn't stop the rest.
type ProjectResult struct {
	Summaries int // achievement-summary.yaml files written
	Empty     int // linked games with nothing archived yet
	Campaigns int // campaigns.yaml files written
	Statuses  int // game statuses changed to match their runs
	Errors    []string
}

// Project regenerates every game's projections purely from what's already on
// disk: achievement-summary.yaml from the archive, campaigns.yaml from the
// Trackmania record, and the game's status from its runs. No API calls, so
// it's safe and fast to run any time a projection needs rebuilding (after a
// hand edit to the archive or playthroughs.yaml, or a change to what a
// summary contains) without re-fetching anything.
func Project(gamesDir string) (ProjectResult, error) {
	var res ProjectResult
	games, err := model.ListGames(gamesDir)
	if err != nil {
		return res, err
	}
	archiveDir := model.FindArchiveDir(gamesDir)
	fail := func(slug, what string, err error) {
		res.Errors = append(res.Errors, fmt.Sprintf("%s: %s: %v", slug, what, err))
	}

	for _, g := range games {
		gameDir := filepath.Dir(g.Path)

		// A game's status is a projection of its runs too (model.
		// DeriveGameStatus), so a hand edit to playthroughs.yaml shows up here.
		if pf, err := model.LoadPlaythroughs(gameDir); err != nil {
			fail(g.Slug, "status", err)
		} else if changed, err := mutate.SyncGameStatus(pf); err != nil {
			fail(g.Slug, "status", err)
		} else if changed {
			res.Statuses++
		}

		// Trackmania's campaign history is a separate projection with its own
		// source record, so it's rebuilt alongside rather than inside the
		// achievement summary — see model.WriteCampaignSummary.
		if g.NadeoAccountID != "" {
			if wrote, err := model.WriteCampaignSummary(archiveDir, gameDir, g.NadeoAccountID); err != nil {
				fail(g.Slug, "campaigns", err)
			} else if wrote {
				res.Campaigns++
			}
		}

		links := g.ProviderLinks()
		if len(links) == 0 {
			continue
		}
		switch wrote, err := model.WriteAchievementSummary(archiveDir, gameDir, links); {
		case err != nil:
			fail(g.Slug, "achievements", err)
		case wrote:
			res.Summaries++
		default:
			res.Empty++
		}
	}
	return res, nil
}
