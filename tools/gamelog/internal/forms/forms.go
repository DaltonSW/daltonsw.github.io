package forms

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"

	"go.dalton.dog/gamelog/internal/model"
)

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

// "endless", "multiplayer", and "software" all mark an entry that
// doesn't have a meaningful start/finish narrative — it's used in an
// open-ended series of sessions with no state that ends play — for three
// different reasons: endless is a replay-loop design
// (roguelike/sandbox/idle), multiplayer is inherently social, and software
// isn't a game at all. Steam sells tools alongside games and reports playtime
// for them identically, so they arrive through the same scan; "software" says
// the completion vocabulary simply doesn't apply rather than forcing a
// finished/dropped answer to a question that was never asked.
//
// endless and multiplayer can also be set on an individual playthrough entry
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
var GameStatuses = []string{"backlog", "playing", "finished", "mastered", "dropped", "paused", "endless", "multiplayer", "software", "unplayed"}

// IsOneShot reports whether a status (game-level or entry-level) forbids a
// second playthrough entry of that same status on the same platform. Keep
// this in step with the statuses documented above.
func IsOneShot(status string) bool {
	return status == "endless" || status == "multiplayer" || status == "software"
}

// "misc_launch" covers an entry that isn't a real attempt at all — booted up
// just to check dates, or a launch that never got past a broken platform
// port (e.g. a Linux build that wouldn't run) — as distinct from "dropped",
// which implies play was actually attempted and abandoned. Unlike
// endless/multiplayer, a second misc_launch entry on the same platform isn't
// fragmentation: each launch is its own unrelated occasion, so it's not
// one-shot (see IsOneShot).
var PlaythroughStatuses = []string{"playing", "finished", "mastered", "dropped", "paused", "endless", "multiplayer", "misc_launch"}

// ParseSubgames splits a newline-separated "Subgames" text field into a
// roster, trimming each line and dropping blanks, so a game with no roster
// round-trips to nil rather than an empty-but-non-nil slice. Exported so the
// web server's game-info forms can parse the same textarea shape without
// duplicating this logic.
func ParseSubgames(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			out = append(out, t)
		}
	}
	return out
}

const StaleAccept = "accept"

// StaleMarkPrefix + a status (e.g. "mark_finished") is the action value for
// one "it's actually X" quick correction, applied by ApplyStaleAction.
const StaleMarkPrefix = "mark_"

// StaleQuickStatuses are offered as one-click corrections alongside
// whichever status `gamelog serve`'s Housekeeping page actually guessed.
// "paused" is never itself a guess (there's no signal for "meant to keep
// playing"), but it's always offered: distinguishing an intentional pause
// from "dropped" is exactly what a quiet game with no completion signal
// can't tell you on its own.
var StaleQuickStatuses = []string{"finished", "mastered", "dropped", "paused"}

// ScanQuickStatuses are the same idea for a scan candidate: the guess is
// offered as the primary button and the rest as "create as X instead", so a
// wrong guess costs one click rather than a create-then-edit round trip.
//
// "software" is in the set because Steam sells tools alongside games and
// reports playtime for them identically — the scan can't tell them apart, and
// the person reading the row can.
var ScanQuickStatuses = []string{"playing", "finished", "mastered", "dropped", "software"}

// BacklogQuickStatuses is the backlog half's set. Deliberately smaller:
// these are games under the playtime threshold, so any status claiming real
// history would be contradicted by the evidence that put them in this list.
// "unplayed" against "backlog" is the distinction worth one click —
// "haven't gotten to it" versus "don't know if I ever will".
var BacklogQuickStatuses = []string{"backlog", "unplayed", "software"}

// IsGameStatus reports whether s is a status a game may actually be created
// or set to. Used to check values arriving from a submitted form, which must
// never be written to front matter unvalidated.
func IsGameStatus(s string) bool {
	return slices.Contains(GameStatuses, s)
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
