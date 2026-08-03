package main

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
)

// ── Scan ─────────────────────────────────────────────────────────────────

type scanData struct {
	Page
	Report     string
	Candidates []Candidate
	MinHours   float64
}

// scanCandidates re-runs the two bulk RA/Steam endpoints scan.go's runScan
// calls. Both GET and POST need this (POST re-derives it to map submitted
// checkbox indices back to real candidates) — cheap and safe to call twice,
// per README: "two bulk endpoints ... fast, can't be rate-limited."
func (s *server) scanCandidates(minHours float64) ([]Candidate, int, error) {
	existing, err := ListGames(s.gamesDir)
	if err != nil {
		return nil, 0, err
	}
	creds := loadCredentials()
	if !creds.RAConfigured() && !creds.SteamConfigured() {
		return nil, len(existing), fmt.Errorf("no credentials configured; see tools/gamelog/README.md")
	}
	ctx := context.Background()
	index := newLoggedIndex(existing)
	var candidates []Candidate
	if creds.RAConfigured() {
		if found, err := scanRA(ctx, creds, index); err == nil {
			candidates = append(candidates, found...)
		}
	}
	if creds.SteamConfigured() {
		if found, err := scanSteam(ctx, creds, index, ScanOptions{MinHours: minHours}); err == nil {
			candidates = append(candidates, found...)
		}
	}
	sortCandidates(candidates)
	return candidates, len(existing), nil
}

func (s *server) handleScan(w http.ResponseWriter, r *http.Request) {
	minHours := float64(defaultMinHours)
	if v := r.URL.Query().Get("min_hours"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			minHours = f
		}
	}
	candidates, numExisting, err := s.scanCandidates(minHours)
	data := scanData{Page: newPage(r, "Scan", "scan"), Candidates: candidates, MinHours: minHours}
	if err != nil {
		data.Flash.Error = err.Error()
	} else {
		data.Report = formatScanReport(candidates, numExisting, ScanOptions{MinHours: minHours})
	}
	s.render(w, "scan", data)
}

func (s *server) handleScanCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectErr(w, r, "/scan", err)
		return
	}
	minHours := atoiFloatOr(r.FormValue("min_hours"), defaultMinHours)
	candidates, _, err := s.scanCandidates(minHours)
	if err != nil {
		redirectErr(w, r, "/scan", err)
		return
	}
	var chosen []int
	for _, v := range r.Form["candidate"] {
		if i, err := strconv.Atoi(v); err == nil && i >= 0 && i < len(candidates) {
			chosen = append(chosen, i)
		}
	}
	if len(chosen) == 0 {
		redirectOK(w, r, "/scan", "Nothing created.")
		return
	}
	created, skipped := createFromCandidates(s.gamesDir, candidates, chosen)
	redirectOK(w, r, "/scan", fmt.Sprintf("%d created, %d skipped. Review them, then flip draft off to publish.", len(created), skipped))
}

func atoiFloatOr(s string, def float64) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return def
	}
	return f
}

// ── Review ───────────────────────────────────────────────────────────────

type reviewRow struct {
	Game      GameSummary
	Card      string
	CanDelete bool
}

type reviewData struct {
	Page
	Games []reviewRow
}

