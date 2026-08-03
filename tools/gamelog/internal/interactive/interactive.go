// Package interactive implements the terminal REPL: pick or create a game,
// then log a playthrough, log a session, or update one. This is the `gamelog`
// no-args flow — see cmd/gamelog for how it's wired up alongside the other
// subcommands, and internal/server for the web UI equivalent.
package interactive

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"

	"go.dalton.dog/gamelog/internal/commands"
	"go.dalton.dog/gamelog/internal/forms"
	"go.dalton.dog/gamelog/internal/model"
	"go.dalton.dog/gamelog/internal/mutate"
)

const (
	actionEditInfo       = "edit_info"
	actionNewPlaythrough = "new_playthrough"
	actionNewSession     = "new_session"
	actionUpdate         = "update"
	actionManageSessions = "manage_sessions"
	actionAddPlanned     = "add_planned"
	actionManagePlanned  = "manage_planned"
)

func Run() error {
	gamesDir, err := model.FindGamesDir()
	if err != nil {
		return err
	}

	for {
		games, err := model.ListGames(gamesDir)
		if err != nil {
			return err
		}

		slug, err := forms.SelectGame(games)
		if err != nil {
			return err
		}

		if slug == forms.NewGameSentinel {
			if err := createGame(gamesDir); err != nil {
				return err
			}
		} else {
			var summary model.GameSummary
			for _, g := range games {
				if g.Slug == slug {
					summary = g
					break
				}
			}
			if err := logForGame(summary); err != nil {
				return err
			}
		}

		var again bool
		if err := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().Title("Do another?").Value(&again),
			),
		).Run(); err != nil {
			return err
		}
		if !again {
			return nil
		}
	}
}

func createGame(gamesDir string) error {
	f, err := forms.NewGameForm("")
	if err != nil {
		return err
	}
	slug := model.Slugify(f.Title)

	path, err := model.CreateGameFile(gamesDir, slug, f)
	if err != nil {
		return err
	}
	fmt.Printf("Created %s\n", path)
	return nil
}

func logForGame(g model.GameSummary) error {
	doc, err := model.LoadDoc(g.Path)
	if err != nil {
		return err
	}
	pf, err := model.LoadPlaythroughs(filepath.Dir(g.Path))
	if err != nil {
		return err
	}

	playthroughs := pf.Views()

	fmt.Print(commands.FormatReviewCard(g, doc.FM, pf))

	options := []huh.Option[string]{
		huh.NewOption("Edit game info (title/platform/status/dates/rating/draft)", actionEditInfo),
	}
	options = append(options, huh.NewOption("Start a new playthrough", actionNewPlaythrough))
	options = append(options, huh.NewOption("Add a planned replay", actionAddPlanned))
	if hasPlannedEntry(playthroughs) {
		options = append(options, huh.NewOption("Manage a planned playthrough", actionManagePlanned))
	}
	if len(playthroughs) > 0 {
		options = append(options,
			huh.NewOption("Log a new session", actionNewSession),
			huh.NewOption("Update a playthrough (finish/status/rating/notes)", actionUpdate),
		)
		if hasManageableSessions(playthroughs) {
			options = append(options, huh.NewOption("Manage sessions (edit/delete/split)", actionManageSessions))
		}
	}

	var action string
	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().Title(fmt.Sprintf("%s — what do you want to do?", g.Title)).Options(options...).Value(&action),
		),
	).Run(); err != nil {
		return err
	}

	switch action {
	case actionEditInfo:
		return doEditGameInfo(doc, pf)
	case actionNewPlaythrough:
		return doNewPlaythrough(pf, doc)
	case actionNewSession:
		return doNewSession(pf, playthroughs)
	case actionUpdate:
		return doUpdate(pf, playthroughs, doc.FM.Platform)
	case actionManageSessions:
		return doManageSessions(pf, playthroughs)
	case actionAddPlanned:
		return doAddPlannedReplay(pf, doc)
	case actionManagePlanned:
		return doManagePlanned(pf, playthroughs, doc.FM.Platform)
	}
	return nil
}

