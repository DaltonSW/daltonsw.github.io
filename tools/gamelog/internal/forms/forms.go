package forms

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"

	"go.dalton.dog/gamelog/internal/externalid"
	"go.dalton.dog/gamelog/internal/model"
)

const NewGameSentinel = "__new__"

func ValidateDate(required bool) func(string) error {
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

func ValidateRating(s string) error {
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
func Today() string {
	return time.Now().In(model.SiteLocation).Format("2006-01-02")
}

// SelectGame prompts for an existing game, or the sentinel "create a new
// game" option. Returns slug == NewGameSentinel when the latter is chosen.
func SelectGame(games []model.GameSummary) (string, error) {
	options := []huh.Option[string]{huh.NewOption("+ New game", NewGameSentinel)}
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
func SelectExistingGame(games []model.GameSummary) (string, error) {
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

// "ongoing", "multiplayer", and "software" all mark an entry that
// doesn't have a meaningful start/finish narrative — it's used in an
// open-ended series of sessions with no state that ends play — for three
// different reasons: ongoing is a replay-loop design
// (roguelike/sandbox/idle), multiplayer is inherently social, and software
// isn't a game at all. Steam sells tools alongside games and reports playtime
// for them identically, so they arrive through the same scan; "software" says
// the completion vocabulary simply doesn't apply rather than forcing a
// finished/dropped answer to a question that was never asked.
//
// ongoing and multiplayer can also be set on an individual playthrough entry
// (see PlaythroughStatuses) — a game with distinct modes (Hitman's story
// campaign, its Freelancer roguelike, and its multiplayer contracts) is one
// game with several differently-shaped entries, not one status forced onto
// all of them. "software" stays game-level only: it describes the whole
// record, not a mode a game can also have alongside others.
//
// Each one-shot status still gets at most one entry per platform — see
// IsOneShot and oneShotConflict — since none of the three has a save file or
// finish line, a second entry of the *same* one-shot status on the same
// platform would be fragmentation, not a second mode.
//
// "unplayed" is game-level only: it's for a game that was launched or
// touched somehow (booting a Switch title solely for a cross-save/gift
// unlock, say) but never actually played, and — unlike "backlog" — makes no
// claim about intending to play it eventually. "backlog" says "haven't
// gotten to it yet"; "unplayed" says "don't know if I ever will."
var GameStatuses = []string{"backlog", "playing", "finished", "mastered", "dropped", "paused", "ongoing", "multiplayer", "software", "unplayed"}

// IsOneShot reports whether a status (game-level or entry-level) forbids a
// second playthrough entry of that same status on the same platform. Keep
// this in step with the statuses documented above.
func IsOneShot(status string) bool {
	return status == "ongoing" || status == "multiplayer" || status == "software"
}

// "misc_launch" covers an entry that isn't a real attempt at all — booted up
// just to check dates, or a launch that never got past a broken platform
// port (e.g. a Linux build that wouldn't run) — as distinct from "dropped",
// which implies play was actually attempted and abandoned. Unlike
// ongoing/multiplayer, a second misc_launch entry on the same platform isn't
// fragmentation: each launch is its own unrelated occasion, so it's not
// one-shot (see IsOneShot).
var PlaythroughStatuses = []string{"playing", "finished", "mastered", "dropped", "paused", "ongoing", "multiplayer", "misc_launch"}

func selectOptions(values []string) []huh.Option[string] {
	opts := make([]huh.Option[string], len(values))
	for i, v := range values {
		opts[i] = huh.NewOption(v, v)
	}
	return opts
}

// NewGameForm prompts for all fields of a brand-new game entry.
func NewGameForm(defaultTitle string) (model.NewGameFields, error) {
	f := model.NewGameFields{
		Title:   defaultTitle,
		Status:  "playing",
		Started: Today(),
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
				Value(&f.RetroAchievementsID).Validate(externalid.ValidateExternalID),
			huh.NewInput().Title("Steam appid or store URL (blank if none)").
				Value(&f.SteamAppID).Validate(externalid.ValidateExternalID),
			huh.NewSelect[string]().Title("Status").Options(selectOptions(GameStatuses)...).Value(&f.Status),
			huh.NewInput().Title("Started (YYYY-MM-DD)").Value(&f.Started).Validate(ValidateDate(false)),
			huh.NewInput().Title("Finished (YYYY-MM-DD, blank if ongoing)").Value(&f.Finished).Validate(ValidateDate(false)),
			huh.NewInput().Title("Rating (1-10, blank if none)").Value(&f.Rating).Validate(ValidateRating),
			huh.NewText().Title("Overview").Value(&f.Overview),
		),
	).Run()
	if err != nil {
		return f, err
	}
	// Validation above only proves the input is parseable; store the bare ID
	// so front matter never holds a URL.
	if f.RetroAchievementsID, err = externalid.ParseExternalID(f.RetroAchievementsID); err != nil {
		return f, err
	}
	if f.SteamAppID, err = externalid.ParseExternalID(f.SteamAppID); err != nil {
		return f, err
	}
	return f, nil
}

// EditGameForm prompts to edit a game's front-matter fields, prefilled with
// current values. RetroAchievementsID/SteamAppID and the markdown body
// aren't editable here — relinking a provider deserves its own flow.
func EditGameForm(fm model.FrontMatter) (model.FrontMatter, error) {
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
			huh.NewSelect[string]().Title("Status").Options(selectOptions(GameStatuses)...).Value(&f.Status),
			huh.NewInput().Title("Started (YYYY-MM-DD)").Value(&f.Started).Validate(ValidateDate(false)),
			huh.NewInput().Title("Finished (YYYY-MM-DD, blank if ongoing)").Value(&f.Finished).Validate(ValidateDate(false)),
			huh.NewInput().Title("Rating (1-10, blank if none)").Value(&rating).Validate(ValidateRating),
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
	ReviewPublish     = "publish"
	ReviewEditPublish = "edit_publish"
	ReviewEditOnly    = "edit_only"
	ReviewDrop        = "drop"
	ReviewSkip        = "skip"
	ReviewDelete      = "delete"
	ReviewQuit        = "quit"
)

// SelectReviewAction prompts for what to do with one draft game during
// `gamelog review`. Delete is only offered when canDelete (no captured
// playthroughs or archive history to risk).
func SelectReviewAction(canDelete bool) (string, error) {
	options := []huh.Option[string]{
		huh.NewOption("Publish as-is", ReviewPublish),
		huh.NewOption("Edit, then publish", ReviewEditPublish),
		huh.NewOption("Edit without publishing", ReviewEditOnly),
		huh.NewOption("Mark dropped & publish", ReviewDrop),
		huh.NewOption("Skip (leave draft, show again next run)", ReviewSkip),
	}
	if canDelete {
		options = append(options, huh.NewOption("Delete this stub", ReviewDelete))
	}
	options = append(options, huh.NewOption("Quit review", ReviewQuit))

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
	StaleAccept = "accept"
	StaleEdit   = "edit"
	StaleSkip   = "skip"
	StaleQuit   = "quit"
)

// StaleMarkAction is the action value for one "it's actually X" quick
// correction — StaleMarkPrefix + a status, e.g. "mark_finished".
const StaleMarkPrefix = "mark_"

func StaleMarkAction(status string) string { return StaleMarkPrefix + status }

// StaleQuickStatuses are offered as one-click corrections alongside
// whichever status `stale` actually guessed — see SelectStaleAction.
// "paused" is never itself a guess (there's no signal for "meant to keep
// playing"), but it's always offered: distinguishing an intentional pause
// from "dropped" is exactly what a quiet game with no completion signal
// can't tell you on its own.
var StaleQuickStatuses = []string{"finished", "mastered", "dropped", "paused"}

// SelectStaleAction prompts for what to do with one stale-game suggestion
// during `gamelog stale`. Nothing here writes on its own — every path either
// discards or hands off to the same confirm/loss-check write every other
// front-matter edit goes through. Alongside the guess, every other status
// `stale` might have guessed is offered as a one-click correction — no need
// to open the full edit form just to say "it was actually finished."
func SelectStaleAction(suggested string) (string, error) {
	options := []huh.Option[string]{
		huh.NewOption(fmt.Sprintf("Correct — mark %s", suggested), StaleAccept),
	}
	for _, status := range StaleQuickStatuses {
		if status == suggested {
			continue
		}
		options = append(options, huh.NewOption(fmt.Sprintf("Nope — mark %s instead", status), StaleMarkAction(status)))
	}
	options = append(options,
		huh.NewOption("Edit before applying", StaleEdit),
		huh.NewOption("Still playing / skip for now", StaleSkip),
		huh.NewOption("Quit", StaleQuit),
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
//
// defaultStatus prefills the status select — doNewPlaythrough passes the
// game's own front-matter status when that's a one-shot status (ongoing/
// multiplayer/software), so the common case of a single-mode game's one
// entry stays a one-keystroke confirm, same as before this field existed.
func PlaythroughForm(gamePlatform, defaultStatus string) (model.PlaythroughFields, error) {
	f := model.PlaythroughFields{
		Status:  defaultStatus,
		Started: Today(),
	}
	title := "Platform (blank = same as game)"
	if gamePlatform != "" {
		title = fmt.Sprintf("Platform (blank = %s)", gamePlatform)
	}
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Started (YYYY-MM-DD)").Value(&f.Started).Validate(ValidateDate(true)),
			huh.NewSelect[string]().Title("Status").Options(selectOptions(PlaythroughStatuses)...).Value(&f.Status),
			huh.NewInput().Title("Finished (YYYY-MM-DD, blank if ongoing)").Value(&f.Finished).Validate(ValidateDate(false)),
			huh.NewInput().Title(title).Value(&f.Platform),
			huh.NewInput().Title("Rating (1-10, blank if none)").Value(&f.Rating).Validate(ValidateRating),
			huh.NewText().Title("Notes").Value(&f.Notes),
		),
	).Run()
	return f, err
}

// SelectPlaythrough prompts to pick one of a game's playthroughs entries.
func SelectPlaythrough(playthroughs []model.Playthrough) (int, error) {
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
			label += fmt.Sprintf(" (%d sessions, last %s→%s)", len(p.Sessions), last.Started, OrDash(last.Finished))
		} else if p.Started == "" {
			// OrDash's "ongoing" already means something else in this
			// codebase (in progress, not finished) — a blank Started with no
			// sessions is a planned placeholder that hasn't begun at all.
			label += " (not started)"
		} else {
			label += fmt.Sprintf(" (%s→%s)", p.Started, OrDash(p.Finished))
		}
		options[i] = huh.NewOption(label, p.Index)
	}
	var idx int
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[int]().Title("Which playthrough?").Options(options...).Value(&idx),
		),
	).Run()
	return idx, err
}

