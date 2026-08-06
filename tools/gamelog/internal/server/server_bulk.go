package server

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"go.dalton.dog/gamelog/internal/commands"
	"go.dalton.dog/gamelog/internal/forms"
	"go.dalton.dog/gamelog/internal/model"
	"go.dalton.dog/gamelog/internal/mutate"
)

// ── Scan (lives on the Housekeeping page — see below) ─────────────────────

// scanCandidates re-runs the two bulk RA/Steam endpoints scan.go's runScan
// calls. Both GET and POST need this (POST re-derives it to map submitted
// checkbox indices back to real candidates) — cheap and safe to call twice,
// per README: "two bulk endpoints ... fast, can't be rate-limited."
//
// mode picks which half of the Steam library is wanted, and the backlog half
// skips RetroAchievements entirely: RA has no notion of ownership, and
// GetUserCompletionProgress only returns games with at least one achievement
// already earned, so nothing it reports could belong in a backlog list.
func (s *server) scanCandidates(minHours float64, mode commands.ScanMode) ([]commands.Candidate, int, error) {
	existing, err := model.ListGames(s.gamesDir)
	if err != nil {
		return nil, 0, err
	}
	creds := commands.LoadCredentials()
	backlog := mode == commands.ScanModeBacklog
	if backlog {
		if !creds.SteamConfigured() {
			return nil, len(existing), fmt.Errorf("Steam credentials not configured; see tools/gamelog/README.md")
		}
	} else if !creds.RAConfigured() && !creds.SteamConfigured() {
		return nil, len(existing), fmt.Errorf("no credentials configured; see tools/gamelog/README.md")
	}
	ctx := context.Background()
	index := commands.NewLoggedIndex(existing)
	var candidates []commands.Candidate
	if creds.RAConfigured() && !backlog {
		if found, err := commands.ScanRA(ctx, creds, index); err == nil {
			candidates = append(candidates, found...)
		}
	}
	if creds.SteamConfigured() {
		opts := commands.ScanOptions{MinHours: minHours, Mode: mode}
		if found, err := commands.ScanSteam(ctx, creds, index, opts); err == nil {
			candidates = append(candidates, found...)
		}
	}
	commands.SortCandidates(candidates)
	return candidates, len(existing), nil
}

func (s *server) handleScanCreate(w http.ResponseWriter, r *http.Request) {
	s.createFromScan(w, r, commands.ScanModePlayed)
}

func (s *server) handleBacklogCreate(w http.ResponseWriter, r *http.Request) {
	s.createFromScan(w, r, commands.ScanModeBacklog)
}

// createFromScan backs both create buttons. The submitted checkbox values are
// indices into a list the browser never held, so the scan has to be re-derived
// with the same min_hours *and* the same mode — a mismatch on either would map
// the indices onto different games.
func (s *server) createFromScan(w http.ResponseWriter, r *http.Request, mode commands.ScanMode) {
	if err := r.ParseForm(); err != nil {
		redirectErr(w, r, "/housekeeping", err)
		return
	}
	minHours := atoiFloatOr(r.FormValue("min_hours"), commands.DefaultMinHours)
	candidates, _, err := s.scanCandidates(minHours, mode)
	if err != nil {
		redirectErr(w, r, "/housekeeping", err)
		return
	}
	var chosen []int
	for _, v := range r.Form["candidate"] {
		if i, err := strconv.Atoi(v); err == nil && i >= 0 && i < len(candidates) {
			chosen = append(chosen, i)
		}
	}
	if len(chosen) == 0 {
		redirectOK(w, r, "/housekeeping", "Nothing created.")
		return
	}
	created, skipped := commands.CreateFromCandidates(s.gamesDir, candidates, chosen)
	kind := "created"
	if mode == commands.ScanModeBacklog {
		kind = "created as backlog"
	}
	redirectOK(w, r, "/housekeeping", fmt.Sprintf("%d %s, %d skipped. Find them via the games list's \"drafts only\" filter to finish them.", len(created), kind, skipped))
}

func atoiFloatOr(s string, def float64) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return def
	}
	return f
}

// ── Housekeeping (scan + stale + close + achievements) ────────────────────
//
// All periodic whole-library maintenance, sharing one page: scan finds
// un-logged games, stale/close both clean up playthroughs that went quiet
// (status vs. finish date — see README), achievements refreshes archived
// data. Draft review itself isn't here — the games list's own "drafts only"
// filter and Draft? column cover that now. The scan/stale/close sections use
// disjoint query-param prefixes (min_hours is already scan-only; stale_*/
// close_* for the other two) since stale and close would otherwise collide
// on "days" now that they share a page.

type staleRow struct {
	C             commands.StaleCandidate
	Card          string
	OtherStatuses []string
}

type housekeepingData struct {
	Page
	ScanReport     string
	ScanCandidates []commands.Candidate
	ScanError      string
	MinHours       float64
	// Backlog* is the same scan against the other side of MinHours. It
	// shares min_hours on purpose — one boundary, two complementary lists,
	// so nothing can appear in both.
	BacklogReport     string
	BacklogCandidates []commands.Candidate
	BacklogError      string
	Unfinished        []commands.UnfinishedCandidate
	UnfinishedPct     float64
	UnfinishedAll     bool
	StaleGames        []staleRow
	StaleDays         int
	CloseEntries      []commands.OpenEntry
	CloseDays         int
	CloseAll          bool
}