// hasManageableSessions reports whether at least one playthrough has been
// converted to a sessions list — entries still on flat started/finished have
// nothing "Manage sessions" can act on.
func hasManageableSessions(playthroughs []model.Playthrough) bool {
	for _, p := range playthroughs {
		if p.HasSessions() {
			return true
		}
	}
	return false
}

// hasPlannedEntry reports whether any playthrough is a planned-replay
// placeholder — gates the "Manage a planned playthrough" menu option, same
// shape as hasManageableSessions above.
func hasPlannedEntry(playthroughs []model.Playthrough) bool {
	for _, p := range playthroughs {
		if p.Status == "planned" {
			return true
		}
	}
	return false
}

// doAddPlannedReplay logs a dateless "planned" placeholder — intent to play
// (or replay) this game, with no start date yet. Guards against a duplicate
// bookmark for the same effective platform the same way oneShotConflict
// guards ongoing/multiplayer, but planned isn't part of that family: it has
// no save file to fragment, it would just be two bookmarks for one intent.
func doAddPlannedReplay(pf *model.PlaythroughsFile, doc *model.Doc) error {
	platform, notes, err := forms.PlannedReplayForm(doc.FM.Platform, "", "")
	if err != nil {
		return err
	}
	if mutate.HasPlannedFor(pf.Playthroughs, platform, doc.FM.Platform) {
		want := model.EffectivePlatform(platform, doc.FM.Platform)
		return fmt.Errorf("%s already has a planned replay on %s", doc.FM.Title, forms.OrDash(want))
	}
	pf.AddPlaythrough(model.PlaythroughFields{Status: "planned", Platform: platform, Notes: notes})
	return mutate.ConfirmAndWrite(pf, len(pf.Playthroughs)-1)
}
func doNewPlaythrough(pf *model.PlaythroughsFile, doc *model.Doc) error {
	// Prefill the status with the game's own front-matter status when that's
	// a status an entry can actually carry (ongoing/multiplayer) — keeps the
	// common single-mode case (this game's one-and-only entry) a one-
	// keystroke confirm, same as before per-entry ongoing/multiplayer
	// existed. "software" isn't in playthroughStatuses (it describes the
	// whole record, not a mode), so it falls through to "playing".
	defaultStatus := "playing"
	if doc.FM.Status == "ongoing" || doc.FM.Status == "multiplayer" {
		defaultStatus = doc.FM.Status
	}
	f, err := forms.PlaythroughForm(doc.FM.Platform, defaultStatus)
	if err != nil {
		return err
	}
	if conflict := mutate.OneShotConflict(pf.Playthroughs, f.Status, f.Platform, doc.FM.Platform, doc.FM.Status); conflict != nil {
		want := model.EffectivePlatform(f.Platform, doc.FM.Platform)
		return fmt.Errorf("%s already has a %s playthrough on %s — log a new session instead",
			doc.FM.Title, f.Status, forms.OrDash(want))
	}
	pf.AddPlaythrough(f)
	return mutate.ConfirmAndWrite(pf, len(pf.Playthroughs)-1)
}