// SelectSession prompts to pick one session within an already-chosen
// playthrough — framed as "last session to keep in the original," the same
// picker split reuses as its split point.
func SelectSession(sessions []model.Session) (int, error) {
	options := make([]huh.Option[int], len(sessions))
	for i, s := range sessions {
		label := fmt.Sprintf("#%d %s→%s", i+1, s.Started, OrDash(s.Finished))
		if s.Title != "" {
			label += "  " + s.Title
		}
		options[i] = huh.NewOption(label, s.Index)
	}
	var idx int
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[int]().Title("Which session?").Options(options...).Value(&idx),
		),
	).Run()
	return idx, err
}

const (
	SessionActionEdit   = "session_edit"
	SessionActionDelete = "session_delete"
	SessionActionSplit  = "session_split"
)

// SelectSessionAction prompts for what to do with the selected session.
// canDelete gates on RemoveSession's minimum-session rule; canSplit gates on
// there being a later session to move into a new entry.
func SelectSessionAction(canDelete, canSplit bool) (string, error) {
	options := []huh.Option[string]{huh.NewOption("Edit dates/title", SessionActionEdit)}
	if canDelete {
		options = append(options, huh.NewOption("Delete this session", SessionActionDelete))
	}
	if canSplit {
		options = append(options, huh.NewOption("Split everything after this into a new playthrough", SessionActionSplit))
	}
	var action string
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().Title("What next?").Options(options...).Value(&action),
		),
	).Run()
	return action, err
}

