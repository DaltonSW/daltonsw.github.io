package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
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

const usage = `gamelog — maintain this site's game log in content/games/ and archive/.

Usage:
  gamelog                    interactive: pick or create a game, then log a
                             playthrough, log a session, or update one
  gamelog suggest [slug]     print suggested playthrough dates from
                             RetroAchievements/Steam (read-only, never writes)
  gamelog scan [flags]       find games on RetroAchievements/Steam that aren't
                             logged yet, then optionally create entries for them
                               --min-hours N   Steam playtime floor (default 5)
  gamelog review             walk through draft games one at a time: publish,
                             edit, mark dropped, skip, or delete the stub
  gamelog achievements [slug]
                             capture the full unlock history into
                             archive/<provider>/<id>.json (re-run to refresh)
  gamelog achievements --all refresh every game that has a provider link,
                             instead of one slug at a time
  gamelog project             regenerate every game's achievement-summary.yaml
                             from the archive already on disk (no API calls)
  gamelog stale [flags]       walk through "playing" Steam games that have
                             gone quiet, suggesting finished/dropped
                               --days N        quiet threshold (default 30)
  gamelog close [flags]       cap playthroughs left without a closing date at
                             the last day they were played; statuses unchanged
                               --days N        quiet threshold (default 30)
                               --all           include "playing" games too
  gamelog serve [flags]       serve a local web UI over content/games, as an
                             alternative to the interactive terminal flow
                               --port N        port to listen on (default 8080)
  gamelog help               show this message

Environment (or a .env beside this tool; real env vars take precedence):
  RA_USERNAME, RA_API_KEY    https://retroachievements.org/settings
  STEAM_API_KEY              https://steamcommunity.com/dev/apikey
  STEAM_ID                   SteamID64 or profile vanity name
                             (STEAM_USER_ID works too)

See tools/gamelog/README.md for details.`

func isHelpFlag(s string) bool {
	switch s {
	case "help", "-h", "--help":
		return true
	}
	return false
}

func main() {
	args := os.Args[1:]

	if len(args) > 0 && isHelpFlag(args[0]) {
		fmt.Println(usage)
		return
	}

	var err error
	switch {
	case len(args) == 0:
		err = run()
	case args[0] == "suggest":
		if len(args) > 1 && isHelpFlag(args[1]) {
			fmt.Println(usage)
			return
		}
		err = runSuggest(args[1:])
	case args[0] == "scan":
		err = runScan(args[1:])
	case args[0] == "review":
		if len(args) > 1 && isHelpFlag(args[1]) {
			fmt.Println(usage)
			return
		}
		err = runReview(args[1:])
	case args[0] == "achievements":
		if len(args) > 1 && isHelpFlag(args[1]) {
			fmt.Println(usage)
			return
		}
		err = runAchievements(args[1:])
	case args[0] == "project":
		err = runProject(args[1:])
	case args[0] == "stale":
		err = runStale(args[1:])
	case args[0] == "close":
		err = runClose(args[1:])
	case args[0] == "serve":
		err = runServe(args[1:])
	default:
		// Previously any unknown argument silently opened the interactive
		// form, which made a typo look like the tool ignoring you.
		fmt.Fprintf(os.Stderr, "gamelog: unknown command %q\n\n%s\n", args[0], usage)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "gamelog:", err)
		os.Exit(1)
	}
}

