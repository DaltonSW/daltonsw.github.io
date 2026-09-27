package server

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"go.dalton.dog/gamelog/internal/commands"
	"go.dalton.dog/gamelog/internal/externalid"
	"go.dalton.dog/gamelog/internal/model"
)

// ── Suggest (read-only) ──────────────────────────────────────────────────

type suggestPickerData struct {
	Page
	Games []model.GameSummary
	Query string
}

// handleSuggestPicker lists only RA/Steam-linked games — nothing else can
// produce a suggestion — with the same title filter as the Games page.
func (s *server) handleSuggestPicker(w http.ResponseWriter, r *http.Request) {
	games, err := model.ListGames(s.gamesDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	lower := strings.ToLower(q)
	var linked []model.GameSummary
	for _, g := range games {
		if g.RAGameID == "" && g.SteamAppID == "" {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(g.Title), lower) {
			continue
		}
		linked = append(linked, g)
	}
	s.render(w, "suggest_picker", suggestPickerData{Page: newPage(r, "Suggest", "suggest"), Games: linked, Query: q})
}

// suggestPath is where a game's suggestion lives: its own detail page, with
// the panel fetched on arrival — so every entry point (games list, picker,
// sweep log) lands where the suggestion can be logged, not on a read-only
// report that has to be retyped somewhere else.
func suggestPath(slug string) string { return gamePath(slug) + "?suggest=1#suggest" }

// handleSuggestReport keeps old /suggest/{slug} links working now that the
// suggestion is shown on the game's own page.
func (s *server) handleSuggestReport(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, suggestPath(r.PathValue("slug")), http.StatusSeeOther)
}

// suggestionFor runs the same provider fetches as `gamelog suggest` for one
// game. It returns the loaded doc and playthroughs too, since every caller
// needs them to decide what to pre-fill.
func (s *server) suggestionFor(ctx context.Context, slug string) (commands.SuggestionReport, *model.Doc, *model.PlaythroughsFile, error) {
	doc, pf, err := s.loadGame(slug)
	if err != nil {
		return commands.SuggestionReport{}, nil, nil, err
	}
	raID, steamAppID := doc.ExternalIDs()
	if id, err := externalid.ParseExternalID(raID); err == nil {
		raID = id
	}
	if id, err := externalid.ParseExternalID(steamAppID); err == nil {
		steamAppID = id
	}
	creds := commands.LoadCredentials()
	g := model.GameSummaryFor(slug, doc.Path, doc, pf)

	report := commands.SuggestionReport{
		Title: doc.FM.Title, Slug: slug, NumPlaythroughs: g.NumPlaythroughs,
		RAGameID: raID, RAUsername: creds.RAUsername, SteamAppID: steamAppID,
	}
	report.RA = commands.FetchRAResult(ctx, raID, creds)
	report.Steam = commands.FetchSteamResult(ctx, steamAppID, creds)
	return report, doc, pf, nil
}

// providerSuggestion is one provider's block in the suggest panel: what it
// found, and — when it found dates — the values its forms are pre-filled with.
type providerSuggestion struct {
	Name       string
	Confidence string
	Detail     string
	SkipReason string // set when the provider was skipped or failed; no forms then

	// Pre-fill. Finished is left blank unless the provider actually signals
	// a finish (an RA award, or every Steam achievement unlocked); the
	// last-activity date goes in FinishedHint instead, so logging a
	// suggestion never claims a finish nobody typed.
	Started      string
	Finished     string
	FinishedHint string
	Status       string
	Notes        string
}

// sessionTarget is one existing playthrough a suggestion can be logged
// against as a new session.
type sessionTarget struct {
	Action   string
	Label    string
	Selected bool
}

type suggestView struct {
	Slug            string
	NumPlaythroughs int
	GamePlatform    string
	GameSubgames    []string
	Providers       []providerSuggestion
	SessionTargets  []sessionTarget
	Error           string
}

