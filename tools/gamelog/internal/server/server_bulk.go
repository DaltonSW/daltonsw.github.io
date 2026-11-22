package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"go.dalton.dog/gamelog/internal/commands"
	"go.dalton.dog/gamelog/internal/forms"
	"go.dalton.dog/gamelog/internal/model"
	"go.dalton.dog/gamelog/internal/mutate"
)

// housekeepingReturnTo rebuilds the page URL from whichever filter fields —
// and which tab — the posting form carried as hidden inputs, so an action's
// redirect lands back on the same view the user was looking at rather than
// the page's bare defaults (see the forms in housekeeping.html and
// catchup.html: every POST form there carries these alongside its own fields
// for exactly this reason). A form carrying from=catchup came from Catch up
// (/), which shares the scan, stale and close handlers with Housekeeping.
func housekeepingReturnTo(r *http.Request) string {
	base, keys := "/housekeeping", []string{"min_hours", "incomplete_pct", "incomplete_all", "tab"}
	if r.FormValue("from") == "catchup" {
		base, keys = "/", []string{"min_hours", "stale_days", "close_days", "close_all"}
	}
	v := url.Values{}
	for _, key := range keys {
		if val := r.FormValue(key); val != "" {
			v.Set(key, val)
		}
	}
	if len(v) == 0 {
		return base
	}
	return base + "?" + v.Encode()
}

// ── Scan (rows render on Catch up; Backlog's on Housekeeping) ─────────────

// scanCandidates runs commands.Scan over every configured provider. Both GET
// and POST need it (POST re-derives it to match a submitted row back to a real
// candidate); the provider lists themselves are memoized for a few minutes
// (see commands.cachedList), so a page of row-by-row clicks doesn't re-hit
// six APIs per click, while which games are already covered is re-derived
// from content/games every time.
//
// mode picks which half of the Steam library is wanted; the backlog half is
// Steam alone — see commands.Scan.
func (s *server) scanCandidates(minHours float64, mode commands.ScanMode, opts ...func(*commands.ScanOptions)) (commands.ScanResult, int, error) {
	existing, err := model.ListGames(s.gamesDir)
	if err != nil {
		return commands.ScanResult{}, 0, err
	}
	creds := commands.LoadCredentials()
	if mode == commands.ScanModeBacklog && !creds.SteamConfigured() {
		return commands.ScanResult{}, len(existing), fmt.Errorf("Steam credentials not configured; see tools/gamelog/README.md")
	}
	ignored, err := model.LoadIgnored(model.FindArchiveDir(s.gamesDir))
	if err != nil {
		return commands.ScanResult{}, len(existing), err
	}
	index := commands.NewLoggedIndex(existing).WithIgnored(ignored)
	so := commands.ScanOptions{MinHours: minHours, Mode: mode}
	for _, o := range opts {
		o(&so)
	}
	res, err := commands.Scan(context.Background(), creds, index, so)
	return res, len(existing), err
}

func (s *server) handleScanCreate(w http.ResponseWriter, r *http.Request) {
	s.createFromScan(w, r, commands.ScanModePlayed)
}

func (s *server) handleBacklogCreate(w http.ResponseWriter, r *http.Request) {
	s.createFromScan(w, r, commands.ScanModeBacklog)
}