func (s *server) handleReview(w http.ResponseWriter, r *http.Request) {
	games, err := ListGames(s.gamesDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	archiveDir := findArchiveDir(s.gamesDir)
	var rows []reviewRow
	for _, g := range games {
		if !g.Draft {
			continue
		}
		doc, err := LoadDoc(g.Path)
		if err != nil {
			continue
		}
		pf, err := LoadPlaythroughs(filepath.Dir(g.Path))
		if err != nil {
			pf = &PlaythroughsFile{}
		}
		canDelete := g.NumPlaythroughs == 0 && !hasArchiveRecord(archiveDir, g.ProviderLinks())
		rows = append(rows, reviewRow{Game: g, Card: formatReviewCard(g, doc.FM, pf), CanDelete: canDelete})
	}
	s.render(w, "review", reviewData{Page: newPage(r, "Review", "review"), Games: rows})
}

func (s *server) handleReviewAction(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	doc, pf, ok := s.gameOr404(w, r, slug)
	if !ok {
		return
	}
	action := r.FormValue("action")

	switch action {
	case reviewPublish, reviewDrop:
		applyReviewAction(doc, action)
		syncing := pf.SyncStatus(doc.FM.Status, doc.FM.Finished)
		if err := writeFrontMatter(doc, pf, syncing); err != nil {
			redirectErr(w, r, "/review", err)
			return
		}
		redirectOK(w, r, "/review", doc.FM.Title+": saved.")
	case reviewDelete:
		games, err := ListGames(s.gamesDir)
		if err != nil {
			redirectErr(w, r, "/review", err)
			return
		}
		var g GameSummary
		found := false
		for _, cand := range games {
			if cand.Slug == slug {
				g, found = cand, true
				break
			}
		}
		if !found {
			redirectErr(w, r, "/review", fmt.Errorf("no such game"))
			return
		}
		archiveDir := findArchiveDir(s.gamesDir)
		canDelete := g.NumPlaythroughs == 0 && !hasArchiveRecord(archiveDir, g.ProviderLinks())
		if err := deleteGameStub(s.gamesDir, g, canDelete); err != nil {
			redirectErr(w, r, "/review", err)
			return
		}
		redirectOK(w, r, "/review", "Deleted "+g.Title+".")
	default:
		redirectErr(w, r, "/review", fmt.Errorf("unknown action %q", action))
	}
}

// ── Stale ────────────────────────────────────────────────────────────────

type staleRow struct {
	C             StaleCandidate
	Card          string
	OtherStatuses []string
}

type staleData struct {
	Page
	Games []staleRow
	Days  int
}

func (s *server) handleStale(w http.ResponseWriter, r *http.Request) {
	days := atoiOr(r.URL.Query().Get("days"), defaultStaleDays)
	games, err := ListGames(s.gamesDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	candidates, err := findStaleCandidates(findArchiveDir(s.gamesDir), games, days)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rows := make([]staleRow, len(candidates))
	for i, c := range candidates {
		var others []string
		for _, st := range staleQuickStatuses {
			if st != c.SuggestedStatus {
				others = append(others, st)
			}
		}
		rows[i] = staleRow{C: c, Card: formatStaleCard(c), OtherStatuses: others}
	}
	s.render(w, "stale", staleData{Page: newPage(r, "Stale", "stale"), Games: rows, Days: days})
}

func (s *server) handleStaleAction(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	days := atoiOr(r.FormValue("days"), defaultStaleDays)
	backTo := fmt.Sprintf("/stale?days=%d", days)

	games, err := ListGames(s.gamesDir)
	if err != nil {
		redirectErr(w, r, backTo, err)
		return
	}
	candidates, err := findStaleCandidates(findArchiveDir(s.gamesDir), games, days)
	if err != nil {
		redirectErr(w, r, backTo, err)
		return
	}
	var c *StaleCandidate
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
	if action != staleAccept && !isStaleMarkAction(action) {
		redirectErr(w, r, backTo, fmt.Errorf("unknown action %q", action))
		return
	}
	applyStaleAction(doc, *c, action)
	syncing := pf.SyncStatus(doc.FM.Status, doc.FM.Finished)
	if err := writeFrontMatter(doc, pf, syncing); err != nil {
		redirectErr(w, r, backTo, err)
		return
	}
	redirectOK(w, r, backTo, doc.FM.Title+": saved.")
}

func isStaleMarkAction(action string) bool {
	return len(action) > len(staleMarkPrefix) && action[:len(staleMarkPrefix)] == staleMarkPrefix
}

// ── Close ────────────────────────────────────────────────────────────────

type closeData struct {
	Page
	Entries []OpenEntry
	Days    int
	All     bool
}

func (s *server) handleClose(w http.ResponseWriter, r *http.Request) {
	days := atoiOr(r.URL.Query().Get("days"), defaultStaleDays)
	all := r.URL.Query().Get("all") == "1"
	games, err := ListGames(s.gamesDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	open, err := findOpenEntries(findArchiveDir(s.gamesDir), s.gamesDir, games, days, all)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, "close", closeData{Page: newPage(r, "Close", "close"), Entries: open, Days: days, All: all})
}

func (s *server) handleCloseSelected(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectErr(w, r, "/close", err)
		return
	}
	days := atoiOr(r.FormValue("days"), defaultStaleDays)
	all := r.FormValue("all") == "1"
	backTo := fmt.Sprintf("/close?days=%d", days)
	if all {
		backTo += "&all=1"
	}

	games, err := ListGames(s.gamesDir)
	if err != nil {
		redirectErr(w, r, backTo, err)
		return
	}
	open, err := findOpenEntries(findArchiveDir(s.gamesDir), s.gamesDir, games, days, all)
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
		if did, err := closeEntry(open[i]); err == nil && did {
			closed++
		}
	}
	redirectOK(w, r, backTo, fmt.Sprintf("Closed %d of %d.", closed, len(open)))
}
