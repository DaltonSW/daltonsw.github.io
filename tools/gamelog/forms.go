package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
)

const newGameSentinel = "__new__"

func validateDate(required bool) func(string) error {
	return func(s string) error {
		if s == "" {
			if required {
				return fmt.Errorf("a date is required (YYYY-MM-DD)")
			}
			return nil
		}
		if _, err := time.Parse("2006-01-02", s); err != nil {
			return fmt.Errorf("must be a real YYYY-MM-DD date or blank")
		}
		return nil
	}
}

func validateRating(s string) error {
	if s == "" {
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 10 {
		return fmt.Errorf("must be 1-10 or blank")
	}
	return nil
}

// today is the current date in the site's timezone, so a date typed into a
// form matches the day the rest of the tool would report.
func today() string {
	return time.Now().In(siteLocation).Format("2006-01-02")
}

// SelectGame prompts for an existing game, or the sentinel "create a new
// game" option. Returns slug == newGameSentinel when the latter is chosen.
func SelectGame(games []GameSummary) (string, error) {
	options := []huh.Option[string]{huh.NewOption("+ New game", newGameSentinel)}
	for _, g := range games {
		options = append(options, huh.NewOption(g.Label(), g.Slug))
	}
	var slug string
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().Title("Which game?").Options(options...).Value(&slug),
		),
	).Run()
	return slug, err
}

// SelectExistingGame prompts for one of an existing set of games, with no
// "+ New game" option — for read-only flows (like `gamelog suggest`) where
// creating a new game makes no sense.
func SelectExistingGame(games []GameSummary) (string, error) {
	if len(games) == 0 {
		return "", fmt.Errorf("no games found to choose from")
	}
	options := make([]huh.Option[string], len(games))
	for i, g := range games {
		options[i] = huh.NewOption(g.SuggestLabel(), g.Slug)
	}
	var slug string
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().Title("Which game?").Options(options...).Value(&slug),
		),
	).Run()
	return slug, err
}

// SelectCandidates asks which discovered games to create, returning indices
// into candidates. Nothing is preselected — creating content files is opt-in
// per game.
func SelectCandidates(candidates []Candidate) ([]int, error) {
	options := make([]huh.Option[int], len(candidates))
	for i, c := range candidates {
		label := fmt.Sprintf("%s  [%s]", c.Title, c.Provider)
		if c.Finished {
			label += " finished"
		}
		if c.PlaytimeMins > 0 {
			label += " " + formatHours(c.PlaytimeMins)
		}
		options[i] = huh.NewOption(label, i)
	}

	var chosen []int
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[int]().
				Title("Create entries for which games?").
				Description("space to toggle, enter to confirm — none selected creates nothing").
				Options(options...).
				Value(&chosen),
		),
	).Run()
	return chosen, err
}

// "session-based", "multiplayer", and "software" all mark an entry that
// doesn't have a meaningful start/finish narrative — it's used in an
// open-ended series of sessions with no state that ends play — for three
// different reasons: session-based is a replay-loop design
// (roguelike/sandbox/idle), multiplayer is inherently social, and software
// isn't a game at all. Steam sells tools alongside games and reports playtime
// for them identically, so they arrive through the same scan; "software" says
// the completion vocabulary simply doesn't apply rather than forcing a
// finished/dropped answer to a question that was never asked.
//
// All three get exactly one playthrough entry, ever; see isOneShot and the
// gating in logForGame.
var gameStatuses = []string{"backlog", "playing", "finished", "mastered", "dropped", "paused", "session-based", "multiplayer", "software"}

// isOneShot reports whether a game-level status forbids a second playthrough
// entry. Keep this in step with the three statuses documented above.
func isOneShot(status string) bool {
	return status == "session-based" || status == "multiplayer" || status == "software"
}

var playthroughStatuses = []string{"playing", "finished", "mastered", "dropped", "paused"}

func selectOptions(values []string) []huh.Option[string] {
	opts := make([]huh.Option[string], len(values))
	for i, v := range values {
		opts[i] = huh.NewOption(v, v)
	}
	return opts
}

// NewGameForm prompts for all fields of a brand-new game entry.
func NewGameForm(defaultTitle string) (NewGameFields, error) {
	f := NewGameFields{
		Title:   defaultTitle,
		Status:  "playing",
		Started: today(),
	}
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Title").Value(&f.Title).Validate(func(s string) error {
				if strings.TrimSpace(s) == "" {
					return fmt.Errorf("title is required")
				}
				return nil
			}),
			huh.NewInput().Title("Platform").Value(&f.Platform),
			huh.NewInput().Title("RetroAchievements game ID or URL (blank if none)").
				Value(&f.RetroAchievementsID).Validate(validateExternalID),
			huh.NewInput().Title("Steam appid or store URL (blank if none)").
				Value(&f.SteamAppID).Validate(validateExternalID),
			huh.NewSelect[string]().Title("Status").Options(selectOptions(gameStatuses)...).Value(&f.Status),
			huh.NewInput().Title("Started (YYYY-MM-DD)").Value(&f.Started).Validate(validateDate(false)),
			huh.NewInput().Title("Finished (YYYY-MM-DD, blank if ongoing)").Value(&f.Finished).Validate(validateDate(false)),
			huh.NewInput().Title("Rating (1-10, blank if none)").Value(&f.Rating).Validate(validateRating),
			huh.NewText().Title("Overview").Value(&f.Overview),
		),
	).Run()
	if err != nil {
		return f, err
	}
	// Validation above only proves the input is parseable; store the bare ID
	// so front matter never holds a URL.
	if f.RetroAchievementsID, err = parseExternalID(f.RetroAchievementsID); err != nil {
		return f, err
	}
	if f.SteamAppID, err = parseExternalID(f.SteamAppID); err != nil {
		return f, err
	}
	return f, nil
}

