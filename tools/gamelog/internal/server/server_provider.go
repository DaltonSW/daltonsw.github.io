package server

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"strings"

	"go.dalton.dog/gamelog/internal/commands"
	"go.dalton.dog/gamelog/internal/externalid"
	"go.dalton.dog/gamelog/internal/forms"
	"go.dalton.dog/gamelog/internal/model"
)

// ── Suggest (read-only) ──────────────────────────────────────────────────

// suggestPath is where a game's suggestion lives: its own detail page, with
// the panel fetched on arrival — so every entry point (the games list's
// Suggest button, old /suggest/{slug} links) lands where the suggestion can be
// logged, not on a read-only report that has to be retyped somewhere else.
func suggestPath(slug string) string { return gamePath(slug) + "?suggest=1#suggest" }

// handleSuggestReport keeps old /suggest/{slug} links working now that the
// suggestion is shown on the game's own page.
func (s *server) handleSuggestReport(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, suggestPath(r.PathValue("slug")), http.StatusSeeOther)
}

// handleSuggestIndex keeps /suggest working now that its whole-library sweep
// is Catch up's unlogged-activity list.
func (s *server) handleSuggestIndex(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/", http.StatusSeeOther)
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
	Covered    string // set when every activity day is already logged; no forms then

	// Pre-fill. Finished is left blank unless the provider actually signals
	// a finish (an RA award, or every Steam achievement unlocked); the
	// last-activity date goes in FinishedHint instead, so logging a
	// suggestion never claims a finish nobody typed.
	Started      string
	Finished     string
	FinishedHint string
	Status       string
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
	report = commands.WithCoverage(report, pf)
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
		v.SessionTargets = append(v.SessionTargets, sessionTarget{
			Action: fmt.Sprintf("/games/%s/playthroughs/%d/sessions", report.Slug, p.Index),
			Label:  runLabel(p),
		})
	}
	// The newest playthrough is the likeliest one a fresh session belongs to.
	if n := len(v.SessionTargets); n > 0 {
		v.SessionTargets[n-1].Selected = true
	}
	return v
}

