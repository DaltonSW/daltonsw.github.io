package server

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.dalton.dog/gamelog/internal/commands"
	"go.dalton.dog/gamelog/internal/forms"
	"go.dalton.dog/gamelog/internal/model"
	"go.dalton.dog/gamelog/internal/providers/nadeo"
)

// ── Catch up ─────────────────────────────────────────────────────────────
//
// The page you open after time away: one "Load what's new" run (scan for new
// games, refresh every provider into the archive, BTD6, and Trackmania only
// when asked), then a single queue of things to act on in place — unlogged
// activity, new games, games gone quiet, runs left open. The activity half is
// read off the archive (commands.FindUnloggedActivity) rather than fetched
// again, so it covers every provider the refresh just wrote.

// catchupThresholds are the page's tunable lines, carried as GET params and
// as hidden inputs on every form that posts back here.
type catchupThresholds struct {
	MinHours  float64
	StaleDays int
	CloseDays int
	CloseAll  bool
}

func thresholdsFrom(get func(string) string) catchupThresholds {
	return catchupThresholds{
		MinHours:  atoiFloatOr(get("min_hours"), commands.DefaultMinHours),
		StaleDays: atoiOr(get("stale_days"), commands.DefaultStaleDays),
		CloseDays: atoiOr(get("close_days"), commands.DefaultStaleDays),
		CloseAll:  get("close_all") == "1",
	}
}

// activityTarget is one choice in a card's "log as" select.
type activityTarget struct {
	Value    string // "new", or a playthrough index
	Label    string
	Selected bool
}

// activityCard is an ActivityItem plus the values its form shows — the guess
// at first, or what was typed when a submit came back with an error.
type activityCard struct {
	commands.ActivityItem
	Targets      []activityTarget
	Target       string
	Started      string
	Finished     string
	FinishedHint string
	Beaten       string
	Status       string
	Notes        string
	Error        string
	Thresholds   catchupThresholds
}

func newActivityCard(it commands.ActivityItem, th catchupThresholds) activityCard {
	g := it.Guess
	c := activityCard{
		ActivityItem: it, Started: g.Started, Finished: g.Finished, FinishedHint: g.FinishedHint,
		Beaten: g.Beaten, Status: g.Status, Target: "new", Thresholds: th,
	}
	if g.Target >= 0 {
		c.Target = strconv.Itoa(g.Target)
	}
	if c.Status == "" {
		c.Status = forms.DefaultPlaythroughStatus(it.Game.Status)
	}
	c.setTargets()
	return c
}

func (c *activityCard) setTargets() {
	c.Targets = []activityTarget{{Value: "new", Label: "new playthrough", Selected: c.Target == "new"}}
	for _, p := range c.Runs {
		v := strconv.Itoa(p.Index)
		c.Targets = append(c.Targets, activityTarget{Value: v, Label: "session on " + runLabel(p), Selected: c.Target == v})
	}
}

// IsSession reports whether the card is set to add a session, which is what
// disables its status select (a session has no status of its own).
func (c activityCard) IsSession() bool { return c.Target != "new" }

type onDemandProvider struct{ Key, Label string }

// OnDemandScan is the providers whose New games lists wait for a button.
func (catchupData) OnDemandScan() []onDemandProvider {
	var out []onDemandProvider
	for _, p := range commands.OnDemandScanProviders {
		out = append(out, onDemandProvider{p, model.ProviderLabel(p)})
	}
	return out
}

type catchupData struct {
	Page
	Thresholds catchupThresholds

	LastRun    string // date the last run started, "" if never
	LastRunAgo string
	Since      string // the "new since" boundary, as a day
	RunningJob string // id of a run still in flight

	GamesPlayed int
	Unlocks     int
	Problems    []commands.RecordProblem
	Notes       []string

	New   []activityCard
	Older []activityCard
	Stale []staleRow
	Close []commands.OpenEntry
	// Drafts counts scan-created games still marked draft: reviewing one
	// means reading its inferred fields, so it's a link, not a button.
	Drafts int
}