func (s *server) handleHousekeeping(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	minHours := float64(commands.DefaultMinHours)
	if v := q.Get("min_hours"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			minHours = f
		}
	}
	staleDays := atoiOr(q.Get("stale_days"), commands.DefaultStaleDays)
	closeDays := atoiOr(q.Get("close_days"), commands.DefaultStaleDays)
	closeAll := q.Get("close_all") == "1"
	unfinishedPct := float64(commands.DefaultUnfinishedPct)
	if v := q.Get("unfinished_pct"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			unfinishedPct = f
		}
	}
	unfinishedAll := q.Get("unfinished_all") == "1"

	data := housekeepingData{
		Page:          newPage(r, "Housekeeping", "housekeeping"),
		MinHours:      minHours,
		UnfinishedPct: unfinishedPct,
		UnfinishedAll: unfinishedAll,
		StaleDays:     staleDays,
		CloseDays:     closeDays,
		CloseAll:      closeAll,
	}

	candidates, numExisting, err := s.scanCandidates(minHours, commands.ScanModePlayed)
	if err != nil {
		// Kept separate from Page.Flash, which is reserved for the
		// redirect-landing confirmation from a stale/close/scan-create
		// action — scan's "no credentials configured" would otherwise
		// silently clobber that on every load.
		data.ScanError = err.Error()
	} else {
		data.ScanCandidates = candidates
		data.ScanReport = commands.FormatScanReport(candidates, numExisting, commands.ScanOptions{MinHours: minHours})
	}

	backlogOpts := commands.ScanOptions{MinHours: minHours, Mode: commands.ScanModeBacklog}
	backlog, numExisting, err := s.scanCandidates(minHours, commands.ScanModeBacklog)
	if err != nil {
		data.BacklogError = err.Error()
	} else {
		data.BacklogCandidates = backlog
		data.BacklogReport = commands.FormatBacklogReport(backlog, numExisting, backlogOpts)
	}

	games, err := model.ListGames(s.gamesDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Reads the archive on disk only — no provider calls, so this section
	// renders even with no credentials configured at all.
	unfinished, err := commands.FindUnfinished(model.FindArchiveDir(s.gamesDir), games, commands.UnfinishedOptions{
		MinPct:      unfinishedPct,
		IncludeDone: unfinishedAll,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data.Unfinished = unfinished

	staleCandidates, err := commands.FindStaleCandidates(model.FindArchiveDir(s.gamesDir), games, staleDays)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	staleRows := make([]staleRow, len(staleCandidates))
	for i, c := range staleCandidates {
		var others []string
		for _, st := range forms.StaleQuickStatuses {
			if st != c.SuggestedStatus {
				others = append(others, st)
			}
		}
		staleRows[i] = staleRow{C: c, Card: commands.FormatStaleCard(c), OtherStatuses: others}
	}
	data.StaleGames = staleRows

	open, err := commands.FindOpenEntries(model.FindArchiveDir(s.gamesDir), s.gamesDir, games, closeDays, closeAll)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data.CloseEntries = open

	s.render(w, "housekeeping", data)
}

func (s *server) handleStaleAction(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	days := atoiOr(r.FormValue("stale_days"), commands.DefaultStaleDays)
	backTo := fmt.Sprintf("/housekeeping?stale_days=%d", days)

	games, err := model.ListGames(s.gamesDir)
	if err != nil {
		redirectErr(w, r, backTo, err)
		return
	}
	candidates, err := commands.FindStaleCandidates(model.FindArchiveDir(s.gamesDir), games, days)
	if err != nil {
		redirectErr(w, r, backTo, err)
		return
	}
	var c *commands.StaleCandidate
	for i := range candidates {
		if candidates[i].Game.Slug == slug {
			c = &candidates[i]
			break
		}
	}
	if c == nil {
		redirectErr(w, r, backTo, fmt.Errorf("%s is no longer a stale candidate", slug))
		return
	}

	doc, pf, ok := s.gameOr404(w, r, slug)
	if !ok {
		return
	}
	action := r.FormValue("action")
	if action != forms.StaleAccept && !isStaleMarkAction(action) {
		redirectErr(w, r, backTo, fmt.Errorf("unknown action %q", action))
		return
	}
	commands.ApplyStaleAction(doc, *c, action)
	syncing := pf.SyncStatus(doc.FM.Status, doc.FM.Finished)
	if err := mutate.WriteFrontMatter(doc, pf, syncing); err != nil {
		redirectErr(w, r, backTo, err)
		return
	}
	redirectOK(w, r, backTo, doc.FM.Title+": saved.")
}

func isStaleMarkAction(action string) bool {
	return len(action) > len(forms.StaleMarkPrefix) && action[:len(forms.StaleMarkPrefix)] == forms.StaleMarkPrefix
}

func (s *server) handleCloseSelected(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectErr(w, r, "/housekeeping", err)
		return
	}
	days := atoiOr(r.FormValue("close_days"), commands.DefaultStaleDays)
	all := r.FormValue("close_all") == "1"
	backTo := fmt.Sprintf("/housekeeping?close_days=%d", days)
	if all {
		backTo += "&close_all=1"
	}

	games, err := model.ListGames(s.gamesDir)
	if err != nil {
		redirectErr(w, r, backTo, err)
		return
	}
	open, err := commands.FindOpenEntries(model.FindArchiveDir(s.gamesDir), s.gamesDir, games, days, all)
	if err != nil {
		redirectErr(w, r, backTo, err)
		return
	}

	closed := 0
	for _, v := range r.Form["entry"] {
		i, err := strconv.Atoi(v)
		if err != nil || i < 0 || i >= len(open) {
			continue
		}
		if did, err := commands.CloseEntry(open[i]); err == nil && did {
			closed++
		}
	}
	redirectOK(w, r, backTo, fmt.Sprintf("Closed %d of %d.", closed, len(open)))
}
