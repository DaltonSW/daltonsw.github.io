package model

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

// IsHandSetStatus reports whether a game status is never derived.
func IsHandSetStatus(status string) bool {
	return status == "endless" || status == "multiplayer" || status == "software"
}

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
	if IsHandSetStatus(current) {
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