// EditSessionForm prompts to edit one session's dates/title, prefilled with
// its current values.
func EditSessionForm(s model.Session) (started, finished, title string, err error) {
	started, finished, title = s.Started, s.Finished, s.Title
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Session started (YYYY-MM-DD)").Value(&started).Validate(ValidateDate(true)),
			huh.NewInput().Title("Session finished (YYYY-MM-DD, blank if still ongoing)").Value(&finished).Validate(ValidateDate(false)),
			huh.NewInput().Title("Session title (optional)").Value(&title),
		),
	).Run()
	return started, finished, title, err
}

// SplitStatusForm prompts for the new split-off entry's status, defaulting
// to the source entry's current status but editable.
func SplitStatusForm(defaultStatus string) (string, error) {
	status := defaultStatus
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().Title("Status for the new playthrough").Options(selectOptions(PlaythroughStatuses)...).Value(&status),
		),
	).Run()
	return status, err
}

func OrDash(s string) string {
	if s == "" {
		return "ongoing"
	}
	return s
}

// OrNone is OrDash's counterpart for fields where a blank value doesn't mean
// "ongoing" — a start date, a status, a platform. OrDash's "ongoing" only
// reads correctly next to a *finish* date (blank there genuinely means still
// being played); reusing it for these would print nonsense like
// "started: ongoing" or "platform: ongoing".
func OrNone(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// SessionForm prompts for a new session's date range and an optional title,
// for a session worth telling apart from the others on the playthrough bar
// (a DLC release, "wrapping up achievements", and so on). defaultStarted and
// defaultFinished prefill the two date fields — the manual "log a session"
// flow defaults to today/ongoing, while a nudge triggered by a provider
// refresh prefills both with the activity date that triggered it, since
// that's all a playtime/last-unlock signal can tell you.
func SessionForm(defaultStarted, defaultFinished string) (started, finished, title string, err error) {
	started, finished = defaultStarted, defaultFinished
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Session started (YYYY-MM-DD)").Value(&started).Validate(ValidateDate(true)),
			huh.NewInput().Title("Session finished (YYYY-MM-DD, blank if still ongoing)").Value(&finished).Validate(ValidateDate(false)),
			huh.NewInput().Title("Session title (optional)").Value(&title),
		),
	).Run()
	return started, finished, title, err
}