// Quiet reports whether the always-present sections have nothing in them,
// for the "all caught up" line. New games load separately and say so
// themselves.
func (d catchupData) Quiet() bool {
	return len(d.New) == 0 && len(d.Stale) == 0 && len(d.Close) == 0 && len(d.Problems) == 0 && len(d.Notes) == 0
}

func (s *server) handleCatchup(w http.ResponseWriter, r *http.Request) {
	th := thresholdsFrom(r.URL.Query().Get)
	archiveDir := model.FindArchiveDir(s.gamesDir)
	data := catchupData{Page: newPage(r, "Catch up", "catchup"), Thresholds: th}

	st, err := model.LoadCatchup(archiveDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	now := time.Now()
	since := catchupSince(st, now)
	data.Since = commands.Day(since)
	if last := st.LastRunTime(); !last.IsZero() {
		data.LastRun = commands.Day(last)
		data.LastRunAgo = daysAgo(last, now)
	}

	s.catchupMu.Lock()
	if s.jobs != nil && s.jobs.running(s.catchupJobID) {
		data.RunningJob = s.catchupJobID
	}
	data.Notes = append([]string(nil), s.catchupNotes...)
	s.catchupMu.Unlock()

	games, err := model.ListGames(s.gamesDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rep, err := commands.FindUnloggedActivity(archiveDir, games, commands.ActivityOptions{
		Reviewed: st, Since: since, StaleDays: th.StaleDays, ProblemsSince: st.LastRunTime(), Now: now,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data.GamesPlayed, data.Unlocks, data.Problems = rep.GamesPlayed, rep.Unlocks, rep.Problems
	for _, it := range rep.Items {
		c := newActivityCard(it, th)
		if it.IsNew(since) {
			data.New = append(data.New, c)
		} else {
			data.Older = append(data.Older, c)
		}
	}

	stale, err := commands.FindStaleCandidates(archiveDir, games, th.StaleDays)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data.Stale = staleRows(stale)

	open, err := commands.FindOpenEntries(archiveDir, s.gamesDir, games, th.CloseDays, th.CloseAll)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data.Close = open

	for _, g := range games {
		if g.Draft {
			data.Drafts++
		}
	}
	s.render(w, "catchup", data)
}

// catchupSince is the boundary "new since last time" is measured from: the
// run before the latest one (the latest run is what loaded the new data), or
// a stale threshold back when there's no earlier run to compare against.
func catchupSince(st model.CatchupState, now time.Time) time.Time {
	if t := st.PreviousRunTime(); !t.IsZero() {
		return t
	}
	return now.AddDate(0, 0, -commands.DefaultStaleDays)
}

func daysAgo(t, now time.Time) string {
	switch d := int(now.Sub(t).Hours() / 24); d {
	case 0:
		return "today"
	case 1:
		return "yesterday"
	default:
		return fmt.Sprintf("%d days ago", d)
	}
}

// ── New games (lazy) ─────────────────────────────────────────────────────

type catchupScanData struct {
	Rows       []scanRow
	Notices    []commands.ScanNotice
	Scanned    string
	Error      string
	NumLogged  int
	Thresholds catchupThresholds
	OnDemand   string // label of the provider this fragment was loaded for, if any
}

// handleCatchupScan renders the New games section on its own, fetched by htmx
// once the page is up: it needs every provider's library list (memoized for
// ten minutes — a fresh run has just warmed it), and the rest of the page
// shouldn't wait on six APIs to draw.
func (s *server) handleCatchupScan(w http.ResponseWriter, r *http.Request) {
	th := thresholdsFrom(r.URL.Query().Get)
	data := catchupScanData{Thresholds: th}
	// ?with=psn loads one on-demand provider instead of the default set.
	var only []string
	if w := r.URL.Query().Get("with"); slices.Contains(commands.OnDemandScanProviders, w) {
		only = []string{w}
		data.OnDemand = model.ProviderLabel(w)
	}
	scan, numLogged, err := s.scanCandidates(th.MinHours, commands.ScanModePlayed, func(o *commands.ScanOptions) { o.Only = only })
	if err != nil {
		data.Error = err.Error()
	} else {
		data.Rows = scanRows(scan.Candidates, forms.ScanQuickStatuses, "/scan", th.MinHours, "scan")
		for i := range data.Rows {
			data.Rows[i].From = "catchup"
		}
		data.Notices = scan.Notices
		var scanned []string
		for _, p := range scan.Scanned {
			scanned = append(scanned, model.ProviderLabel(p))
		}
		data.Scanned = strings.Join(scanned, ", ")
		data.NumLogged = numLogged
	}
	s.renderFragment(w, catchupFragmentsCache, "catchup_fragments.html", "catchup_scan", data, catchupFragmentFiles[1:]...)
}

// ── The run ──────────────────────────────────────────────────────────────

type catchupRunOptions struct {
	full      bool            // re-fetch unchanged Steam games too
	providers map[string]bool // the ticked "load" boxes; nothing else is fetched
	minHours  float64
}

// catchupRunProvider is one "load" checkbox on the run form.
type catchupRunProvider struct {
	Key, Label string
	Checked    bool   // ticked by default
	Note       string // shown after the label
}

// catchupRunProviders is every source a run can load, in run order: the six
// achievement providers, BTD6, then Trackmania — unticked by default, since
// Nadeo calls run on a real player account.
func catchupRunProviders() []catchupRunProvider {
	var out []catchupRunProvider
	for _, p := range model.ProviderOrder {
		out = append(out, catchupRunProvider{Key: p, Label: model.ProviderLabel(p), Checked: true})
	}
	return append(out,
		catchupRunProvider{Key: model.ProviderNinjaKiwi, Label: "BTD6", Checked: true},
		catchupRunProvider{Key: model.ProviderNadeo, Label: "Trackmania", Note: "current season; uses your play account"},
	)
}

// RunProviders feeds the run form's checkboxes.
func (catchupData) RunProviders() []catchupRunProvider { return catchupRunProviders() }

// handleCatchupRun starts "Load what's new", or joins the one already running.
func (s *server) handleCatchupRun(w http.ResponseWriter, r *http.Request) {
	s.catchupMu.Lock()
	defer s.catchupMu.Unlock()
	if s.jobs.running(s.catchupJobID) {
		http.Redirect(w, r, "/jobs/"+s.catchupJobID, http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectErr(w, r, housekeepingReturnTo(r), err)
		return
	}
	o := catchupRunOptions{
		full:      r.FormValue("full") == "1",
		providers: map[string]bool{},
		minHours:  atoiFloatOr(r.FormValue("min_hours"), commands.DefaultMinHours),
	}
	for _, p := range r.Form["load"] {
		o.providers[p] = true
	}
	back := housekeepingReturnTo(r)
	// A run with nothing ticked would still be recorded, moving the "since"
	// boundary past activity that was never loaded.
	if len(o.providers) == 0 {
		redirectErr(w, r, back, fmt.Errorf("tick at least one provider to load"))
		return
	}
	id, j := s.jobs.create("Catch up — loading what's new", back, "Back to catch up")
	j.Nav, j.DoneHref = "catchup", back
	s.catchupJobID = id
	s.catchupNotes = nil
	go s.runCatchupJob(j, o)
	http.Redirect(w, r, "/jobs/"+id, http.StatusSeeOther)
}

// runCatchupJob is the whole "what's changed since last time" fetch, in the
// order the page reads it back: the new-games scan (warming the cache the
// page's New games section reads), every linked game's achievements, BTD6,
// and Trackmania when ticked. Every write is the same merge-only archive
// write the per-game buttons make; nothing touches playthroughs.yaml.
func (s *server) runCatchupJob(j *job, o catchupRunOptions) {
	start := time.Now()
	ctx := context.Background()
	archiveDir := model.FindArchiveDir(s.gamesDir)
	games, err := model.ListGames(s.gamesDir)
	if err != nil {
		j.finish(err)
		return
	}
	creds := commands.LoadCredentials()

	var withLinks []model.GameSummary
	var btd6, tm bool
	for _, g := range games {
		if slices.ContainsFunc(g.ProviderLinks(), func(l model.ProviderLink) bool { return o.providers[l.Provider] }) {
			withLinks = append(withLinks, g)
		}
		btd6 = btd6 || g.NinjaKiwiUserID != ""
		tm = tm || g.NadeoAccountID != ""
	}
	btd6 = btd6 && o.providers[model.ProviderNinjaKiwi] && creds.OAKConfigured()
	tm = tm && o.providers[model.ProviderNadeo] && creds.NadeoConfigured()
	// The scan covers only the default (not on-demand) providers, as before,
	// and of those only the ticked ones.
	var scanOnly []string
	for _, p := range model.ProviderOrder {
		if o.providers[p] && !slices.Contains(commands.OnDemandScanProviders, p) {
			scanOnly = append(scanOnly, p)
		}
	}
	total := 1 + len(withLinks)
	if btd6 {
		total++
	}
	if tm {
		total++
	}
	var notes []string
	note := func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		notes = append(notes, msg)
		j.log("  %s", msg)
	}

	j.progress(1, total, "new games")
	j.log("New games")
	commands.ForgetScanCache(creds)
	if len(scanOnly) == 0 {
		j.log("  skipped (no scanned provider ticked)")
	} else if scan, _, err := s.scanCandidates(o.minHours, commands.ScanModePlayed, func(so *commands.ScanOptions) { so.Only = scanOnly }); err != nil {
		note("scan failed: %v", err)
	} else {
		j.log("  %d unlogged or unlinked", len(scan.Candidates))
		for _, n := range scan.Notices {
			j.log("  %s", n)
		}
	}

	j.log("")
	j.log("Achievements")
	c := s.refreshAchievements(j, withLinks, refreshOptions{full: o.full, only: o.providers, quiet: true, base: 1, total: total})
	j.log("  %d games checked, %d changed, %d failed", c.refreshed, c.changed, c.failed)

	done := 1 + len(withLinks)
	if btd6 {
		done++
		j.progress(done, total, "BTD6")
		j.log("")
		j.log("BTD6")
		res, err := commands.SaveNinjaKiwi(ctx, s.gamesDir)
		switch {
		case err != nil:
			note("BTD6: %v", err)
		case res.LastError != "":
			note("BTD6: %s (existing data kept)", res.LastError)
		default:
			j.log("  %s", commands.DescribeNinjaKiwiRecord(res.Stored))
		}
	}
	if tm {
		done++
		j.progress(done, total, "Trackmania")
		j.log("")
		j.log("Trackmania")
		res, err := commands.SaveNadeo(ctx, s.gamesDir, nadeo.FetchOptions{}, "")
		switch {
		case err != nil:
			note("Trackmania: %v", err)
		case res.LastError != "":
			note("Trackmania: %s (existing data kept)", res.LastError)
		default:
			j.log("  %s", commands.DescribeNadeoRecord(res.Stored))
		}
	}

	st, err := model.LoadCatchup(archiveDir)
	if err == nil {
		st.RecordRun(start)
		err = model.SaveCatchup(archiveDir, st)
	}
	if err != nil {
		note("could not record this run: %v", err)
	}

	s.catchupMu.Lock()
	s.catchupNotes = notes
	s.catchupMu.Unlock()
	j.progress(total, total, "")
	j.finish(nil)
}

// ── Acting on an activity card ───────────────────────────────────────────

// activityFor recomputes one game's activity item from disk — what a card's
// submit is checked against, and what an errored card is re-rendered from.
// nil when the game has nothing unlogged (any more).
func (s *server) activityFor(slug string, th catchupThresholds) (*commands.ActivityItem, model.CatchupState, error) {
	archiveDir := model.FindArchiveDir(s.gamesDir)
	st, err := model.LoadCatchup(archiveDir)
	if err != nil {
		return nil, st, err
	}
	doc, pf, err := s.loadGame(slug)
	if err != nil {
		return nil, st, err
	}
	g := model.GameSummaryFor(slug, doc.Path, doc, pf)
	now := time.Now()
	rep, err := commands.FindUnloggedActivity(archiveDir, []model.GameSummary{g}, commands.ActivityOptions{
		Reviewed: st, Since: catchupSince(st, now), StaleDays: th.StaleDays, Now: now,
	})
	if err != nil || len(rep.Items) == 0 {
		return nil, st, err
	}
	return &rep.Items[0], st, nil
}

// handleCatchupLog logs a card's activity as a new playthrough or as a session
// on an existing one, through the same helpers (and so the same validation
// and pre-write loss check) as the game page's own forms. Success swaps the
// card for a one-line result; an error re-renders the card with what was
// typed, so nothing has to be retyped.
func (s *server) handleCatchupLog(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	th := thresholdsFrom(r.FormValue)
	doc, pf, err := s.loadGame(slug)
	if err != nil {
		s.respondHousekeepingErr(w, r, "/", err)
		return
	}
	target := r.FormValue("target")
	started, finished, status, notes := r.FormValue("started"), r.FormValue("finished"), r.FormValue("status"), r.FormValue("notes")
	beaten := strings.TrimSpace(r.FormValue("beaten"))

	var msg string
	if target == "new" {
		err = logNewPlaythrough(doc, pf, model.PlaythroughFields{Started: started, Finished: finished, Beaten: beaten, Status: status, Notes: notes})
		msg = fmt.Sprintf("%s: logged a %s playthrough from %s", doc.FM.Title, status, started)
	} else {
		idx, aerr := strconv.Atoi(target)
		if aerr != nil {
			idx = -1
		}
		err = logSession(pf, idx, started, finished, "")
		msg = fmt.Sprintf("%s: logged a session on #%d from %s", doc.FM.Title, idx+1, started)
	}
	if err != nil {
		if !isHTMX(r) {
			redirectErr(w, r, housekeepingReturnTo(r), err)
			return
		}
		it, _, ierr := s.activityFor(slug, th)
		if ierr != nil || it == nil {
			s.respondHousekeepingErr(w, r, "/", err)
			return
		}
		c := newActivityCard(*it, th)
		c.Target, c.Started, c.Finished, c.Status, c.Notes, c.Error = target, started, finished, status, notes, err.Error()
		c.Beaten = beaten
		c.setTargets()
		s.renderFragment(w, catchupFragmentsCache, "catchup_fragments.html", "activity_card", c, catchupFragmentFiles[1:]...)
		return
	}
	if finished != "" {
		msg += " to " + finished
	}
	msg += "."
	if it, _, _ := s.activityFor(slug, th); it != nil {
		msg += fmt.Sprintf(" %d more unlogged day(s) remain — they'll be on the next load.", it.Days)
	}
	s.respondHousekeepingOK(w, r, housekeepingReturnTo(r), msg)
}

// handleCatchupDismiss marks a card's activity reviewed: each provider record
// on it, through its newest activity day as of now. Newer activity on the same
// record comes back on its own; nothing in playthroughs.yaml or the archive is
// touched.
func (s *server) handleCatchupDismiss(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	backTo := housekeepingReturnTo(r)
	it, st, err := s.activityFor(slug, thresholdsFrom(r.FormValue))
	if err != nil {
		s.respondHousekeepingErr(w, r, backTo, err)
		return
	}
	if it == nil {
		s.respondHousekeepingOK(w, r, backTo, "Nothing left to dismiss there.")
		return
	}
	var entries []model.ReviewedActivity
	through := ""
	for _, p := range it.Providers {
		entries = append(entries, model.ReviewedActivity{Provider: p.Provider, ID: p.ID, Title: it.Game.Title, Through: p.Latest})
		through = max(through, p.Latest)
	}
	st.AddReviewed(entries...)
	if err := model.SaveCatchup(model.FindArchiveDir(s.gamesDir), st); err != nil {
		s.respondHousekeepingErr(w, r, backTo, err)
		return
	}
	s.respondHousekeepingOK(w, r, backTo, fmt.Sprintf("%s: dismissed activity through %s. Anything newer will show up again.", it.Game.Title, through))
}