// buildSuggestView turns a report into the suggest panel's pre-filled forms.
// defaultStatus is the status used when the provider doesn't signal a finish
// — the same game-aware default the "start a new playthrough" form uses.
func buildSuggestView(report commands.SuggestionReport, doc *model.Doc, pf *model.PlaythroughsFile, defaultStatus string) suggestView {
	v := suggestView{
		Slug:            report.Slug,
		NumPlaythroughs: report.NumPlaythroughs,
		GamePlatform:    doc.FM.Platform,
		GameSubgames:    doc.FM.Subgames,
		Providers: []providerSuggestion{
			raSuggestion(report.RA, defaultStatus),
			steamSuggestion(report.Steam, defaultStatus),
		},
	}
	for _, p := range pf.Views() {
		if p.Status == "planned" {
			continue
		}
		label := fmt.Sprintf("#%d · %s", p.Index+1, p.Status)
		if p.Subgame != "" {
			label += " · " + p.Subgame
		}
		if p.Platform != "" {
			label += " · " + p.Platform
		}
		v.SessionTargets = append(v.SessionTargets, sessionTarget{
			Action: fmt.Sprintf("/games/%s/playthroughs/%d/sessions", report.Slug, p.Index),
			Label:  label,
		})
	}
	// The newest playthrough is the likeliest one a fresh session belongs to.
	if n := len(v.SessionTargets); n > 0 {
		v.SessionTargets[n-1].Selected = true
	}
	return v
}

func skippedReason(pr commands.ProviderResult) string {
	if pr.Errored {
		return fmt.Sprintf("request failed: %v", pr.Err)
	}
	return pr.Reason
}

func raSuggestion(pr commands.ProviderResult, defaultStatus string) providerSuggestion {
	out := providerSuggestion{Name: "RetroAchievements"}
	s := pr.RA
	if s == nil {
		out.SkipReason = skippedReason(pr)
		return out
	}
	started, last := commands.Day(s.Started), commands.Day(s.Finished)
	out.Started = started
	out.Notes = "dates suggested from RetroAchievements"
	if s.Confidence == "high" {
		out.Confidence = "high (" + s.AwardKind + ")"
		out.Detail = fmt.Sprintf("earliest achievement %s · %s %s", started, s.AwardKind, last)
		out.Finished = last
		out.Status = "finished"
		if s.AwardKind == "mastered" || s.AwardKind == "completed" {
			out.Status = "mastered"
		}
		out.Notes += " (" + s.AwardKind + ")"
		return out
	}
	out.Confidence = "medium — no award, not confirmed finished"
	out.Detail = fmt.Sprintf("earliest achievement %s · latest %s", started, last)
	out.FinishedHint = last
	out.Status = defaultStatus
	return out
}

func steamSuggestion(pr commands.ProviderResult, defaultStatus string) providerSuggestion {
	out := providerSuggestion{Name: "Steam"}
	s := pr.Steam
	if s == nil {
		out.SkipReason = skippedReason(pr)
		return out
	}
	started, last := commands.Day(s.Started), commands.Day(s.Finished)
	out.Started = started
	out.Confidence = "low — guessed from achievement unlock times"
	out.Detail = fmt.Sprintf("%d/%d achievements · earliest unlock %s · latest %s", s.UnlockedCount, s.TotalCount, started, last)
	if pr.SteamPlaytimeKnown {
		out.Detail += " · " + commands.FormatHours(pr.SteamPlaytimeMinutes) + " total playtime"
	}
	out.Notes = fmt.Sprintf("dates suggested from Steam achievements (%d/%d)", s.UnlockedCount, s.TotalCount)
	if s.UnlockedCount >= s.TotalCount {
		out.Finished = last
		out.Status = "finished"
		return out
	}
	out.FinishedHint = last
	out.Status = defaultStatus
	return out
}

// handleSuggestFragment renders the game page's suggest panel on demand
// (htmx), never as part of the page itself: RA throttles to ~1.2s a request,
// too slow to pay on every visit. Failures render inside the panel with a
// 200, since htmx 2 won't swap an error response in.
func (s *server) handleSuggestFragment(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	report, doc, pf, err := s.suggestionFor(r.Context(), slug)
	if err != nil {
		s.renderFragment(w, suggestFragmentsCache, "suggest_fragments.html", "suggest_panel", suggestView{Slug: slug, Error: err.Error()})
		return
	}
	s.renderFragment(w, suggestFragmentsCache, "suggest_fragments.html", "suggest_panel",
		buildSuggestView(report, doc, pf, defaultPthStatus(doc.FM.Status)))
}

// handleSuggestAll kicks off runSuggestAllJob. Like achievements/all it's a
// background job: the picker's one-game-at-a-time flow means opening 200+
// games by hand to find the few with unlogged history, and doing that sweep
// in one request would hang it for minutes (Steam is a couple of calls per
// game, RA throttles to ~1.2s).
func (s *server) handleSuggestAll(w http.ResponseWriter, r *http.Request) {
	id, j := s.jobs.create("Suggest — sweep all games", "/suggest", "Back to suggest")
	go s.runSuggestAllJob(j)
	http.Redirect(w, r, "/jobs/"+id, http.StatusSeeOther)
}

