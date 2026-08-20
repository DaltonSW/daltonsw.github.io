package server

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"

	"go.dalton.dog/gamelog/internal/commands"
	"go.dalton.dog/gamelog/internal/externalid"
	"go.dalton.dog/gamelog/internal/model"
)

// ── Suggest (read-only) ──────────────────────────────────────────────────

type suggestPickerData struct {
	Page
	Games []model.GameSummary
}

func (s *server) handleSuggestPicker(w http.ResponseWriter, r *http.Request) {
	games, err := model.ListGames(s.gamesDir)
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
	games, err := model.ListGames(s.gamesDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var summary model.GameSummary
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
	doc, err := model.LoadDoc(summary.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	raID, steamAppID := doc.ExternalIDs()
	if id, err := externalid.ParseExternalID(raID); err == nil {
		raID = id
	}
	if id, err := externalid.ParseExternalID(steamAppID); err == nil {
		steamAppID = id
	}
	creds := commands.LoadCredentials()

	report := commands.SuggestionReport{
		Title: summary.Title, Slug: slug, NumPlaythroughs: summary.NumPlaythroughs,
		RAGameID: raID, RAUsername: creds.RAUsername, SteamAppID: steamAppID,
	}
	ctx := r.Context()
	report.RA = commands.FetchRAResult(ctx, raID, creds)
	report.Steam = commands.FetchSteamResult(ctx, steamAppID, creds)

	s.render(w, "suggest_report", suggestReportData{
		Page: newPage(r, "Suggest — "+summary.Title, "suggest"), Title: summary.Title, Slug: slug,
		Report: commands.FormatSuggestionReport(report),
	})
}

// ── Achievements (single-game refresh lives in server_games.go) ─────────

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
	games, err := model.ListGames(s.gamesDir)
	if err != nil {
		j.finish(err)
		return
	}
	archiveDir := model.FindArchiveDir(s.gamesDir)
	creds := commands.LoadCredentials()

	var withLinks []model.GameSummary
	for _, g := range games {
		if len(g.ProviderLinks()) > 0 {
			withLinks = append(withLinks, g)
		}
	}
	skipped, total := len(games)-len(withLinks), len(withLinks)

	var refreshed, failed int
	for i, g := range withLinks {
		links := g.ProviderLinks()
		j.progress(i+1, total, g.Title)
		j.log("%s", g.Title)
		saved, err := commands.SaveAchievements(context.Background(), archiveDir, g.Title, links, creds)
		if err != nil {
			j.log("  %v", err)
			failed++
			continue
		}
		for _, sv := range saved {
			p := sv.Record
			if p.LastError != "" {
				j.log("  %s: %s (existing data kept)", sv.Label(), p.LastError)
			} else {
				j.log("  %s: %d/%d unlocked, %s to %s", sv.Label(), p.Unlocked, p.Total, p.First, p.Last)
			}
		}
		if _, err := model.WriteAchievementSummary(archiveDir, filepath.Dir(g.Path), links); err != nil {
			j.log("  (achievement summary not updated: %v)", err)
		}
		refreshed++
	}
	j.progress(total, total, "")
	j.log("")
	j.log("%d refreshed, %d with no provider link, %d failed", refreshed, skipped, failed)
	j.finish(nil)
}

type jobData struct {
	Page
	ID          string
	Status      string
	Lines       []string
	Err         string
	Current     int
	Total       int
	CurrentItem string
}

func (s *server) handleJobStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	j := s.jobs.get(id)
	if j == nil {
		http.NotFound(w, r)
		return
	}
	status, lines, errMsg, current, total, currentItem := j.snapshot()
	// The job monitor is a console, not a document: it claims the viewport
	// and scrolls its log inside itself, so the page must not scroll too.
	page := newPage(r, "Achievements — refresh all", "housekeeping")
	page.BodyClass = "body--fixed"
	s.render(w, "job", jobData{
		Page: page,
		ID:   id, Status: status, Lines: lines, Err: errMsg,
		Current: current, Total: total, CurrentItem: currentItem,
	})
}

// handleJobProgress renders just the #job-progress fragment (see
// job_progress.html) — the htmx poll target handleJobStatus's page swaps in
// place every few seconds while the job runs, instead of the full-page
// reload the status page used to require.
func (s *server) handleJobProgress(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	j := s.jobs.get(id)
	if j == nil {
		http.NotFound(w, r)
		return
	}
	status, lines, errMsg, current, total, currentItem := j.snapshot()
	s.renderJobProgress(w, jobData{
		ID: id, Status: status, Lines: lines, Err: errMsg,
		Current: current, Total: total, CurrentItem: currentItem,
	})
}

// ── Project ──────────────────────────────────────────────────────────────

// handleProject regenerates every game's achievement-summary.yaml purely
// from what's already archived — no API calls (see runProject in
// achievements.go), so unlike achievements/all this is fast enough to run
// synchronously.
func (s *server) handleProject(w http.ResponseWriter, r *http.Request) {
	backTo := housekeepingReturnTo(r)
	games, err := model.ListGames(s.gamesDir)
	if err != nil {
		redirectErr(w, r, backTo, err)
		return
	}
	archiveDir := model.FindArchiveDir(s.gamesDir)

	var written, empty, failed int
	for _, g := range games {
		links := g.ProviderLinks()
		if len(links) == 0 {
			continue
		}
		wrote, err := model.WriteAchievementSummary(archiveDir, filepath.Dir(g.Path), links)
		switch {
		case err != nil:
			failed++
		case wrote:
			written++
		default:
			empty++
		}
	}
	redirectOK(w, r, backTo, fmt.Sprintf("%d summaries written, %d with nothing archived yet, %d failed", written, empty, failed))
}
