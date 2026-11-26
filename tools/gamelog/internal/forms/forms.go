package forms

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

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
// IsOneShot and mutate.OneShotConflict — since none of the three has a save file or
// finish line, a second entry of the *same* one-shot status on the same
// platform would be fragmentation, not a second mode.
//
// "unplayed" is deliberately not here: the site derives it for any game with
// no playthrough entries and no captured playtime (layouts/partials/
// game-unplayed.html), so it sits alongside a status rather than replacing one.
// "mastered" is not here either, for the same reason: it's derived from the
// achievement archive (model.DeriveMastery → achievement-summary.yaml's
// `mastered:`), so a mastered game can still be "playing" a replay.
// "unfinished" sits between "dropped" and "paused": play has actually
// stopped, same as dropped, but it doesn't claim to know whether that's
// permanent. "dropped" asserts you won't come back; "paused" asserts you
// plan to. "unfinished" is for the common case in between — play just
// trailed off with no stated intent either way. It closes like "dropped"
// (gets a finished/closed date, counts as a done status), it just doesn't
// share dropped's certainty.
var GameStatuses = []string{"backlog", "playing", "finished", "dropped", "unfinished", "paused", "endless", "multiplayer", "software"}

// IsOneShot reports whether a status (game-level or entry-level) forbids a
// second playthrough entry of that same status on the same platform. Keep
// this in step with the statuses documented above.
func IsOneShot(status string) bool {
	return status == "endless" || status == "multiplayer" || status == "software"
}

// DefaultPlaythroughStatus is the status a new playthrough starts with when
// nothing says otherwise: "playing", unless the game as a whole is one of the
// modes that has no finish line.
func DefaultPlaythroughStatus(gameStatus string) string {
	if gameStatus == "endless" || gameStatus == "multiplayer" {
		return gameStatus
	}
	return "playing"
}

// "misc_launch" covers an entry that isn't a real attempt at all — booted up
// just to check dates, or a launch that never got past a broken platform
// port (e.g. a Linux build that wouldn't run) — as distinct from "dropped",
// which implies play was actually attempted and abandoned. Unlike
// endless/multiplayer, a second misc_launch entry on the same platform isn't
// fragmentation: each launch is its own unrelated occasion, so it's not
// one-shot (see IsOneShot).
var PlaythroughStatuses = []string{"playing", "finished", "dropped", "unfinished", "paused", "endless", "multiplayer", "misc_launch"}

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
// whichever status Catch up's quiet-games list actually guessed.
// "paused" and "unfinished" are never themselves a guess (there's no signal
// for "meant to keep playing" or "no idea"), but they're always offered:
// distinguishing an intentional pause, or a genuinely unknown "it just
// trailed off", from "dropped" is exactly what a quiet game with no
// completion signal can't tell you on its own.
var StaleQuickStatuses = []string{"finished", "dropped", "unfinished", "paused"}

// ScanQuickStatuses are the same idea for a scan candidate: the guess is
// offered as the primary button and the rest as "create as X instead", so a
// wrong guess costs one click rather than a create-then-edit round trip.
//
// "software" is in the set because Steam sells tools alongside games and
// reports playtime for them identically — the scan can't tell them apart, and
// the person reading the row can. "endless" and "multiplayer" are here for
// the same reason: a scan candidate's evidence is just playtime/achievements,
// which looks identical whether the game has a finish line or not — that
// call is the person reading the row's to make.
var ScanQuickStatuses = []string{"playing", "finished", "dropped", "unfinished", "software", "endless", "multiplayer"}

// BacklogQuickStatuses is the backlog half's set. Deliberately smaller:
// these are games under the playtime threshold, so any status claiming real
// history would be contradicted by the evidence that put them in this list.
var BacklogQuickStatuses = []string{"backlog", "software"}

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
