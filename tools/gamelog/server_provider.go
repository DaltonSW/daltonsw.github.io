package main

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
)

// ── Suggest (read-only) ──────────────────────────────────────────────────

type suggestPickerData struct {
	Page
	Games []GameSummary
}

func (s *server) handleSuggestPicker(w http.ResponseWriter, r *http.Request) {
	games, err := ListGames(s.gamesDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, "suggest_picker", suggestPickerData{Page: newPage(r, "Suggest", "suggest"), Games: games})
}

type suggestReportData struct {
	Page
	Title  string
	Slug   string
	Report string
}

func (s *server) handleSuggestReport(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	games, err := ListGames(s.gamesDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var summary GameSummary
	found := false
	for _, g := range games {
		if g.Slug == slug {
			summary, found = g, true
			break
		}
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	doc, err := LoadDoc(summary.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	raID, steamAppID := doc.ExternalIDs()
	if id, err := parseExternalID(raID); err == nil {
		raID = id
	}
	if id, err := parseExternalID(steamAppID); err == nil {
		steamAppID = id
	}
	creds := loadCredentials()

	report := SuggestionReport{
		Title: summary.Title, Slug: slug, NumPlaythroughs: summary.NumPlaythroughs,
		RAGameID: raID, RAUsername: creds.RAUsername, SteamAppID: steamAppID,
	}
	ctx := r.Context()
	report.RA = fetchRAResult(ctx, raID, creds)
	report.Steam = fetchSteamResult(ctx, steamAppID, creds)

	s.render(w, "suggest_report", suggestReportData{
		Page: newPage(r, "Suggest — "+summary.Title, "suggest"), Title: summary.Title, Slug: slug,
		Report: formatSuggestionReport(report),
	})
}

// ── Achievements (single-game refresh lives in server_games.go) ─────────

func (s *server) handleAchievementsHome(w http.ResponseWriter, r *http.Request) {
	s.render(w, "achievements", struct{ Page }{newPage(r, "Achievements", "achievements")})
}

// handleAchievementsAll runs the same fetch-and-merge loop as
// runAchievementsAll (achievements.go), the one operation slow enough that
// it can't run inside a single request — RA throttles to ~1.2s/call, and
// there can be 200+ linked games. Kicked off in a goroutine behind an
// in-memory job so the page can poll progress instead of the request
// hanging for minutes.
func (s *server) handleAchievementsAll(w http.ResponseWriter, r *http.Request) {
	id, j := s.jobs.create()
	go s.runAchievementsAllJob(j)
	http.Redirect(w, r, "/jobs/"+id, http.StatusSeeOther)
}

func (s *server) runAchievementsAllJob(j *job) {
	games, err := ListGames(s.gamesDir)
	if err != nil {
		j.finish(err)
		return
	}
	archiveDir := findArchiveDir(s.gamesDir)
	creds := loadCredentials()

	var refreshed, skipped, failed int
	for _, g := range games {
		links := g.ProviderLinks()
		if len(links) == 0 {
			skipped++
			continue
		}
		j.log("%s", g.Title)
		saved, err := saveAchievements(context.Background(), archiveDir, g.Title, links, creds)
		if err != nil {
			j.log("  %v", err)
			failed++
			continue
		}
		for _, sv := range saved {
			p := sv.Record
			if p.LastError != "" {
				j.log("  %s: %s (existing data kept)", sv.Provider, p.LastError)
			} else {
				j.log("  %s: %d/%d unlocked, %s to %s", sv.Provider, p.Unlocked, p.Total, p.First, p.Last)
			}
		}
		if _, err := writeAchievementSummary(archiveDir, filepath.Dir(g.Path), links); err != nil {
			j.log("  (achievement summary not updated: %v)", err)
		}
		refreshed++
	}
	j.log("")
	j.log("%d refreshed, %d with no provider link, %d failed", refreshed, skipped, failed)
	j.finish(nil)
}

type jobData struct {
	Page
	Status string
	Lines  []string
	Err    string
}

func (s *server) handleJobStatus(w http.ResponseWriter, r *http.Request) {
	j := s.jobs.get(r.PathValue("id"))
	if j == nil {
		http.NotFound(w, r)
		return
	}
	status, lines, errMsg := j.snapshot()
	s.render(w, "job", jobData{
		Page:   newPage(r, "Achievements — refresh all", "achievements"),
		Status: status, Lines: lines, Err: errMsg,
	})
}

// ── Project ──────────────────────────────────────────────────────────────

// handleProject regenerates every game's achievement-summary.yaml purely
// from what's already archived — no API calls (see runProject in
// achievements.go), so unlike achievements/all this is fast enough to run
// synchronously.
func (s *server) handleProject(w http.ResponseWriter, r *http.Request) {
	games, err := ListGames(s.gamesDir)
	if err != nil {
		redirectErr(w, r, "/achievements", err)
		return
	}
	archiveDir := findArchiveDir(s.gamesDir)

	var written, empty, failed int
	for _, g := range games {
		links := g.ProviderLinks()
		if len(links) == 0 {
			continue
		}
		wrote, err := writeAchievementSummary(archiveDir, filepath.Dir(g.Path), links)
		switch {
		case err != nil:
			failed++
		case wrote:
			written++
		default:
			empty++
		}
	}
	redirectOK(w, r, "/achievements", fmt.Sprintf("%d summaries written, %d with nothing archived yet, %d failed", written, empty, failed))
}