// doEditGameInfo edits a game's front-matter fields (title/platform/status/
// dates/rating/draft), mirroring doUpdate's shape for playthroughs.
func doEditGameInfo(doc *model.Doc, pf *model.PlaythroughsFile) error {
	updated, err := forms.EditGameForm(doc.FM)
	if err != nil {
		return err
	}
	if mutate.SameGameInfo(doc.FM, updated) {
		fmt.Println("No changes.")
		return nil
	}
	doc.FM = updated

	// Unlike playthroughs.yaml's entries, front-matter scalar fields aren't
	// omitempty — clearing one leaves the key present (now blank/null)
	// rather than dropping it, matching how today's hand-authored files
	// always keep `started:`/`rating:` visible even when unset. So there's
	// never a legitimate removal to declare here.
	return mutate.ConfirmAndWriteFrontMatter(doc, pf)
}
func doNewSession(pf *model.PlaythroughsFile, playthroughs []model.Playthrough) error {
	idx, err := forms.SelectPlaythrough(playthroughs)
	if err != nil {
		return err
	}
	started, finished, title, err := forms.SessionForm(forms.Today(), "")
	if err != nil {
		return err
	}
	return mutate.AddSessionAndWrite(pf, idx, playthroughs[idx].HasSessions(), started, finished, title)
}
func doUpdate(pf *model.PlaythroughsFile, playthroughs []model.Playthrough, gamePlatform string) error {
	// A planned placeholder isn't a real playthrough yet — UpdateForm's
	// status select is built from playthroughStatuses, which deliberately
	// excludes "planned", so one reaching this picker would show a status
	// not among its own options. Route those through "Manage a planned
	// playthrough" instead.
	var selectable []model.Playthrough
	for _, p := range playthroughs {
		if p.Status != "planned" {
			selectable = append(selectable, p)
		}
	}
	idx, err := forms.SelectPlaythrough(selectable)
	if err != nil {
		return err
	}
	p := playthroughs[idx]

	// The "finished" field being edited lives on the last session, not the
	// entry itself, once a playthrough has been converted to sessions.
	display := p
	if p.HasSessions() {
		display.Finished = p.Sessions[len(p.Sessions)-1].Finished
	}

	finished, status, platform, rating, notes, err := forms.UpdateForm(display, gamePlatform)
	if err != nil {
		return err
	}
	if finished == display.Finished && status == p.Status && platform == p.Platform &&
		rating == p.Rating && notes == p.Notes {
		fmt.Println("No changes.")
		return nil
	}

	if err := pf.UpdatePlaythrough(idx, finished, status, platform, rating, notes); err != nil {
		return err
	}

	// Clearing a field drops its key. That's the user's intent here, so it's
	// declared rather than treated as loss — anything *else* vanishing isn't.
	var allowed []string
	base := fmt.Sprintf("playthroughs[%d]", idx)
	if strings.TrimSpace(notes) == "" {
		allowed = append(allowed, base+".notes")
	}
	if strings.TrimSpace(rating) == "" {
		allowed = append(allowed, base+".rating")
	}
	if strings.TrimSpace(status) == "" {
		allowed = append(allowed, base+".status")
	}
	// Clearing platform is how a run is handed back to the game's front
	// matter, so the key going away is the intent, not loss.
	if strings.TrimSpace(platform) == "" {
		allowed = append(allowed, base+".platform")
	}
	if strings.TrimSpace(finished) == "" {
		allowed = append(allowed, base+".finished",
			fmt.Sprintf("%s.sessions[%d].finished", base, len(p.Sessions)-1))
	}
	return mutate.ConfirmAndWrite(pf, idx, allowed...)
}

// doManagePlanned lets the user pick a planned-replay placeholder and either
// graduate it into a real playthrough or edit its platform/notes in place.
func doManagePlanned(pf *model.PlaythroughsFile, playthroughs []model.Playthrough, gamePlatform string) error {
	var planned []model.Playthrough
	for _, p := range playthroughs {
		if p.Status == "planned" {
			planned = append(planned, p)
		}
	}
	idx, err := forms.SelectPlaythrough(planned)
	if err != nil {
		return err
	}
	var p model.Playthrough
	for _, cand := range planned {
		if cand.Index == idx {
			p = cand
			break
		}
	}

	action, err := forms.SelectPlannedAction()
	if err != nil {
		return err
	}
	switch action {
	case forms.PlannedActionStart:
		return doStartPlanned(pf, idx, gamePlatform)
	case forms.PlannedActionEdit:
		return doEditPlanned(pf, idx, gamePlatform, p)
	}
	return nil
}

func doStartPlanned(pf *model.PlaythroughsFile, idx int, gamePlatform string) error {
	f, err := forms.PlaythroughForm(gamePlatform, "playing")
	if err != nil {
		return err
	}
	if err := pf.GraduatePlannedPlaythrough(idx, f); err != nil {
		return err
	}
	return mutate.ConfirmAndWrite(pf, idx)
}