// runSuggestAllJob runs the per-game suggestion fetch (commands.FetchRAResult
// / FetchSteamResult — the very calls the single-game /suggest/{slug} page
// makes) across every RetroAchievements/Steam-linked game, then logs only
// the games whose provider history isn't already covered by their
// playthroughs.yaml (see commands.SuggestSweepVerdict). Nothing is written —
// this is the same read-only "here are dates to enter by hand" tool as
// single-game suggest, just swept across the library.
func (s *server) runSuggestAllJob(j *job) {
	games, err := model.ListGames(s.gamesDir)
	if err != nil {
		j.finish(err)
		return
	}
	creds := commands.LoadCredentials()

	var linked []model.GameSummary
	for _, g := range games {
		if g.RAGameID != "" || g.SteamAppID != "" {
			linked = append(linked, g)
		}
	}
	total := len(linked)
	ctx := context.Background()

	var flagged, failed int
	for i, g := range linked {
		j.progress(i+1, total, g.Title)

		raID, steamAppID := g.RAGameID, g.SteamAppID
		if id, err := externalid.ParseExternalID(raID); err == nil {
			raID = id
		}
		if id, err := externalid.ParseExternalID(steamAppID); err == nil {
			steamAppID = id
		}

		report := commands.SuggestionReport{
			Title: g.Title, Slug: g.Slug, NumPlaythroughs: g.NumPlaythroughs,
			RAGameID: raID, RAUsername: creds.RAUsername, SteamAppID: steamAppID,
		}
		report.RA = commands.FetchRAResult(ctx, raID, creds)
		report.Steam = commands.FetchSteamResult(ctx, steamAppID, creds)

		if report.RA.Errored {
			j.log("%s", g.Title)
			j.log("  RetroAchievements request failed: %v", report.RA.Err)
			failed++
		}
		if report.Steam.Errored {
			j.log("%s", g.Title)
			j.log("  Steam request failed: %v", report.Steam.Err)
			failed++
		}

		pf, err := model.LoadPlaythroughs(filepath.Dir(g.Path))
		if err != nil {
			j.log("%s", g.Title)
			j.log("  could not read playthroughs.yaml: %v", err)
			failed++
			continue
		}

		actionable, note := commands.SuggestSweepVerdict(report, pf)
		if !actionable {
			continue
		}
		flagged++
		j.logLink(suggestPath(g.Slug), "%s", g.Title)
		j.log("  %s", note)
		if r := report.RA.RA; r != nil {
			j.log("  RetroAchievements: %s", suggestRangeLine(r.Started, r.Finished, r.Confidence))
		}
		if st := report.Steam.Steam; st != nil {
			j.log("  Steam: %s", suggestRangeLine(st.Started, st.Finished, ""))
		}
	}

	j.progress(total, total, "")
	j.log("")
	j.log("%d linked games checked, %d need a playthrough logged or updated, %d errors",
		total, flagged, failed)
	j.finish(nil)
}

// suggestRangeLine renders a provider's suggested started→finished pair for a
// job log line, tolerating a zero end (an open range) and an optional
// confidence tag.
func suggestRangeLine(start, finish time.Time, confidence string) string {
	fmtDate := func(t time.Time) string {
		if t.IsZero() {
			return "?"
		}
		return t.Format("2006-01-02")
	}
	line := fmt.Sprintf("%s → %s", fmtDate(start), fmtDate(finish))
	if confidence != "" {
		line += " (" + confidence + " confidence)"
	}
	return line
}

// ── Achievements (single-game refresh lives in server_games.go) ─────────

// handleAchievementsAll runs the same fetch-and-merge loop as
// runAchievementsAll (achievements.go), the one operation slow enough that
// it can't run inside a single request — RA throttles to ~1.2s/call, and
// there can be 200+ linked games. Kicked off in a goroutine behind an
// in-memory job so the page can poll progress instead of the request
// hanging for minutes.
func (s *server) handleAchievementsAll(w http.ResponseWriter, r *http.Request) {
	id, j := s.jobs.create("Achievements — refresh all", "/housekeeping", "Back to housekeeping")
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
	Lines       []logLine
	Err         string
	Current     int
	Total       int
	CurrentItem string
	Heading     string // job.Title — the monitor's <h1>
	BackHref    string // job.BackHref
	BackLabel   string // job.BackLabel
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
	page := newPage(r, j.Title, "housekeeping")
	page.BodyClass = "body--fixed"
	s.render(w, "job", jobData{
		Page: page,
		ID:   id, Status: status, Lines: lines, Err: errMsg,
		Current: current, Total: total, CurrentItem: currentItem,
		Heading: j.Title, BackHref: j.BackHref, BackLabel: j.BackLabel,
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