// EditGameForm prompts to edit a game's front-matter fields, prefilled with
// current values. RetroAchievementsID/SteamAppID and the markdown body
// aren't editable here — relinking a provider deserves its own flow.
func EditGameForm(fm FrontMatter) (FrontMatter, error) {
	f := fm
	rating := fm.RatingString()
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Title").Value(&f.Title).Validate(func(s string) error {
				if strings.TrimSpace(s) == "" {
					return fmt.Errorf("title is required")
				}
				return nil
			}),
			huh.NewInput().Title("Platform").Value(&f.Platform),
			huh.NewSelect[string]().Title("Status").Options(selectOptions(gameStatuses)...).Value(&f.Status),
			huh.NewInput().Title("Started (YYYY-MM-DD)").Value(&f.Started).Validate(validateDate(false)),
			huh.NewInput().Title("Finished (YYYY-MM-DD, blank if ongoing)").Value(&f.Finished).Validate(validateDate(false)),
			huh.NewInput().Title("Rating (1-10, blank if none)").Value(&rating).Validate(validateRating),
			huh.NewConfirm().Title("Draft (unpublished)?").Value(&f.Draft),
		),
	).Run()
	if err != nil {
		return fm, err
	}
	f.SetRating(rating)
	return f, nil
}

const (
	reviewPublish     = "publish"
	reviewEditPublish = "edit_publish"
	reviewEditOnly    = "edit_only"
	reviewDrop        = "drop"
	reviewSkip        = "skip"
	reviewDelete      = "delete"
	reviewQuit        = "quit"
)

// SelectReviewAction prompts for what to do with one draft game during
// `gamelog review`. Delete is only offered when canDelete (no captured
// playthroughs or archive history to risk).
func SelectReviewAction(canDelete bool) (string, error) {
	options := []huh.Option[string]{
		huh.NewOption("Publish as-is", reviewPublish),
		huh.NewOption("Edit, then publish", reviewEditPublish),
		huh.NewOption("Edit without publishing", reviewEditOnly),
		huh.NewOption("Mark dropped & publish", reviewDrop),
		huh.NewOption("Skip (leave draft, show again next run)", reviewSkip),
	}
	if canDelete {
		options = append(options, huh.NewOption("Delete this stub", reviewDelete))
	}
	options = append(options, huh.NewOption("Quit review", reviewQuit))

	var action string
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().Title("What next?").Options(options...).Value(&action),
		),
	).Run()
	return action, err
}

// ConfirmDelete asks for explicit confirmation before removing a game's
// directory entirely. Defaults to false — deletion is destructive and rare.
func ConfirmDelete(path string) (bool, error) {
	var ok bool
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().Title(fmt.Sprintf("Permanently delete %s?", path)).Value(&ok),
		),
	).Run()
	return ok, err
}

const (
	staleAccept = "accept"
	staleEdit   = "edit"
	staleSkip   = "skip"
	staleQuit   = "quit"
)

// staleMarkAction is the action value for one "it's actually X" quick
// correction — staleMarkPrefix + a status, e.g. "mark_finished".
const staleMarkPrefix = "mark_"

func staleMarkAction(status string) string { return staleMarkPrefix + status }

// staleQuickStatuses are offered as one-click corrections alongside
// whichever status `stale` actually guessed — see SelectStaleAction.
// "paused" is never itself a guess (there's no signal for "meant to keep
// playing"), but it's always offered: distinguishing an intentional pause
// from "dropped" is exactly what a quiet game with no completion signal
// can't tell you on its own.
var staleQuickStatuses = []string{"finished", "mastered", "dropped", "paused"}

// SelectStaleAction prompts for what to do with one stale-game suggestion
// during `gamelog stale`. Nothing here writes on its own — every path either
// discards or hands off to the same confirm/loss-check write every other
// front-matter edit goes through. Alongside the guess, every other status
// `stale` might have guessed is offered as a one-click correction — no need
// to open the full edit form just to say "it was actually finished."
func SelectStaleAction(suggested string) (string, error) {
	options := []huh.Option[string]{
		huh.NewOption(fmt.Sprintf("Correct — mark %s", suggested), staleAccept),
	}
	for _, status := range staleQuickStatuses {
		if status == suggested {
			continue
		}
		options = append(options, huh.NewOption(fmt.Sprintf("Nope — mark %s instead", status), staleMarkAction(status)))
	}
	options = append(options,
		huh.NewOption("Edit before applying", staleEdit),
		huh.NewOption("Still playing / skip for now", staleSkip),
		huh.NewOption("Quit", staleQuit),
	)
	var action string
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().Title("What next?").Options(options...).Value(&action),
		),
	).Run()
	return action, err
}