// createFromScan backs both create buttons. The scan is re-derived here (with
// the same min_hours *and* mode) and the submitted rows are matched into it by
// provider+id, not by position: the rows on the page can outlive the scan they
// came from — htmx swaps only the acted row, so the rest keep rendering while
// each create/ignore quietly shortens the next scan — and a positional index
// would then point at the wrong game.
func (s *server) createFromScan(w http.ResponseWriter, r *http.Request, mode commands.ScanMode) {
	if err := r.ParseForm(); err != nil {
		redirectErr(w, r, "/housekeeping", err)
		return
	}
	backTo := housekeepingReturnTo(r)
	minHours := atoiFloatOr(r.FormValue("min_hours"), commands.DefaultMinHours)
	// Rows from an on-demand provider were loaded by its button, so the
	// re-derived scan has to include it to find them again.
	var extra []string
	for _, pk := range r.Form["provider"] {
		if slices.Contains(commands.OnDemandScanProviders, pk) {
			extra = append(extra, pk)
		}
	}
	scan, _, err := s.scanCandidates(minHours, mode, func(o *commands.ScanOptions) { o.Include = extra })
	if err != nil {
		s.respondHousekeepingErr(w, r, backTo, err)
		return
	}
	candidates := scan.Candidates
	providers, ids := r.Form["provider"], r.Form["id"]
	var chosen []int
	var stale bool
	for i, pk := range providers {
		if i >= len(ids) {
			break
		}
		idx := -1
		for j, c := range candidates {
			if c.ProviderKey() == pk && c.ID == ids[i] {
				idx = j
				break
			}
		}
		if idx < 0 {
			stale = true
			continue
		}
		chosen = append(chosen, idx)
	}
	if len(chosen) == 0 {
		if stale {
			s.respondHousekeepingOK(w, r, backTo, "That game is no longer in the scan — it may already be logged or ignored. Reload to refresh the list.")
			return
		}
		s.respondHousekeepingOK(w, r, backTo, "Nothing created.")
		return
	}

	// The status dropdown submits its value whichever button sent the form,
	// so it only counts when its own "Create as" button was the one clicked.
	// Without that check the primary button — "create the status I guessed" —
	// would silently create whatever the dropdown was left showing.
	var status string
	if r.FormValue("use_status") != "" {
		status = r.FormValue("status")
		// Validated because it goes straight into front matter, and a
		// submitted form value is not a trusted one.
		if !forms.IsGameStatus(status) {
			s.respondHousekeepingErr(w, r, backTo, fmt.Errorf("unknown status %q", status))
			return
		}
	}

	created, skipped := commands.CreateFromCandidates(s.gamesDir, candidates, chosen, status, commands.LoadCredentials())
	kind := "created"
	if status != "" {
		kind = "created as " + status
	} else if mode == commands.ScanModeBacklog {
		kind = "created as backlog"
	}

	// A row's button only ever creates the one game it belongs to, so the
	// common result reads as a sentence about that game rather than a count
	// with a "0 skipped" tail. The count form is kept for the (currently
	// unreachable from the UI) case of more than one candidate in a post.
	var msg string
	switch {
	case len(created) == 1 && skipped == 0:
		msg = fmt.Sprintf("%s %s — finish it via the games list's \"drafts only\" filter.", created[0].Title, kind)
	case len(created) == 0:
		msg = fmt.Sprintf("Nothing created (%d skipped).", skipped)
	default:
		msg = fmt.Sprintf("%d %s, %d skipped. Find them via the games list's \"drafts only\" filter to finish them.", len(created), kind, skipped)
	}
	s.respondHousekeepingOK(w, r, backTo, msg)
}

// handleScanLink links a scanned game to the logged game sharing its title —
// the same game found again on another service. Like ignore and attach it
// posts ids rather than a row index, and commands.LinkToGame refuses to
// overwrite an id either side already holds.
func (s *server) handleScanLink(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectErr(w, r, "/housekeeping", err)
		return
	}
	backTo := housekeepingReturnTo(r)
	provider, id, slug := r.FormValue("provider"), r.FormValue("id"), r.FormValue("slug")
	if provider == "" || id == "" || slug == "" {
		s.respondHousekeepingErr(w, r, backTo, fmt.Errorf("linking needs a provider, an id and a game"))
		return
	}
	title, err := commands.LinkToGame(r.Context(), s.gamesDir, slug, provider, id, commands.LoadCredentials())
	name := model.FirstNonEmpty(title, slug)
	label := model.ProviderLabel(provider)
	switch {
	case commands.IsAlreadyLinked(err):
		s.respondHousekeepingOK(w, r, backTo, fmt.Sprintf("%s was already linked to %s %s.", name, label, id))
	case err != nil:
		s.respondHousekeepingErr(w, r, backTo, err)
	default:
		s.respondHousekeepingOK(w, r, backTo, fmt.Sprintf("%s linked to %s %s — its history is captured and on the game's page.", name, label, id))
	}
}

