package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// runReview walks through every draft game one at a time, offering to
// publish, edit, mark dropped, skip, or delete the stub. Re-filters to
// Draft==true fresh each run, so skip/quit just leaves the rest for next time.
func runReview(args []string) error {
	gamesDir, err := findGamesDir()
	if err != nil {
		return err
	}

	games, err := ListGames(gamesDir)
	if err != nil {
		return err
	}
	total := countDrafts(games)
	if total == 0 {
		fmt.Println("No draft games to review.")
		return nil
	}

	archiveDir := findArchiveDir(gamesDir)
	skipped := map[string]bool{}
	done := 0

	for {
		games, err := ListGames(gamesDir)
		if err != nil {
			return err
		}
		next := nextDraft(games, skipped)
		if next == nil {
			fmt.Printf("\nDone — %d reviewed, %d skipped for later.\n", done, len(skipped))
			return nil
		}

		fmt.Printf("\n--- game %d of %d ---\n", done+len(skipped)+1, total)
		cont, acted, err := reviewOne(gamesDir, archiveDir, *next)
		if err != nil {
			return err
		}
		if acted {
			done++
		} else {
			skipped[next.Slug] = true
		}
		if !cont {
			fmt.Printf("Stopping — %d reviewed this run.\n", done)
			return nil
		}
	}
}

func countDrafts(games []GameSummary) int {
	n := 0
	for _, g := range games {
		if g.Draft {
			n++
		}
	}
	return n
}

// nextDraft returns the first draft game not already set aside as skipped
// this run, or nil once none remain.
func nextDraft(games []GameSummary, skipped map[string]bool) *GameSummary {
	for i := range games {
		if games[i].Draft && !skipped[games[i].Slug] {
			return &games[i]
		}
	}
	return nil
}

// reviewOne handles one game: shows its summary card, asks what to do, and
// applies it. cont reports whether the review loop should continue; acted
// reports whether this game was resolved (vs. skipped, which leaves it
// draft for a future run).
func reviewOne(gamesDir, archiveDir string, g GameSummary) (cont, acted bool, err error) {
	doc, err := LoadDoc(g.Path)
	if err != nil {
		return false, false, err
	}
	pf, err := LoadPlaythroughs(filepath.Dir(g.Path))
	if err != nil {
		return false, false, err
	}

	// A game with captured playthroughs or archive history isn't a
	// plausible false-positive scan match — deleting it would risk real
	// data, so the option isn't even offered.
	canDelete := g.NumPlaythroughs == 0 && !hasArchiveRecord(archiveDir, g.RAGameID, g.SteamAppID)

	fmt.Print(formatReviewCard(g, doc.FM, pf))
	action, err := SelectReviewAction(canDelete)
	if err != nil {
		return false, false, err
	}

	switch action {
	case reviewPublish:
		doc.FM.Draft = false
		return true, true, confirmAndWriteFrontMatter(doc, pf)

	case reviewEditPublish:
		updated, err := EditGameForm(doc.FM)
		if err != nil {
			return false, false, err
		}
		updated.Draft = false
		doc.FM = updated
		return true, true, confirmAndWriteFrontMatter(doc, pf)

	case reviewEditOnly:
		updated, err := EditGameForm(doc.FM)
		if err != nil {
			return false, false, err
		}
		doc.FM = updated
		return true, true, confirmAndWriteFrontMatter(doc, pf)

	case reviewDrop:
		doc.FM.Status = "dropped"
		doc.FM.Draft = false
		return true, true, confirmAndWriteFrontMatter(doc, pf)

	case reviewSkip:
		fmt.Println("Skipped — will show again on the next `gamelog review` run.")
		return true, false, nil

	case reviewDelete:
		ok, err := ConfirmDelete(g.Path)
		if err != nil {
			return false, false, err
		}
		if !ok {
			fmt.Println("Not deleted.")
			return true, false, nil
		}
		if err := deleteGameStub(gamesDir, g, canDelete); err != nil {
			return false, false, err
		}
		fmt.Printf("Deleted %s\n", filepath.Dir(g.Path))
		return true, true, nil

	case reviewQuit:
		return false, false, nil
	}
	return true, false, nil
}

// deleteGameStub removes a game's whole content directory. Re-checks
// canDelete independently as defense in depth, and only ever targets a slug
// that came from ListGames, never typed input.
func deleteGameStub(gamesDir string, g GameSummary, canDelete bool) error {
	if !canDelete {
		return fmt.Errorf("refusing to delete %s: it has captured data (playthroughs=%d)", g.Slug, g.NumPlaythroughs)
	}
	dir := filepath.Join(gamesDir, g.Slug)
	if filepath.Dir(dir) != gamesDir {
		return fmt.Errorf("refusing to delete outside %s", gamesDir)
	}
	return os.RemoveAll(dir)
}

func hasArchiveRecord(archiveDir, raID, steamAppID string) bool {
	if raID != "" {
		if rec, _ := LoadRecord(archiveDir, providerRA, raID); rec != nil {
			return true
		}
	}
	if steamAppID != "" {
		if rec, _ := LoadRecord(archiveDir, providerSteam, steamAppID); rec != nil {
			return true
		}
	}
	return false
}

// formatReviewCard summarizes one draft game before asking what to do with
// it, mirroring formatScanReport's per-candidate block in scan.go.
func formatReviewCard(g GameSummary, fm FrontMatter, pf *PlaythroughsFile) string {
	var links []string
	if g.RAGameID != "" {
		links = append(links, "RA "+g.RAGameID)
	}
	if g.SteamAppID != "" {
		links = append(links, "Steam "+g.SteamAppID)
	}
	linkStr := "not linked"
	if len(links) > 0 {
		linkStr = links[0]
		for _, l := range links[1:] {
			linkStr += ", " + l
		}
	}

	s := fmt.Sprintf("%s  [%s]\n", g.Title, linkStr)
	s += fmt.Sprintf("  platform: %s\n", orDash(fm.Platform))
	s += fmt.Sprintf("  status: %s · started: %s · finished: %s\n",
		orDash(fm.Status), orDash(fm.Started), orDash(fm.Finished))

	views := pf.Views()
	if len(views) == 0 {
		s += "  no playthroughs logged\n"
		return s
	}
	s += fmt.Sprintf("  %d playthrough(s) logged:\n", len(views))
	for _, p := range views {
		if p.HasSessions() {
			first, last := p.Sessions[0], p.Sessions[len(p.Sessions)-1]
			s += fmt.Sprintf("    #%d %s — %d sessions, %s → %s\n",
				p.Index+1, p.Status, len(p.Sessions), first.Started, orDash(last.Finished))
		} else {
			s += fmt.Sprintf("    #%d %s — %s → %s\n", p.Index+1, p.Status, p.Started, orDash(p.Finished))
		}
	}
	return s
}
