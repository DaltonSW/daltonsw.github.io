package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"go.dalton.dog/gamelog/internal/forms"
	"go.dalton.dog/gamelog/internal/model"
	"go.dalton.dog/gamelog/internal/mutate"
)

// runReview walks through every draft game one at a time, offering to
// publish, edit, mark dropped, skip, or delete the stub. Re-filters to
// Draft==true fresh each run, so skip/quit just leaves the rest for next time.
func RunReview(args []string) error {
	gamesDir, err := model.FindGamesDir()
	if err != nil {
		return err
	}

	games, err := model.ListGames(gamesDir)
	if err != nil {
		return err
	}
	total := countDrafts(games)
	if total == 0 {
		fmt.Println("No draft games to review.")
		return nil
	}

	archiveDir := model.FindArchiveDir(gamesDir)
	skipped := map[string]bool{}
	done := 0

	for {
		games, err := model.ListGames(gamesDir)
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

func countDrafts(games []model.GameSummary) int {
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
func nextDraft(games []model.GameSummary, skipped map[string]bool) *model.GameSummary {
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
func reviewOne(gamesDir, archiveDir string, g model.GameSummary) (cont, acted bool, err error) {
	doc, err := model.LoadDoc(g.Path)
	if err != nil {
		return false, false, err
	}
	pf, err := model.LoadPlaythroughs(filepath.Dir(g.Path))
	if err != nil {
		return false, false, err
	}

	// A game with captured playthroughs or archive history isn't a
	// plausible false-positive scan match — deleting it would risk real
	// data, so the option isn't even offered.
	canDelete := g.NumPlaythroughs == 0 && !HasArchiveRecord(archiveDir, g.ProviderLinks())

	fmt.Print(FormatReviewCard(g, doc.FM, pf))
	action, err := forms.SelectReviewAction(canDelete)
	if err != nil {
		return false, false, err
	}

	switch action {
	case forms.ReviewPublish, forms.ReviewDrop:
		ApplyReviewAction(doc, action)
		return true, true, mutate.ConfirmAndWriteFrontMatter(doc, pf)

	case forms.ReviewEditPublish:
		updated, err := forms.EditGameForm(doc.FM)
		if err != nil {
			return false, false, err
		}
		updated.Draft = false
		doc.FM = updated
		return true, true, mutate.ConfirmAndWriteFrontMatter(doc, pf)

	case forms.ReviewEditOnly:
		updated, err := forms.EditGameForm(doc.FM)
		if err != nil {
			return false, false, err
		}
		doc.FM = updated
		return true, true, mutate.ConfirmAndWriteFrontMatter(doc, pf)

	case forms.ReviewSkip:
		fmt.Println("Skipped — will show again on the next `gamelog review` run.")
		return true, false, nil

	case forms.ReviewDelete:
		ok, err := forms.ConfirmDelete(g.Path)
		if err != nil {
			return false, false, err
		}
		if !ok {
			fmt.Println("Not deleted.")
			return true, false, nil
		}
		if err := DeleteGameStub(gamesDir, g, canDelete); err != nil {
			return false, false, err
		}
		fmt.Printf("Deleted %s\n", filepath.Dir(g.Path))
		return true, true, nil

	case forms.ReviewQuit:
		return false, false, nil
	}
	return true, false, nil
}

// ApplyReviewAction decides what a review decision means for doc's front
// matter — for the two actions that need no further input beyond the action
// itself, publishing as-is or marking dropped-and-publishing — and sets it.
// It does not write anything, the same split as applyStaleAction: the TUI
// still runs confirmAndWriteFrontMatter (prompt included) right after
// calling this, while a web handler goes straight to the no-prompt
// writeFrontMatter since the submitted form is already its confirmation.
// "Edit, then publish"/"Edit without publishing" aren't handled here — they
// need a whole extra form's worth of input first (see reviewOne and, for the
// web UI, the regular game-info edit page); "skip"/"delete"/"quit" aren't
// front-matter edits at all.
func ApplyReviewAction(doc *model.Doc, action string) {
	switch action {
	case forms.ReviewPublish:
		doc.FM.Draft = false
	case forms.ReviewDrop:
		doc.FM.Status = "dropped"
		doc.FM.Draft = false
	}
}

// DeleteGameStub removes a game's whole content directory. Re-checks
// canDelete independently as defense in depth, and only ever targets a slug
// that came from ListGames, never typed input.
func DeleteGameStub(gamesDir string, g model.GameSummary, canDelete bool) error {
	if !canDelete {
		return fmt.Errorf("refusing to delete %s: it has captured data (playthroughs=%d)", g.Slug, g.NumPlaythroughs)
	}
	dir := filepath.Join(gamesDir, g.Slug)
	if filepath.Dir(dir) != gamesDir {
		return fmt.Errorf("refusing to delete outside %s", gamesDir)
	}
	return os.RemoveAll(dir)
}

func HasArchiveRecord(archiveDir string, links []model.ProviderLink) bool {
	for _, l := range links {
		if l.ID == "" {
			continue
		}
		if rec, _ := model.LoadRecord(archiveDir, l.Provider, l.ID); rec != nil {
			return true
		}
	}
	return false
}

// FormatReviewCard summarizes one draft game before asking what to do with
// it, mirroring formatScanReport's per-candidate block in scan.go.
func FormatReviewCard(g model.GameSummary, fm model.FrontMatter, pf *model.PlaythroughsFile) string {
	var links []string
	if g.RAGameID != "" {
		links = append(links, "RA "+g.RAGameID)
	}
	if g.SteamAppID != "" {
		links = append(links, "Steam "+g.SteamAppID)
	}
	if g.PSNID != "" {
		links = append(links, "PSN "+g.PSNID)
	}
	linkStr := "not linked"
	if len(links) > 0 {
		linkStr = links[0]
		for _, l := range links[1:] {
			linkStr += ", " + l
		}
	}

	s := fmt.Sprintf("%s  [%s]\n", g.Title, linkStr)
	s += fmt.Sprintf("  platform: %s\n", forms.OrNone(fm.Platform))
	s += fmt.Sprintf("  status: %s · started: %s · finished: %s\n",
		forms.OrNone(fm.Status), forms.OrNone(fm.Started), forms.OrDash(fm.Finished))

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
				p.Index+1, p.Status, len(p.Sessions), first.Started, forms.OrDash(last.Finished))
		} else {
			s += fmt.Sprintf("    #%d %s — %s → %s\n", p.Index+1, p.Status, p.Started, forms.OrDash(p.Finished))
		}
	}
	return s
}