// handleSubsetAttach links a RetroAchievements subset to the game it belongs
// to, instead of creating a second entry for a game that already has one.
//
// Like handleIgnore — and unlike the create buttons — it posts ids rather than
// a row index: attaching is a durable decision keyed by ids that never change,
// so it shouldn't depend on the scan re-deriving identically. commands
// re-checks the parent link against the target game before writing anything,
// so a stale or hand-edited form can't attach a subset to the wrong game.
func (s *server) handleSubsetAttach(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectErr(w, r, "/housekeeping", err)
		return
	}
	backTo := housekeepingReturnTo(r)
	subsetID, slug := r.FormValue("id"), r.FormValue("slug")
	if subsetID == "" || slug == "" {
		s.respondHousekeepingErr(w, r, backTo, fmt.Errorf("attaching a subset needs an id and a game"))
		return
	}
	creds := commands.LoadCredentials()
	title, err := commands.AttachSubset(r.Context(), s.gamesDir, slug, subsetID, commands.RAClientFor(creds), creds)
	name := model.FirstNonEmpty(title, r.FormValue("title"), subsetID)
	switch {
	case commands.IsAlreadyAttached(err):
		s.respondHousekeepingOK(w, r, backTo, fmt.Sprintf("%s was already attached to %s.", name, slug))
	case err != nil:
		s.respondHousekeepingErr(w, r, backTo, err)
	default:
		s.respondHousekeepingOK(w, r, backTo, fmt.Sprintf("%s attached to %s — its achievements now show on that game's page.", name, slug))
	}
}

// handleSubsetCreateBase is the same action for a subset whose base game isn't
// logged at all: create the base game from what RetroAchievements reports
// about it (a draft, like everything the scan creates), then attach.
func (s *server) handleSubsetCreateBase(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectErr(w, r, "/housekeeping", err)
		return
	}
	backTo := housekeepingReturnTo(r)
	subsetID := r.FormValue("id")
	if subsetID == "" {
		s.respondHousekeepingErr(w, r, backTo, fmt.Errorf("creating a base game needs the subset's id"))
		return
	}
	creds := commands.LoadCredentials()
	slug, baseTitle, err := commands.CreateBaseAndAttach(r.Context(), s.gamesDir, subsetID, commands.RAClientFor(creds), creds)
	if err != nil {
		s.respondHousekeepingErr(w, r, backTo, err)
		return
	}
	s.respondHousekeepingOK(w, r, backTo, fmt.Sprintf("Created %s as a draft (%s) with its subset attached. Review it via the games list's \"drafts only\" filter.", baseTitle, slug))
}

// handleIgnore takes a scan candidate off the list for good. Unlike the
// create handlers this posts provider/id/title directly rather than an index
// into a re-derived scan: an ignore is a durable decision keyed by an ID that
// never changes, so it shouldn't depend on the scan coming back identical.
func (s *server) handleIgnore(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectErr(w, r, "/housekeeping", err)
		return
	}
	backTo := housekeepingReturnTo(r)
	g := model.IgnoredGame{
		Provider: r.FormValue("provider"),
		ID:       r.FormValue("id"),
		Title:    r.FormValue("title"),
		Reason:   r.FormValue("reason"),
	}
	if g.Provider == "" || g.ID == "" {
		s.respondHousekeepingErr(w, r, backTo, fmt.Errorf("ignore needs a provider and an id"))
		return
	}
	added, err := model.AddIgnored(model.FindArchiveDir(s.gamesDir), g)
	if err != nil {
		s.respondHousekeepingErr(w, r, backTo, err)
		return
	}
	name := model.FirstNonEmpty(g.Title, g.Provider+" "+g.ID)
	if !added {
		s.respondHousekeepingOK(w, r, backTo, name+" was already ignored.")
		return
	}
	s.respondHousekeepingOK(w, r, backTo, name+" ignored — it won't show up in scans again.")
}

// handleUnignore is the undo. Nothing was destroyed by ignoring, so this just
// puts the game back in front of the next scan.
func (s *server) handleUnignore(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectErr(w, r, "/housekeeping", err)
		return
	}
	backTo := housekeepingReturnTo(r)
	provider, id := r.FormValue("provider"), r.FormValue("id")
	removed, err := model.RemoveIgnored(model.FindArchiveDir(s.gamesDir), provider, id)
	if err != nil {
		s.respondHousekeepingErr(w, r, backTo, err)
		return
	}
	if !removed {
		s.respondHousekeepingOK(w, r, backTo, "Nothing to un-ignore.")
		return
	}
	s.respondHousekeepingOK(w, r, backTo, model.FirstNonEmpty(r.FormValue("title"), id)+" is back in the scan list.")
}