// PlaythroughForm prompts for all fields of a new `playthroughs:` entry.
//
// gamePlatform is the game's front-matter platform, shown as the prompt's
// default so the common single-platform case stays one keystroke. Leaving it
// blank is meaningful: the entry then inherits the game's platform rather than
// pinning a copy of it, so correcting the game later corrects the run too.
func PlaythroughForm(gamePlatform string) (PlaythroughFields, error) {
	f := PlaythroughFields{
		Status:  "playing",
		Started: today(),
	}
	title := "Platform (blank = same as game)"
	if gamePlatform != "" {
		title = fmt.Sprintf("Platform (blank = %s)", gamePlatform)
	}
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Started (YYYY-MM-DD)").Value(&f.Started).Validate(validateDate(true)),
			huh.NewSelect[string]().Title("Status").Options(selectOptions(playthroughStatuses)...).Value(&f.Status),
			huh.NewInput().Title("Finished (YYYY-MM-DD, blank if ongoing)").Value(&f.Finished).Validate(validateDate(false)),
			huh.NewInput().Title(title).Value(&f.Platform),
			huh.NewInput().Title("Rating (1-10, blank if none)").Value(&f.Rating).Validate(validateRating),
			huh.NewText().Title("Notes").Value(&f.Notes),
		),
	).Run()
	return f, err
}

// SelectPlaythrough prompts to pick one of a game's playthroughs entries.
func SelectPlaythrough(playthroughs []Playthrough) (int, error) {
	options := make([]huh.Option[int], len(playthroughs))
	for i, p := range playthroughs {
		label := fmt.Sprintf("#%d — %s", i+1, p.Status)
		// Two runs of the same game differ by platform as often as by date
		// now, so a picker showing only status/dates can't be read.
		if p.Platform != "" {
			label += fmt.Sprintf(" on %s", p.Platform)
		}
		if p.HasSessions() {
			last := p.Sessions[len(p.Sessions)-1]
			label += fmt.Sprintf(" (%d sessions, last %s→%s)", len(p.Sessions), last.Started, orDash(last.Finished))
		} else {
			label += fmt.Sprintf(" (%s→%s)", p.Started, orDash(p.Finished))
		}
		options[i] = huh.NewOption(label, i)
	}
	var idx int
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[int]().Title("Which playthrough?").Options(options...).Value(&idx),
		),
	).Run()
	return idx, err
}

func orDash(s string) string {
	if s == "" {
		return "ongoing"
	}
	return s
}

// SessionForm prompts for a new session's date range and an optional title,
// for a session worth telling apart from the others on the playthrough bar
// (a DLC release, "wrapping up achievements", and so on).
func SessionForm() (started, finished, title string, err error) {
	started = today()
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Session started (YYYY-MM-DD)").Value(&started).Validate(validateDate(true)),
			huh.NewInput().Title("Session finished (YYYY-MM-DD, blank if still ongoing)").Value(&finished).Validate(validateDate(false)),
			huh.NewInput().Title("Session title (optional)").Value(&title),
		),
	).Run()
	return started, finished, title, err
}

// UpdateForm prompts to edit an existing playthrough's finished date,
// status, platform, rating, and notes, prefilled with its current values.
//
// gamePlatform only labels the blank case, the same way PlaythroughForm uses
// it — it is never written into the entry, so clearing this field restores
// inheritance rather than silently freezing today's game platform in place.
func UpdateForm(p Playthrough, gamePlatform string) (finished, status, platform, rating, notes string, err error) {
	finished = p.Finished
	status = p.Status
	platform = p.Platform
	rating = p.Rating
	notes = p.Notes

	title := "Platform (blank = same as game)"
	if gamePlatform != "" {
		title = fmt.Sprintf("Platform (blank = %s)", gamePlatform)
	}
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Finished (YYYY-MM-DD, blank if ongoing)").Value(&finished).Validate(validateDate(false)),
			huh.NewSelect[string]().Title("Status").Options(selectOptions(playthroughStatuses)...).Value(&status),
			huh.NewInput().Title(title).Value(&platform),
			huh.NewInput().Title("Rating (1-10, blank if none)").Value(&rating).Validate(validateRating),
			huh.NewText().Title("Notes").Value(&notes),
		),
	).Run()
	return finished, status, platform, rating, notes, err
}

// ConfirmWrite shows the lines that will be written and asks for confirmation.
func ConfirmWrite(path string, newLines []string) (bool, error) {
	fmt.Printf("\nAbout to update %s:\n\n", path)
	for _, l := range newLines {
		fmt.Println("  " + l)
	}
	fmt.Println()
	var ok bool
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().Title("Write this change?").Value(&ok),
		),
	).Run()
	return ok, err
}