// runLabel names an existing playthrough in a "log it against which run?"
// choice: its number, status, and whatever distinguishes it from a sibling.
func runLabel(p model.Playthrough) string {
	label := fmt.Sprintf("#%d · %s", p.Index+1, p.Status)
	if p.Subgame != "" {
		label += " · " + p.Subgame
	}
	if p.Platform != "" {
		label += " · " + p.Platform
	}
	return label
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
	if c := pr.Coverage; c != nil && c.Uncovered == 0 {
		out.Covered = commands.CoveredSummary(c)
		return out
	}
	started, last := commands.Day(s.Started), commands.Day(s.Finished)
	out.Started = started
	defer func() { out.Detail += partialCoverage(pr.Coverage) }()
	if s.Confidence == "high" {
		out.Confidence = "high (" + s.AwardKind + ")"
		out.Detail = fmt.Sprintf("earliest achievement %s · %s %s", started, s.AwardKind, last)
		out.Finished = last
		// A mastery award still only says the run finished; the mastery
		// itself is read from the archive (model.DeriveMastery), not a status.
		out.Status = "finished"
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
	if c := pr.Coverage; c != nil && c.Uncovered == 0 {
		out.Covered = commands.CoveredSummary(c)
		return out
	}
	started, last := commands.Day(s.Started), commands.Day(s.Finished)
	out.Started = started
	out.Confidence = "low — guessed from achievement unlock times"
	out.Detail = fmt.Sprintf("%d/%d achievements · earliest unlock %s · latest %s", s.UnlockedCount, s.TotalCount, started, last)
	if pr.SteamPlaytimeKnown {
		out.Detail += " · " + commands.FormatHours(pr.SteamPlaytimeMinutes) + " total playtime"
	}
	out.Detail += partialCoverage(pr.Coverage)
	// Every achievement unlocked only signals a finish if the last unlock is
	// new; otherwise it belongs to a run that's already logged.
	if s.UnlockedCount >= s.TotalCount && (pr.Coverage == nil || !pr.Coverage.FinishCovered) {
		out.Finished = last
		out.Status = "finished"
		return out
	}
	out.FinishedHint = last
	out.Status = defaultStatus
	return out
}

// partialCoverage notes, after a provider's detail line, that its dates have
// been narrowed to the activity not already logged.
func partialCoverage(c *commands.Coverage) string {
	if c == nil || c.Uncovered == c.Days {
		return ""
	}
	return fmt.Sprintf(" · %d of %d activity days not yet logged (dates above are those)", c.Uncovered, c.Days)
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
	status := forms.DefaultPlaythroughStatus(doc.FM.Status)
	v := buildSuggestView(report, doc, pf, status)
	v.Providers = append(v.Providers, s.archiveSuggestion(slug, doc, pf, status))
	s.renderFragment(w, suggestFragmentsCache, "suggest_fragments.html", "suggest_panel", v)
}

// archiveSuggestion is the panel's block for everything the live RA/Steam
// requests can't see: every linked record's last_played, award dates and
// unlocks on all providers, read from the archive with no API call. It's the
// same read Catch up does, narrowed to this one game — so a Steam game with
// no achievements still gets a suggestion from its last-played date.
func (s *server) archiveSuggestion(slug string, doc *model.Doc, pf *model.PlaythroughsFile, defaultStatus string) providerSuggestion {
	out := providerSuggestion{Name: "Archive (all linked providers)"}
	g := model.GameSummaryFor(slug, doc.Path, doc, pf)
	if len(g.ProviderLinks()) == 0 {
		out.SkipReason = "no provider records linked to this game"
		return out
	}
	rep, err := commands.FindUnloggedActivity(model.FindArchiveDir(s.gamesDir), []model.GameSummary{g},
		commands.ActivityOptions{StaleDays: 90})
	if err != nil {
		out.SkipReason = "couldn't read the archive: " + err.Error()
		return out
	}
	if len(rep.Items) == 0 {
		out.SkipReason = "the archive has no dated activity outside logged playthroughs (or none captured yet — refresh achievements)"
		return out
	}
	it := rep.Items[0]
	var parts []string
	for _, p := range it.Providers {
		d := fmt.Sprintf("%s %s", p.Label, p.First)
		if p.Last != p.First {
			d += " – " + p.Last
		}
		if p.LastPlayed != "" {
			d += " (last played " + p.LastPlayed + ")"
		}
		parts = append(parts, d)
	}
	out.Confidence = "low — dates from archived unlocks, last-played and awards"
	out.Detail = strings.Join(parts, " · ")
	if it.Award != "" {
		out.Detail += fmt.Sprintf(" · %s %s", it.AwardKind, it.Award)
	}
	out.Started = it.Guess.Started
	out.Finished = it.Guess.Finished
	out.FinishedHint = it.Guess.FinishedHint
	out.Status = it.Guess.Status
	if out.Status == "" {
		out.Status = defaultStatus
	}
	return out
}

// ── Achievements (single-game refresh lives in server_games.go) ─────────

// handleAchievementsAll runs the same fetch-and-merge loop as
// runAchievementsAll (achievements.go), the one operation slow enough that
// it can't run inside a single request — RA throttles to ~1.2s/call, and
// there can be 200+ linked games. Kicked off in a goroutine behind an
// in-memory job so the page can poll progress instead of the request
// hanging for minutes.
//
// An optional "provider" form value narrows the sweep to that one provider:
// only games linked to it are visited, and only that link is fetched. The
// achievement summary is still rebuilt from every link, since it reads the
// archive rather than the fetch.
func (s *server) handleAchievementsAll(w http.ResponseWriter, r *http.Request) {
	provider := r.FormValue("provider")
	title := "Achievements — refresh all"
	if provider != "" {
		if !slices.Contains(model.ProviderOrder, provider) {
			http.Error(w, "unknown provider "+provider, http.StatusBadRequest)
			return
		}
		title = "Achievements — refresh " + model.ProviderLabel(provider)
	}
	id, j := s.jobs.create(title, "/housekeeping", "Back to housekeeping")
	go s.runAchievementsAllJob(j, r.FormValue("full") == "1", provider)
	http.Redirect(w, r, "/jobs/"+id, http.StatusSeeOther)
}

// fetchLinks is the subset of links a sweep fetches: all of them, or just
// the ones on provider when it's set.
func fetchLinks(links []model.ProviderLink, provider string) []model.ProviderLink {
	if provider == "" {
		return links
	}
	var out []model.ProviderLink
	for _, l := range links {
		if l.Provider == provider {
			out = append(out, l)
		}
	}
	return out
}

func (s *server) runAchievementsAllJob(j *job, full bool, provider string) {
	games, err := model.ListGames(s.gamesDir)
	if err != nil {
		j.finish(err)
		return
	}
	var withLinks []model.GameSummary
	for _, g := range games {
		if len(fetchLinks(g.ProviderLinks(), provider)) > 0 {
			withLinks = append(withLinks, g)
		}
	}
	skipped := len(games) - len(withLinks)

	counts := s.refreshAchievements(j, withLinks, refreshOptions{full: full, provider: provider, total: len(withLinks)})

	j.progress(len(withLinks), len(withLinks), "")
	j.log("")
	noLink := "no provider link"
	if provider != "" {
		noLink = "no " + model.ProviderLabel(provider) + " link"
	}
	j.log("%d refreshed, %d with %s, %d failed", counts.refreshed, skipped, noLink, counts.failed)
	j.finish(nil)
}

// refreshOptions tunes refreshAchievements.
type refreshOptions struct {
	full     bool            // re-fetch Steam games whose playtime/last-played haven't moved
	provider string          // only this provider's links, when set
	only     map[string]bool // only these providers' links, when non-nil
	// quiet logs only the games whose archive actually changed (and failures),
	// with what changed, instead of a line per provider per game — Catch up
	// walks 400+ games and wants the dozen that moved.
	quiet bool
	// base and total place this loop's games within the job's progress bar,
	// for a job that does other work around it.
	base, total int
}

type refreshCounts struct{ refreshed, changed, failed int }

// refreshAchievements is the fetch-and-merge loop behind both refresh-all
// and Catch up: SaveAchievements per game (every write a merge that can only
// add — see model.mergeProvider), then that game's achievement summary.
func (s *server) refreshAchievements(j *job, games []model.GameSummary, o refreshOptions) refreshCounts {
	archiveDir := model.FindArchiveDir(s.gamesDir)
	creds := commands.LoadCredentials()
	var opts []commands.SaveOption
	if !o.full {
		opts = append(opts, commands.SkipUnchanged)
	}

	var c refreshCounts
	for i, g := range games {
		links := g.ProviderLinks()
		fetch := fetchLinks(links, o.provider)
		if o.only != nil {
			fetch = slices.DeleteFunc(slices.Clone(fetch), func(l model.ProviderLink) bool { return !o.only[l.Provider] })
		}
		j.progress(o.base+i+1, o.total, g.Title)

		var before map[string]*model.ArchiveRecord
		if o.quiet {
			before = map[string]*model.ArchiveRecord{}
			for _, l := range fetch {
				if rec, err := model.LoadRecord(archiveDir, l.Provider, l.ID); err == nil {
					before[l.Provider+"/"+l.ID] = rec
				}
			}
		}

		saved, err := commands.SaveAchievements(context.Background(), archiveDir, g.Title, fetch, creds, opts...)
		if err != nil {
			j.log("%s", g.Title)
			j.log("  %v", err)
			c.failed++
			continue
		}
		var lines []string
		for _, sv := range saved {
			p := sv.Record
			switch {
			case o.quiet:
				if line := describeChange(sv.Label(), before[sv.Provider+"/"+sv.ID], p); line != "" {
					lines = append(lines, line)
				}
			case sv.Skipped:
				lines = append(lines, fmt.Sprintf("%s: unchanged since last fetch, skipped", sv.Label()))
			case p.LastError != "":
				lines = append(lines, fmt.Sprintf("%s: %s (existing data kept)", sv.Label(), p.LastError))
			default:
				lines = append(lines, fmt.Sprintf("%s: %s", sv.Label(), commands.DescribeUnlocks(p)))
			}
		}
		if _, err := model.WriteAchievementSummary(archiveDir, filepath.Dir(g.Path), links); err != nil {
			lines = append(lines, fmt.Sprintf("(achievement summary not updated: %v)", err))
		}
		c.refreshed++
		if len(lines) > 0 {
			if o.quiet {
				c.changed++
			}
			j.log("%s", g.Title)
			for _, l := range lines {
				j.log("  %s", l)
			}
		}
	}
	return c
}

// describeChange is one record's line in Catch up's quiet log: what a refresh
// added, or why it couldn't, or "" when nothing moved.
func describeChange(label string, before, after *model.ArchiveRecord) string {
	if after == nil {
		return ""
	}
	if after.LastError != "" && (before == nil || before.LastAttempt != after.LastAttempt || before.LastError != after.LastError) {
		return fmt.Sprintf("%s: %s (existing data kept)", label, after.LastError)
	}
	if before == nil {
		return fmt.Sprintf("%s: first capture, %s", label, commands.DescribeUnlocks(after))
	}
	var parts []string
	if d := after.Unlocked - before.Unlocked; d > 0 {
		parts = append(parts, fmt.Sprintf("+%d (%d/%d)", d, after.Unlocked, after.Total))
	}
	if after.LastPlayed != "" && after.LastPlayed != before.LastPlayed {
		parts = append(parts, "last played "+after.LastPlayed)
	}
	if len(parts) == 0 {
		return ""
	}
	return label + ": " + strings.Join(parts, ", ")
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
	page := newPage(r, j.Title, j.Nav)
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
	if status == "done" && j.DoneHref != "" {
		w.Header().Set("HX-Redirect", j.DoneHref)
	}
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