func atoiFloatOr(s string, def float64) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return def
	}
	return f
}

// ── Housekeeping (backlog + ignored + incomplete + achievements) ──────────
//
// The whole-library lists that aren't a "what changed" queue: owned-but-
// unplayed games, the ignore list, achievement completion, and the manual
// refresh/regenerate tools. Scan, Stale and Close render on Catch up (/)
// instead, but their row handlers live here and serve both pages.

type staleRow struct {
	C             commands.StaleCandidate
	Card          string
	OtherStatuses []string
}

// scanRow is staleRow's counterpart for a scan/backlog candidate, and follows
// the same rule the stale section established: the guess is the primary
// button, every other plausible answer is one click beside it, and nothing
// forces a create-then-edit round trip to fix a wrong guess.
type scanRow struct {
	I             int // index into the scan this row came from
	C             commands.Candidate
	OtherStatuses []string

	// Action, MinHours, Tab and From are carried per row rather than read off
	// the page data, because the button block is a nested template: inside a
	// {{define}} the enclosing page's dot isn't reachable. Tab is which panel
	// this row's form should redirect back into, and From which page (see
	// housekeepingReturnTo).
	Action   string
	MinHours float64
	Tab      string
	From     string
}

// scanRows pairs each candidate with the alternatives to its guessed status.
// quick is the mode's plausible set — see forms.ScanQuickStatuses.
func scanRows(candidates []commands.Candidate, quick []string, action string, minHours float64, tab string) []scanRow {
	rows := make([]scanRow, len(candidates))
	for i, c := range candidates {
		// A link row's primary button links rather than creating its
		// guessed status, so that status belongs in the dropdown too.
		var others []string
		for _, st := range quick {
			if st != c.Status || c.IsLink() {
				others = append(others, st)
			}
		}
		rows[i] = scanRow{I: i, C: c, OtherStatuses: others, Action: action, MinHours: minHours, Tab: tab}
	}
	return rows
}

type housekeepingData struct {
	Page
	MinHours float64
	// Backlog* is the Steam scan against the low side of MinHours — the
	// other side of Catch up's New games list. They share min_hours on
	// purpose: one boundary, two complementary lists, so nothing can appear
	// in both.
	BacklogCandidates []scanRow
	BacklogError      string
	BacklogNotices    []commands.ScanNotice
	Incomplete        []commands.IncompleteCandidate
	IncompletePct     float64
	IncompleteAll     bool
	// Ignored are the games deliberately kept out of New games and Backlog.
	// Shown so the decision stays visible and reversible from the same page
	// it was made on.
	Ignored []model.IgnoredGame
	// ActiveTab is which of the page's radio-driven tabs should render
	// checked, so a redirect back from a POST action (see
	// housekeepingReturnTo) reopens on the tab the user was on rather than
	// always snapping back to Backlog.
	ActiveTab string
}

// housekeepingTabs are the valid values for the "tab" query/form param —
// the ids on housekeeping.html's tab radios, minus their "tab-" prefix.
var housekeepingTabs = []string{"backlog", "ignored", "incomplete", "achievements"}

