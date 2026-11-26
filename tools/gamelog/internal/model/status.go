package model

import "slices"

// A game's status is derived from its playthroughs whenever it can be, the
// same way its dates and mastery are. Two kinds of status stay hand-set:
//
//   - the one-shot statuses (endless/multiplayer/software): they describe the
//     kind of game, not how a run went, so Hades stays "endless" even though
//     its run is "finished";
//   - any game with no runs that count: backlog, or a game logged before runs
//     existed.
//
// The tool writes the derived value back into front matter (mutate.
// SyncGameStatus, and Housekeeping's Regenerate summaries for every game) so the site's status
// taxonomy, counts and filters keep reading .Params.status unchanged.

// countsForStatus reports whether a run takes part in deriving the game's
// status. planned hasn't happened; misc_launch wasn't a real attempt; an
// endless/multiplayer run on a game with a finish line is a side mode (and
// deriving *into* a one-shot status would make the game hand-set for good).
func countsForStatus(p Playthrough) bool {
	switch p.Status {
	case "planned", "misc_launch", "endless", "multiplayer":
		return false
	}
	return true
}

// DeriveGameStatus returns the status a game's runs imply, and false when the
// status stays hand-set (see the package comment above):
//
//   - any open run: the status of the most recently active one, so a replay
//     of a finished game reads "playing" and an on-hold run "paused";
//   - otherwise "finished" if any run was finished — a later dropped attempt
//     doesn't un-finish a game — else the status of the run that ended last.
func DeriveGameStatus(current string, views []Playthrough) (string, bool) {
	if IsOneShot(current) {
		return "", false
	}
	var latestOpen, latestClosed *Playthrough
	finished := false
	for i := range views {
		p := &views[i]
		if !countsForStatus(*p) {
			continue
		}
		if p.IsOpen() {
			if latestOpen == nil || p.LatestDate() > latestOpen.LatestDate() {
				latestOpen = p
			}
			continue
		}
		if p.Status == "finished" {
			finished = true
		}
		if latestClosed == nil || p.FinishDate() > latestClosed.FinishDate() {
			latestClosed = p
		}
	}
	switch {
	case latestOpen != nil:
		return latestOpen.Status, latestOpen.Status != ""
	case finished:
		return "finished", true
	case latestClosed != nil:
		return latestClosed.Status, latestClosed.Status != ""
	}
	return "", false
}

// CloseLatestOpenRun applies a triage verdict to the game's most recently
// active open run: it takes status, and is closed on closedOn unless that's
// blank (as "paused" always passes). Reports whether there was such a run.
// The game's own status then follows via DeriveGameStatus.
func (f *PlaythroughsFile) CloseLatestOpenRun(status, closedOn string) bool {
	idx := -1
	views := f.Views()
	for i, p := range views {
		if !countsForStatus(p) || !p.IsOpen() {
			continue
		}
		if idx < 0 || p.LatestDate() > views[idx].LatestDate() {
			idx = i
		}
	}
	if idx < 0 {
		return false
	}
	e := &f.Playthroughs[idx]
	e.Status = status
	if closedOn != "" {
		e.Sessions[e.LatestSessionIndex()].Finished = closedOn
	}
	return true
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
// There's no cap on entries of a one-shot status: a second multiplayer entry
// on the same platform (another group, another season) is a real record.
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

// IsOneShot reports whether a status is one of the one-shot statuses
// documented above. On a game it means the status is hand-set, never derived
// from runs (see DeriveGameStatus). It doesn't limit how many entries of
// that status a game can have.
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
// which implies play was actually attempted and abandoned. Each launch is
// its own unrelated occasion, so it repeats freely; it isn't one-shot (see
// IsOneShot).
var PlaythroughStatuses = []string{"playing", "finished", "dropped", "unfinished", "paused", "endless", "multiplayer", "misc_launch"}

// IsGameStatus reports whether s is a status a game may actually be created
// or set to. Used to check values arriving from a submitted form, which must
// never be written to front matter unvalidated.
func IsGameStatus(s string) bool {
	return slices.Contains(GameStatuses, s)
}

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