func doEditPlanned(pf *model.PlaythroughsFile, idx int, gamePlatform string, p model.Playthrough) error {
	platform, notes, err := forms.PlannedReplayForm(gamePlatform, p.Platform, p.Notes)
	if err != nil {
		return err
	}
	if platform == p.Platform && notes == p.Notes {
		fmt.Println("No changes.")
		return nil
	}
	if err := pf.EditPlanned(idx, platform, notes); err != nil {
		return err
	}

	var allowed []string
	base := fmt.Sprintf("playthroughs[%d]", idx)
	if strings.TrimSpace(platform) == "" {
		allowed = append(allowed, base+".platform")
	}
	if strings.TrimSpace(notes) == "" {
		allowed = append(allowed, base+".notes")
	}
	return mutate.ConfirmAndWrite(pf, idx, allowed...)
}

// doManageSessions lets the user pick a playthrough already converted to
// sessions, pick one of its sessions, then edit/delete/split it.
func doManageSessions(pf *model.PlaythroughsFile, playthroughs []model.Playthrough) error {
	var withSessions []model.Playthrough
	for _, p := range playthroughs {
		if p.HasSessions() {
			withSessions = append(withSessions, p)
		}
	}
	idx, err := forms.SelectPlaythrough(withSessions)
	if err != nil {
		return err
	}
	var p model.Playthrough
	for _, cand := range withSessions {
		if cand.Index == idx {
			p = cand
			break
		}
	}

	sIdx, err := forms.SelectSession(p.Sessions)
	if err != nil {
		return err
	}

	n := len(p.Sessions)
	action, err := forms.SelectSessionAction(n >= 2, sIdx < n-1)
	if err != nil {
		return err
	}

	switch action {
	case forms.SessionActionEdit:
		return doEditSession(pf, idx, sIdx, p.Sessions[sIdx])
	case forms.SessionActionDelete:
		return doRemoveSession(pf, idx, sIdx)
	case forms.SessionActionSplit:
		return doSplitPlaythrough(pf, idx, sIdx, p.Status)
	}
	return nil
}

func doEditSession(pf *model.PlaythroughsFile, idx, j int, s model.Session) error {
	started, finished, title, err := forms.EditSessionForm(s)
	if err != nil {
		return err
	}
	if started == s.Started && finished == s.Finished && title == s.Title {
		fmt.Println("No changes.")
		return nil
	}
	if err := pf.EditSession(idx, j, started, finished, title); err != nil {
		return err
	}

	// Clearing a title drops its key, same as doUpdate's field-clearing cases.
	var allowed []string
	if strings.TrimSpace(title) == "" && s.Title != "" {
		allowed = append(allowed, model.SessionPath(idx, j, "title"))
	}
	return mutate.ConfirmAndWrite(pf, idx, allowed...)
}

func doRemoveSession(pf *model.PlaythroughsFile, idx, j int) error {
	before := append([]model.SessionEntry(nil), pf.Playthroughs[idx].Sessions...)
	if err := pf.RemoveSession(idx, j); err != nil {
		return err
	}
	return mutate.ConfirmAndWrite(pf, idx, model.RemoveSessionAllowedPaths(idx, before, j)...)
}

// doSplitPlaythrough splits playthrough idx after session keepLast — the
// session the user picked as the last one to stay in the original — moving
// everything after it into a new entry.
func doSplitPlaythrough(pf *model.PlaythroughsFile, idx, keepLast int, defaultStatus string) error {
	newStatus, err := forms.SplitStatusForm(defaultStatus)
	if err != nil {
		return err
	}
	before := append([]model.SessionEntry(nil), pf.Playthroughs[idx].Sessions...)
	splitFrom := keepLast + 1
	newIdx, err := pf.SplitPlaythrough(idx, splitFrom, newStatus)
	if err != nil {
		return err
	}
	allowed := model.TruncateSessionAllowedPaths(idx, before, splitFrom)
	return mutate.ConfirmAndWriteSplit(pf, idx, newIdx, allowed...)
}