func (s *server) handleHousekeeping(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	minHours := float64(commands.DefaultMinHours)
	if v := q.Get("min_hours"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			minHours = f
		}
	}
	incompletePct := float64(commands.DefaultIncompletePct)
	if v := q.Get("incomplete_pct"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			incompletePct = f
		}
	}
	incompleteAll := q.Get("incomplete_all") == "1"
	activeTab := "backlog"
	if v := q.Get("tab"); slices.Contains(housekeepingTabs, v) {
		activeTab = v
	}

	data := housekeepingData{
		Page:          newPage(r, "Housekeeping", "housekeeping"),
		MinHours:      minHours,
		IncompletePct: incompletePct,
		IncompleteAll: incompleteAll,
		ActiveTab:     activeTab,
	}

	// Re-scan asks for Steam's list fresh, not the memoized one — that's how
	// something bought in the last few minutes shows up.
	if q.Get("refresh") == "1" {
		commands.ForgetScanCache(commands.LoadCredentials())
	}
	backlog, _, err := s.scanCandidates(minHours, commands.ScanModeBacklog)
	if err != nil {
		data.BacklogError = err.Error()
	} else {
		data.BacklogCandidates = scanRows(backlog.Candidates, forms.BacklogQuickStatuses, "/backlog", minHours, "backlog")
		data.BacklogNotices = backlog.Notices
	}

	if ignored, err := model.LoadIgnored(model.FindArchiveDir(s.gamesDir)); err == nil {
		data.Ignored = ignored.Games
	}

	games, err := model.ListGames(s.gamesDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Reads the archive on disk only — no provider calls, so this section
	// renders even with no credentials configured at all.
	incomplete, err := commands.FindIncomplete(model.FindArchiveDir(s.gamesDir), games, commands.IncompleteOptions{
		MinPct:      incompletePct,
		IncludeDone: incompleteAll,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data.Incomplete = incomplete

	s.render(w, "housekeeping", data)
}

// staleRows pairs each stale candidate with its rendered card and the
// statuses it didn't guess, for the one-click corrections beside the guess.
func staleRows(candidates []commands.StaleCandidate) []staleRow {
	rows := make([]staleRow, len(candidates))
	for i, c := range candidates {
		var others []string
		for _, st := range forms.StaleQuickStatuses {
			if st != c.SuggestedStatus {
				others = append(others, st)
			}
		}
		rows[i] = staleRow{C: c, Card: commands.FormatStaleCard(c), OtherStatuses: others}
	}
	return rows
}

func (s *server) handleStaleAction(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	days := atoiOr(r.FormValue("stale_days"), commands.DefaultStaleDays)
	backTo := housekeepingReturnTo(r)

	games, err := model.ListGames(s.gamesDir)
	if err != nil {
		s.respondHousekeepingErr(w, r, backTo, err)
		return
	}
	candidates, err := commands.FindStaleCandidates(model.FindArchiveDir(s.gamesDir), games, days)
	if err != nil {
		s.respondHousekeepingErr(w, r, backTo, err)
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
		s.respondHousekeepingErr(w, r, backTo, fmt.Errorf("%s is no longer a stale candidate", slug))
		return
	}

	doc, pf, ok := s.gameOr404(w, r, slug)
	if !ok {
		return
	}
	action := r.FormValue("action")
	if action != forms.StaleAccept && !isStaleMarkAction(action) {
		s.respondHousekeepingErr(w, r, backTo, fmt.Errorf("unknown action %q", action))
		return
	}
	if commands.ApplyStaleAction(doc, pf, *c, action) {
		err = mutate.WriteEntry(pf) // the game's status follows via SyncGameStatus
	} else {
		err = mutate.WriteFrontMatter(doc)
	}
	if err != nil {
		s.respondHousekeepingErr(w, r, backTo, err)
		return
	}
	s.respondHousekeepingOK(w, r, backTo, doc.FM.Title+": saved.")
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
	backTo := housekeepingReturnTo(r)

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

	// Match the ticked rows by key against the list as it stands now, never by
	// position (see OpenEntry.Key). Re-deriving it also keeps its filters in
	// force: a row that was closed elsewhere, paused, or dropped under the
	// threshold since the page rendered is skipped, not written.
	byKey := make(map[string]commands.OpenEntry, len(open))
	for _, e := range open {
		byKey[e.Key()] = e
	}
	selected := r.Form["entry"]
	closed, skipped := 0, 0
	var failures []string
	for _, key := range selected {
		e, ok := byKey[key]
		if !ok {
			skipped++
			continue
		}
		did, err := commands.CloseEntry(e)
		switch {
		case err != nil:
			failures = append(failures, fmt.Sprintf("%s: %v", e.Game.Title, err))
		case did:
			closed++
		default:
			skipped++
		}
	}
	msg := fmt.Sprintf("Closed %d of %d selected.", closed, len(selected))
	if skipped > 0 {
		msg += fmt.Sprintf(" %d no longer open, skipped.", skipped)
	}
	if len(failures) > 0 {
		redirectErr(w, r, backTo, fmt.Errorf("%s Failed: %s", msg, strings.Join(failures, "; ")))
		return
	}
	redirectOK(w, r, backTo, msg)
}