// PlannedReplayForm prompts for the two fields a planned-replay placeholder
// carries: which platform (blank = same as the game) and an optional note
// about what's planned (a mode to try, DLC to catch up on). No dates, no
// status select — the entry this feeds stays status: planned until it's
// graduated into a real playthrough. platform/notes are also the prefill, so
// the same form serves both creating a placeholder and editing one in place.
func PlannedReplayForm(gamePlatform, platform, notes string) (string, string, error) {
	title := "Platform (blank = same as game)"
	if gamePlatform != "" {
		title = fmt.Sprintf("Platform (blank = %s)", gamePlatform)
	}
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title(title).Value(&platform),
			huh.NewText().Title("Notes (optional)").Value(&notes),
		),
	).Run()
	return platform, notes, err
}

const (
	PlannedActionStart = "planned_start"
	PlannedActionEdit  = "planned_edit"
)

// SelectPlannedAction prompts for what to do with a chosen planned-replay
// placeholder. There is deliberately no "remove" option here — see the
// no-delete-mutator note on PlaythroughsFile.GraduatePlannedPlaythrough.
func SelectPlannedAction() (string, error) {
	var action string
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().Title("What next?").Options(
				huh.NewOption("Start this playthrough now", PlannedActionStart),
				huh.NewOption("Edit platform/notes", PlannedActionEdit),
			).Value(&action),
		),
	).Run()
	return action, err
}

// UpdateForm prompts to edit an existing playthrough's finished date,
// status, platform, rating, and notes, prefilled with its current values.
//
// gamePlatform only labels the blank case, the same way PlaythroughForm uses
// it — it is never written into the entry, so clearing this field restores
// inheritance rather than silently freezing today's game platform in place.
func UpdateForm(p model.Playthrough, gamePlatform string) (finished, status, platform, rating, notes string, err error) {
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
			huh.NewInput().Title("Finished (YYYY-MM-DD, blank if ongoing)").Value(&finished).Validate(ValidateDate(false)),
			huh.NewSelect[string]().Title("Status").Options(selectOptions(PlaythroughStatuses)...).Value(&status),
			huh.NewInput().Title(title).Value(&platform),
			huh.NewInput().Title("Rating (1-10, blank if none)").Value(&rating).Validate(ValidateRating),
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
