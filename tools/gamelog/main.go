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
  gamelog project             regenerate every game's achievement-summary.yaml
                             from the archive already on disk (no API calls)
  gamelog stale [flags]       walk through "playing" Steam games that have
                             gone quiet, suggesting finished/dropped
                               --days N        quiet threshold (default 30)
  gamelog close [flags]       cap playthroughs left without a closing date at
                             the last day they were played; statuses unchanged
                               --days N        quiet threshold (default 30)
                               --all           include "playing" games too
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

	// A session-based, multiplayer, or software entry (no start/finish
	// narrative — see gameStatuses) gets exactly one playthrough entry
	// *per platform*. The cap exists so an open-ended game can't fragment
	// into a series of bogus discrete playthroughs — but a second platform
	// isn't fragmentation. Saves don't cross consoles, so playing it on PS4
	// and on PC really is two separate records with two separate achievement
	// sets. doNewPlaythrough enforces the per-platform half, which can only
	// be checked once the form has collected the platform.
	oneShot := isOneShot(doc.FM.Status)

	options := []huh.Option[string]{
		huh.NewOption("Edit game info (title/platform/status/dates/rating/draft)", actionEditInfo),
	}
	options = append(options, huh.NewOption("Start a new playthrough", actionNewPlaythrough))
	if len(playthroughs) > 0 {
		options = append(options,
			huh.NewOption("Log a new session", actionNewSession),
			huh.NewOption("Update a playthrough (finish/status/rating/notes)", actionUpdate),
		)
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
		return doNewPlaythrough(pf, doc, oneShot)
	case actionNewSession:
		return doNewSession(pf, playthroughs)
	case actionUpdate:
		return doUpdate(pf, playthroughs, doc.FM.Platform)
	}
	return nil
}

func doNewPlaythrough(pf *PlaythroughsFile, doc *Doc, oneShot bool) error {
	f, err := PlaythroughForm(doc.FM.Platform)
	if err != nil {
		return err
	}
	// The one-shot cap is per platform: a second entry is only meaningful
	// when it's a second platform, since that's a separate save and a
	// separate achievement set. Same platform means "log a session" instead.
	if oneShot {
		want := effectivePlatform(f.Platform, doc.FM.Platform)
		for _, e := range pf.Playthroughs {
			if effectivePlatform(e.Platform, doc.FM.Platform) == want {
				return fmt.Errorf("%s is %s and already has a playthrough on %s — log a new session instead",
					doc.FM.Title, doc.FM.Status, orDash(want))
			}
		}
	}
	pf.AddPlaythrough(f)
	return confirmAndWrite(pf, len(pf.Playthroughs)-1)
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
	started, finished, title, err := SessionForm()
	if err != nil {
		return err
	}

	hadSessions := playthroughs[idx].HasSessions()
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
	idx, err := SelectPlaythrough(playthroughs)
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