func run() error {
	gamesDir, err := findGamesDir()
	if err != nil {
		return err
	}

	for {
		games, err := ListGames(gamesDir)
		if err != nil {
			return err
		}

		slug, err := SelectGame(games)
		if err != nil {
			return err
		}

		if slug == newGameSentinel {
			if err := createGame(gamesDir); err != nil {
				return err
			}
		} else {
			var summary GameSummary
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
	f, err := NewGameForm("")
	if err != nil {
		return err
	}
	slug := Slugify(f.Title)

	path, err := CreateGameFile(gamesDir, slug, f)
	if err != nil {
		return err
	}
	fmt.Printf("Created %s\n", path)
	return nil
}

func logForGame(g GameSummary) error {
	doc, err := LoadDoc(g.Path)
	if err != nil {
		return err
	}
	pf, err := LoadPlaythroughs(filepath.Dir(g.Path))
	if err != nil {
		return err
	}

	playthroughs := pf.Views()

	fmt.Print(formatReviewCard(g, doc.FM, pf))

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
func hasManageableSessions(playthroughs []Playthrough) bool {
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
func hasPlannedEntry(playthroughs []Playthrough) bool {
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
func doAddPlannedReplay(pf *PlaythroughsFile, doc *Doc) error {
	platform, notes, err := PlannedReplayForm(doc.FM.Platform, "", "")
	if err != nil {
		return err
	}
	if hasPlannedFor(pf.Playthroughs, platform, doc.FM.Platform) {
		want := effectivePlatform(platform, doc.FM.Platform)
		return fmt.Errorf("%s already has a planned replay on %s", doc.FM.Title, orDash(want))
	}
	pf.AddPlaythrough(PlaythroughFields{Status: "planned", Platform: platform, Notes: notes})
	return confirmAndWrite(pf, len(pf.Playthroughs)-1)
}

// hasPlannedFor reports whether playthroughs already has a planned-replay
// placeholder for the given effective platform.
func hasPlannedFor(playthroughs []PlaythroughEntry, entryPlatform, gamePlatform string) bool {
	want := effectivePlatform(entryPlatform, gamePlatform)
	for _, e := range playthroughs {
		if e.Status == "planned" && effectivePlatform(e.Platform, gamePlatform) == want {
			return true
		}
	}
	return false
}

func doNewPlaythrough(pf *PlaythroughsFile, doc *Doc) error {
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
	f, err := PlaythroughForm(doc.FM.Platform, defaultStatus)
	if err != nil {
		return err
	}
	if conflict := oneShotConflict(pf.Playthroughs, f.Status, f.Platform, doc.FM.Platform, doc.FM.Status); conflict != nil {
		want := effectivePlatform(f.Platform, doc.FM.Platform)
		return fmt.Errorf("%s already has a %s playthrough on %s — log a new session instead",
			doc.FM.Title, f.Status, orDash(want))
	}
	pf.AddPlaythrough(f)
	return confirmAndWrite(pf, len(pf.Playthroughs)-1)
}

// oneShotConflict reports the existing entry that already covers a one-shot
// status (ongoing/multiplayer) on the given platform, if any — that's the
// case doNewPlaythrough refuses. None of the one-shot statuses has a save
// file or finish line, so a *second* entry of the same one-shot status on
// the same platform would be fragmentation, not a distinct mode; a
// differently-statused entry (a finished campaign alongside an ongoing
// sandbox mode, say) is a real second mode and is allowed to coexist. A
// second platform is never a conflict either way — saves don't cross
// consoles, so that's a genuinely separate record.
//
// gameStatus resolves entries written before per-entry ongoing/multiplayer
// existed — see effectiveOneShotStatus.
func oneShotConflict(playthroughs []PlaythroughEntry, status, entryPlatform, gamePlatform, gameStatus string) *PlaythroughEntry {
	if !isOneShot(status) {
		return nil
	}
	want := effectivePlatform(entryPlatform, gamePlatform)
	for i, e := range playthroughs {
		if effectiveOneShotStatus(e.Status, gameStatus) == status && effectivePlatform(e.Platform, gamePlatform) == want {
			return &playthroughs[i]
		}
	}
	return nil
}

// effectiveOneShotStatus resolves what one-shot status, if any, an existing
// entry really represents. Every ongoing/multiplayer/software game written
// before per-entry ongoing/multiplayer support kept its sole entry's own
// status as "playing" and relied entirely on the game's front-matter status
// for its real meaning (games-timeline.html's override still does this for
// display). Without this, a new literal "ongoing" entry wouldn't be seen as
// conflicting with that old-style "playing" entry, silently dropping the
// fragmentation guard for every game written the old way.
func effectiveOneShotStatus(entryStatus, gameStatus string) string {
	if entryStatus == "playing" && isOneShot(gameStatus) {
		return gameStatus
	}
	return entryStatus
}

// doEditGameInfo edits a game's front-matter fields (title/platform/status/
// dates/rating/draft), mirroring doUpdate's shape for playthroughs.
func doEditGameInfo(doc *Doc, pf *PlaythroughsFile) error {
	updated, err := EditGameForm(doc.FM)
	if err != nil {
		return err
	}
	if sameGameInfo(doc.FM, updated) {
		fmt.Println("No changes.")
		return nil
	}
	doc.FM = updated

	// Unlike playthroughs.yaml's entries, front-matter scalar fields aren't
	// omitempty — clearing one leaves the key present (now blank/null)
	// rather than dropping it, matching how today's hand-authored files
	// always keep `started:`/`rating:` visible even when unset. So there's
	// never a legitimate removal to declare here.
	return confirmAndWriteFrontMatter(doc, pf)
}

func sameGameInfo(a, b FrontMatter) bool {
	return a.Title == b.Title && a.Platform == b.Platform && a.Status == b.Status &&
		a.Started == b.Started && a.Finished == b.Finished && a.Draft == b.Draft &&
		a.RatingString() == b.RatingString()
}

// confirmAndWriteFrontMatter is confirmAndWrite's front-matter analog:
// preview, confirm, prove the rewrite loses nothing, then write. When pf is
// non-nil, it also syncs and writes the playthrough via SyncStatus under the
// same confirmation, so status and the timeline never drift apart again.
func confirmAndWriteFrontMatter(doc *Doc, pf *PlaythroughsFile, allowedRemovals ...string) error {
	fm, err := doc.encodeFM()
	if err != nil {
		return err
	}
	preview := strings.Split(strings.TrimRight(string(fm), "\n"), "\n")

	syncing := pf != nil && pf.SyncStatus(doc.FM.Status, doc.FM.Finished)
	if syncing {
		entryPreview, err := previewEntry(pf, 0)
		if err != nil {
			return err
		}
		preview = append(preview, "", "playthroughs.yaml:")
		preview = append(preview, entryPreview...)
	}

	ok, err := ConfirmWrite(doc.Path, preview)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println("Discarded.")
		return nil
	}

	return writeFrontMatter(doc, pf, syncing, allowedRemovals...)
}

// writeFrontMatter is confirmAndWriteFrontMatter's no-prompt tail — see
// writeEntry. syncing must match what the caller already decided (whether
// pf's status was brought in line with doc.FM.Status), since that's what
// decides whether pf needs its own loss-check and save alongside doc's.
func writeFrontMatter(doc *Doc, pf *PlaythroughsFile, syncing bool, allowedRemovals ...string) error {
	fm, err := doc.encodeFM()
	if err != nil {
		return err
	}

	oldFields, err := collectYAMLFields(doc.fmRaw)
	if err != nil {
		return err
	}
	newFields, err := collectYAMLFields(fm)
	if err != nil {
		return fmt.Errorf("refusing to write %s — the result would not parse: %w", doc.Path, err)
	}
	if err := checkNoFieldLoss(oldFields, newFields, allowedRemovals); err != nil {
		return err
	}
	if syncing {
		if err := verifyNoLoss(pf, nil); err != nil {
			return err
		}
	}

	if err := doc.Save(); err != nil {
		return err
	}
	fmt.Printf("Saved %s\n", doc.Path)

	if syncing {
		if err := pf.Save(); err != nil {
			return err
		}
		fmt.Printf("Saved %s\n", pf.Path)
	}
	return nil
}

func doNewSession(pf *PlaythroughsFile, playthroughs []Playthrough) error {
	idx, err := SelectPlaythrough(playthroughs)
	if err != nil {
		return err
	}
	started, finished, title, err := SessionForm(today(), "")
	if err != nil {
		return err
	}
	return addSessionAndWrite(pf, idx, playthroughs[idx].HasSessions(), started, finished, title)
}

// addSessionAndWrite logs a session against an existing playthrough and
// writes it. hadSessions must reflect the entry's state *before* this call —
// converting a flat started/finished pair into a sessions list is this
// call's own doing, not data loss, so the check needs to know whether that
// conversion is about to happen.
func addSessionAndWrite(pf *PlaythroughsFile, idx int, hadSessions bool, started, finished, title string) error {
	if err := pf.AddSession(idx, started, finished, title); err != nil {
		return err
	}

	// Converting to sessions moves the flat date pair into the list rather
	// than dropping it, so those two paths are expected to disappear.
	var allowed []string
	if !hadSessions {
		allowed = []string{
			fmt.Sprintf("playthroughs[%d].started", idx),
			fmt.Sprintf("playthroughs[%d].finished", idx),
		}
	}
	return confirmAndWrite(pf, idx, allowed...)
}

func doUpdate(pf *PlaythroughsFile, playthroughs []Playthrough, gamePlatform string) error {
	// A planned placeholder isn't a real playthrough yet — UpdateForm's
	// status select is built from playthroughStatuses, which deliberately
	// excludes "planned", so one reaching this picker would show a status
	// not among its own options. Route those through "Manage a planned
	// playthrough" instead.
	var selectable []Playthrough
	for _, p := range playthroughs {
		if p.Status != "planned" {
			selectable = append(selectable, p)
		}
	}
	idx, err := SelectPlaythrough(selectable)
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

	finished, status, platform, rating, notes, err := UpdateForm(display, gamePlatform)
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
	return confirmAndWrite(pf, idx, allowed...)
}

// doManagePlanned lets the user pick a planned-replay placeholder and either
// graduate it into a real playthrough or edit its platform/notes in place.
func doManagePlanned(pf *PlaythroughsFile, playthroughs []Playthrough, gamePlatform string) error {
	var planned []Playthrough
	for _, p := range playthroughs {
		if p.Status == "planned" {
			planned = append(planned, p)
		}
	}
	idx, err := SelectPlaythrough(planned)
	if err != nil {
		return err
	}
	var p Playthrough
	for _, cand := range planned {
		if cand.Index == idx {
			p = cand
			break
		}
	}

	action, err := SelectPlannedAction()
	if err != nil {
		return err
	}
	switch action {
	case plannedActionStart:
		return doStartPlanned(pf, idx, gamePlatform)
	case plannedActionEdit:
		return doEditPlanned(pf, idx, gamePlatform, p)
	}
	return nil
}

func doStartPlanned(pf *PlaythroughsFile, idx int, gamePlatform string) error {
	f, err := PlaythroughForm(gamePlatform, "playing")
	if err != nil {
		return err
	}
	if err := pf.GraduatePlannedPlaythrough(idx, f); err != nil {
		return err
	}
	return confirmAndWrite(pf, idx)
}

func doEditPlanned(pf *PlaythroughsFile, idx int, gamePlatform string, p Playthrough) error {
	platform, notes, err := PlannedReplayForm(gamePlatform, p.Platform, p.Notes)
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
	return confirmAndWrite(pf, idx, allowed...)
}

// doManageSessions lets the user pick a playthrough already converted to
// sessions, pick one of its sessions, then edit/delete/split it.
func doManageSessions(pf *PlaythroughsFile, playthroughs []Playthrough) error {
	var withSessions []Playthrough
	for _, p := range playthroughs {
		if p.HasSessions() {
			withSessions = append(withSessions, p)
		}
	}
	idx, err := SelectPlaythrough(withSessions)
	if err != nil {
		return err
	}
	var p Playthrough
	for _, cand := range withSessions {
		if cand.Index == idx {
			p = cand
			break
		}
	}

	sIdx, err := SelectSession(p.Sessions)
	if err != nil {
		return err
	}

	n := len(p.Sessions)
	action, err := SelectSessionAction(n >= 2, sIdx < n-1)
	if err != nil {
		return err
	}

	switch action {
	case sessionActionEdit:
		return doEditSession(pf, idx, sIdx, p.Sessions[sIdx])
	case sessionActionDelete:
		return doRemoveSession(pf, idx, sIdx)
	case sessionActionSplit:
		return doSplitPlaythrough(pf, idx, sIdx, p.Status)
	}
	return nil
}

func doEditSession(pf *PlaythroughsFile, idx, j int, s Session) error {
	started, finished, title, err := EditSessionForm(s)
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
		allowed = append(allowed, sessionPath(idx, j, "title"))
	}
	return confirmAndWrite(pf, idx, allowed...)
}

func doRemoveSession(pf *PlaythroughsFile, idx, j int) error {
	before := append([]SessionEntry(nil), pf.Playthroughs[idx].Sessions...)
	if err := pf.RemoveSession(idx, j); err != nil {
		return err
	}
	return confirmAndWrite(pf, idx, removeSessionAllowedPaths(idx, before, j)...)
}

// doSplitPlaythrough splits playthrough idx after session keepLast — the
// session the user picked as the last one to stay in the original — moving
// everything after it into a new entry.
func doSplitPlaythrough(pf *PlaythroughsFile, idx, keepLast int, defaultStatus string) error {
	newStatus, err := SplitStatusForm(defaultStatus)
	if err != nil {
		return err
	}
	before := append([]SessionEntry(nil), pf.Playthroughs[idx].Sessions...)
	splitFrom := keepLast + 1
	newIdx, err := pf.SplitPlaythrough(idx, splitFrom, newStatus)
	if err != nil {
		return err
	}
	allowed := truncateSessionAllowedPaths(idx, before, splitFrom)
	return confirmAndWriteSplit(pf, idx, newIdx, allowed...)
}

// confirmAndWrite previews the affected entry, and on confirmation proves the
// rewrite loses nothing before letting it reach disk. allowedRemovals names
// the paths this particular operation means to drop; every other field
// present beforehand must still be there afterwards.
//
// The check compares against the bytes actually on disk, not against the
// in-memory entries, which the operation has already mutated in place.
func confirmAndWrite(pf *PlaythroughsFile, focus int, allowedRemovals ...string) error {
	preview, err := previewEntry(pf, focus)
	if err != nil {
		return err
	}
	ok, err := ConfirmWrite(pf.Path, preview)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println("Discarded.")
		return nil
	}

	return writeEntry(pf, allowedRemovals...)
}

// writeEntry is confirmAndWrite's no-prompt tail: verify-no-loss, save,
// report. Split out so a caller that has already gotten its confirmation some
// other way — a submitted web form, say — can reach the same safety net
// without going through a terminal prompt.
func writeEntry(pf *PlaythroughsFile, allowedRemovals ...string) error {
	if err := verifyNoLoss(pf, allowedRemovals); err != nil {
		return err
	}
	if err := pf.Save(); err != nil {
		return err
	}
	fmt.Printf("Saved %s\n", pf.Path)
	return nil
}

// confirmAndWriteSplit is confirmAndWrite's two-entry analog: previews both
// the shrunk source entry and the new split-off entry, confirms once, then
// writes.
func confirmAndWriteSplit(pf *PlaythroughsFile, srcIdx, newIdx int, allowedRemovals ...string) error {
	srcPreview, err := previewEntry(pf, srcIdx)
	if err != nil {
		return err
	}
	newPreview, err := previewEntry(pf, newIdx)
	if err != nil {
		return err
	}
	preview := append(append(append([]string{}, srcPreview...), "", "new playthrough:"), newPreview...)

	ok, err := ConfirmWrite(pf.Path, preview)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println("Discarded.")
		return nil
	}

	return writeSplit(pf, allowedRemovals...)
}

// writeSplit is confirmAndWriteSplit's no-prompt tail — see writeEntry.
func writeSplit(pf *PlaythroughsFile, allowedRemovals ...string) error {
	if err := verifyNoLoss(pf, allowedRemovals); err != nil {
		return err
	}
	if err := pf.Save(); err != nil {
		return err
	}
	fmt.Printf("Saved %s\n", pf.Path)
	return nil
}

// previewEntry renders just the entry being changed. Showing the whole file
// would bury a one-line edit in a game with a dozen playthroughs.
func previewEntry(pf *PlaythroughsFile, focus int) ([]string, error) {
	if focus < 0 || focus >= len(pf.Playthroughs) {
		return nil, fmt.Errorf("no playthrough %d to preview", focus+1)
	}
	one := &PlaythroughsFile{Playthroughs: pf.Playthroughs[focus : focus+1]}
	out, err := one.Encode()
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimRight(string(out), "\n"), "\n"), nil
}

// verifyNoLoss compares the pending rewrite against the entries as they were
// read. The file is now encoded whole rather than patched, so the failure
// mode is a field the struct doesn't model being dropped on the way through —
// `Extra` is what catches those, and this is what proves it did.
func verifyNoLoss(pf *PlaythroughsFile, allowedRemovals []string) error {
	newBytes, err := pf.Encode()
	if err != nil {
		return err
	}
	oldFields, err := collectYAMLFields(pf.Raw())
	if err != nil {
		return err
	}
	newFields, err := collectYAMLFields(newBytes)
	if err != nil {
		return fmt.Errorf("refusing to write %s — the result would not parse: %w", pf.Path, err)
	}
	return checkNoFieldLoss(oldFields, newFields, allowedRemovals)
}
