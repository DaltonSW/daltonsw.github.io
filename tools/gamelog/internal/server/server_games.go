package server

import (
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"go.dalton.dog/gamelog/internal/commands"
	"go.dalton.dog/gamelog/internal/externalid"
	"go.dalton.dog/gamelog/internal/forms"
	"go.dalton.dog/gamelog/internal/model"
	"go.dalton.dog/gamelog/internal/mutate"
)

// loadGame loads a game's Doc and PlaythroughsFile by slug, the web
// equivalent of what the TUI's SelectGame + logForGame do together.
func (s *server) loadGame(slug string) (*model.Doc, *model.PlaythroughsFile, error) {
	path := filepath.Join(s.gamesDir, slug, "_index.md")
	doc, err := model.LoadDoc(path)
	if err != nil {
		return nil, nil, err
	}
	pf, err := model.LoadPlaythroughs(filepath.Dir(path))
	if err != nil {
		return nil, nil, err
	}
	return doc, pf, nil
}

// gameOr404 loads a game or writes a 404, returning ok=false when the
// handler should stop.
func (s *server) gameOr404(w http.ResponseWriter, r *http.Request, slug string) (*model.Doc, *model.PlaythroughsFile, bool) {
	doc, pf, err := s.loadGame(slug)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return nil, nil, false
	}
	return doc, pf, true
}

func gamePath(slug string) string { return "/games/" + slug }

// ── Games list / create ────────────────────────────────────────────────────

// gameRowView is what game_row.html renders: a GameSummary plus the one bit
// of state that only exists mid-edit — a quickedit validation error, shown
// inline with the values the user actually typed rather than reverting them.
type gameRowView struct {
	model.GameSummary
	Error string
	Saved bool // true right after a successful quickedit, for a one-shot CSS flash
}

type indexData struct {
	Page
	Games      []gameRowView
	Query      string
	DraftsOnly bool
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	games, err := model.ListGames(s.gamesDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q != "" {
		lower := strings.ToLower(q)
		var filtered []model.GameSummary
		for _, g := range games {
			if strings.Contains(strings.ToLower(g.Title), lower) {
				filtered = append(filtered, g)
			}
		}
		games = filtered
	}
	draftsOnly := r.URL.Query().Get("drafts") == "1"
	if draftsOnly {
		var filtered []model.GameSummary
		for _, g := range games {
			if g.Draft {
				filtered = append(filtered, g)
			}
		}
		games = filtered
	}
	rows := make([]gameRowView, len(games))
	for i, g := range games {
		rows[i] = gameRowView{GameSummary: g}
	}
	s.render(w, "index", indexData{Page: newPage(r, "Games", "games"), Games: rows, Query: q, DraftsOnly: draftsOnly})
}

type gameNewData struct {
	Page
	model.NewGameFields
}

func (s *server) handleNewGameForm(w http.ResponseWriter, r *http.Request) {
	data := gameNewData{
		Page:          newPage(r, "New game", "games"),
		NewGameFields: model.NewGameFields{Status: "playing", Started: forms.Today()},
	}
	s.render(w, "game_new", data)
}

func (s *server) handleCreateGame(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f := model.NewGameFields{
		Title:    strings.TrimSpace(r.FormValue("title")),
		Platform: r.FormValue("platform"),
		Status:   r.FormValue("status"),
		Subgames: forms.ParseSubgames(r.FormValue("subgames")),
		Started:  r.FormValue("started"),
		Finished: r.FormValue("finished"),
		Rating:   r.FormValue("rating"),
		Overview: r.FormValue("overview"),
	}

	fail := func(msg string) {
		data := gameNewData{Page: newPage(r, "New game", "games"), NewGameFields: f}
		data.Flash.Error = msg
		s.render(w, "game_new", data)
	}

	if f.Title == "" {
		fail("title is required")
		return
	}
	raID, err := externalid.ParseExternalID(r.FormValue("ra_id"))
	if err != nil {
		fail("RetroAchievements ID: " + err.Error())
		return
	}
	steamID, err := externalid.ParseExternalID(r.FormValue("steam_id"))
	if err != nil {
		fail("Steam appid: " + err.Error())
		return
	}
	if err := forms.ValidateDate(false)(f.Started); err != nil {
		fail("started: " + err.Error())
		return
	}
	if err := forms.ValidateDate(false)(f.Finished); err != nil {
		fail("finished: " + err.Error())
		return
	}
	if err := forms.ValidateRating(f.Rating); err != nil {
		fail("rating: " + err.Error())
		return
	}
	f.RetroAchievementsID, f.SteamAppID = raID, steamID

	slug := model.Slugify(f.Title)
	path, err := model.CreateGameFile(s.gamesDir, slug, f)
	if err != nil {
		fail(err.Error())
		return
	}
	fmt.Printf("Created %s\n", path)
	redirectOK(w, r, gamePath(slug), "Created.")
}

// ── Game detail ─────────────────────────────────────────────────────────────

// pthUpdateView is what playthrough_header/sessions_table render: a
// Playthrough view plus the context those fragments need (Slug/GamePlatform
// aren't on model.Playthrough itself) and the Error/Saved pair gameRowView
// established for inline feedback without reverting the user's typed values.
type pthUpdateView struct {
	model.Playthrough
	DisplayFinished string
	Slug            string
	GamePlatform    string
	GameSubgames    []string
	SessionRows     []sessionRowView
	Error           string
	Saved           bool
}

// sessionRowView is one row of a playthrough's sessions table.
type sessionRowView struct {
	model.Session
	Slug        string
	PthIndex    int
	NumSessions int
	Error       string
	Saved       bool
}

// playthroughsSectionView is the whole "Playthroughs" section — the fragment
// handleSplitPlaythrough re-renders, since splitting changes the entry count
// and so can't stay scoped to one playthrough's own fragment.
type playthroughsSectionView struct {
	Slug         string
	Playthroughs []pthUpdateView
	Error        string
}

// buildSessionRows renders a playthrough's sessions table rows. An entry
// that predates sessions entirely (a flat started/finished pair, never
// converted) is shown as an implicit session 1 rather than an empty table —
// EditSession and sessionIndices know how to convert it in place if that row
// is ever saved.
func buildSessionRows(slug string, p model.Playthrough) []sessionRowView {
	if !p.HasSessions() {
		if p.Started == "" {
			return nil
		}
		return []sessionRowView{{
			Session:     model.Session{Started: p.Started, Finished: p.Finished},
			Slug:        slug,
			PthIndex:    p.Index,
			NumSessions: 1,
		}}
	}
	rows := make([]sessionRowView, len(p.Sessions))
	for i, sess := range p.Sessions {
		rows[i] = sessionRowView{Session: sess, Slug: slug, PthIndex: p.Index, NumSessions: len(p.Sessions)}
	}
	return rows
}

func buildPthUpdateView(slug, gamePlatform string, gameSubgames []string, p model.Playthrough) pthUpdateView {
	displayFinished := p.Finished
	if p.HasSessions() {
		displayFinished = p.Sessions[len(p.Sessions)-1].Finished
	}
	return pthUpdateView{
		Playthrough:     p,
		DisplayFinished: displayFinished,
		Slug:            slug,
		GamePlatform:    gamePlatform,
		GameSubgames:    gameSubgames,
		SessionRows:     buildSessionRows(slug, p),
	}
}

func buildPlaythroughsSectionView(slug, gamePlatform string, gameSubgames []string, pf *model.PlaythroughsFile) playthroughsSectionView {
	var played []pthUpdateView
	for _, p := range pf.Views() {
		if p.Status == "planned" {
			continue
		}
		played = append(played, buildPthUpdateView(slug, gamePlatform, gameSubgames, p))
	}
	return playthroughsSectionView{Slug: slug, Playthroughs: played}
}

type gameDetailData struct {
	Page
	Slug             string
	FM               model.FrontMatter
	RatingStr        string
	HasProviderLink  bool
	Planned          []model.Playthrough
	Playthroughs     playthroughsSectionView
	Today            string
	DefaultPthStatus string
	CanDelete        bool
	// Started/Finished are the fallback-aware display values (playthroughs
	// first, front matter only if nothing's logged); StartedEditable/
	// FinishedEditable gate whether editing the raw front-matter field below
	// would have any effect — see model.GameSummary's doc comment.
	Started          string
	Finished         string
	StartedEditable  bool
	FinishedEditable bool
	Achievements     []model.EarnedAchievement
	// Provider ID fields, rendered as strings regardless of how the YAML
	// scalar underneath decoded — see model.GameSummary's fields of the
	// same name.
	RAGameID   string
	SteamAppID string
	PSNID      string
	UbisoftID  string
	XboxID     string
	// Subsets are the RetroAchievements subsets attached to this game, with
	// the counts the summary keeps separate from the game's own. Read-only
	// here: attaching one is Housekeeping's job, because it needs the parent
	// check only the provider can answer (see commands.AttachSubset).
	Subsets []model.SubsetBreakdown
}

func (s *server) buildGameDetail(r *http.Request, slug string, doc *model.Doc, pf *model.PlaythroughsFile) gameDetailData {
	var planned []model.Playthrough
	for _, p := range pf.Views() {
		if p.Status == "planned" {
			planned = append(planned, p)
		}
	}

	defaultStatus := "playing"
	if doc.FM.Status == "ongoing" || doc.FM.Status == "multiplayer" {
		defaultStatus = doc.FM.Status
	}

	// Same rule as the (now-removed) review flow's delete action: a game
	// with captured playthroughs or archive history isn't a stub, and
	// deleting it would risk real data — see commands.DeleteGameStub.
	g := model.GameSummaryFor(slug, doc.Path, doc, pf)
	canDelete := g.NumPlaythroughs == 0 && !commands.HasArchiveRecord(model.FindArchiveDir(s.gamesDir), g.ProviderLinks())

	var achievements []model.EarnedAchievement
	var subsets []model.SubsetBreakdown
	if summary, err := model.LoadAchievementSummary(doc.GameDir()); err == nil && summary != nil {
		subsets = summary.Subsets
		// Display newest-first; the stored file is oldest-first (see
		// AchievementSummary.Earned's doc comment) since display order is a
		// reader's choice, not the archive's.
		achievements = make([]model.EarnedAchievement, len(summary.Earned))
		for i, a := range summary.Earned {
			achievements[len(summary.Earned)-1-i] = a
		}
	}

	return gameDetailData{
		Page:             newPage(r, doc.FM.Title, "games"),
		Slug:             slug,
		FM:               doc.FM,
		RatingStr:        doc.FM.RatingString(),
		HasProviderLink:  len(doc.ProviderLinks()) > 0,
		Planned:          planned,
		Playthroughs:     buildPlaythroughsSectionView(slug, doc.FM.Platform, doc.FM.Subgames, pf),
		Today:            forms.Today(),
		DefaultPthStatus: defaultStatus,
		CanDelete:        canDelete,
		Started:          g.Started,
		Finished:         g.Finished,
		StartedEditable:  g.StartedEditable,
		FinishedEditable: g.FinishedEditable,
		Achievements:     achievements,
		RAGameID:         g.RAGameID,
		SteamAppID:       g.SteamAppID,
		PSNID:            g.PSNID,
		UbisoftID:        g.UbisoftID,
		XboxID:           g.XboxID,
		Subsets:          subsets,
	}
}

func (s *server) handleGameDetail(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	doc, pf, ok := s.gameOr404(w, r, slug)
	if !ok {
		return
	}
	s.render(w, "game_detail", s.buildGameDetail(r, slug, doc, pf))
}

// handleDeleteGame re-checks CanDelete independently of what the page showed
// (commands.DeleteGameStub does too, as defense in depth) rather than trusting the
// form submission alone — the game's data may have changed since the page
// was loaded.
func (s *server) handleDeleteGame(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	doc, pf, ok := s.gameOr404(w, r, slug)
	if !ok {
		return
	}
	g := model.GameSummaryFor(slug, doc.Path, doc, pf)
	canDelete := g.NumPlaythroughs == 0 && !commands.HasArchiveRecord(model.FindArchiveDir(s.gamesDir), g.ProviderLinks())
	if err := commands.DeleteGameStub(s.gamesDir, g, canDelete); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	redirectOK(w, r, "/", doc.FM.Title+": deleted.")
}

// ── Game info ────────────────────────────────────────────────────────────

func (s *server) handleEditInfo(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	doc, pf, ok := s.gameOr404(w, r, slug)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		redirectErr(w, r, gamePath(slug), fmt.Errorf("title is required"))
		return
	}

	// Started/Finished are only genuinely editable here when the game's
	// GameSummary says so — otherwise playthroughs.yaml already governs
	// them and a value typed here would silently never be displayed. This
	// is the load-bearing check: the template hides/disables these inputs,
	// but a stale page or a hand-crafted POST must still be rejected here.
	g := model.GameSummaryFor(slug, doc.Path, doc, pf)
	started, finished := doc.FM.Started, doc.FM.Finished
	if g.StartedEditable {
		started = r.FormValue("started")
		if err := forms.ValidateDate(false)(started); err != nil {
			redirectErr(w, r, gamePath(slug), err)
			return
		}
	}
	if g.FinishedEditable {
		finished = r.FormValue("finished")
		if err := forms.ValidateDate(false)(finished); err != nil {
			redirectErr(w, r, gamePath(slug), err)
			return
		}
	}
	rating := r.FormValue("rating")
	if err := forms.ValidateRating(rating); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}

	updated := doc.FM
	updated.Title = title
	updated.Platform = r.FormValue("platform")
	updated.Status = r.FormValue("status")
	updated.Subgames = forms.ParseSubgames(r.FormValue("subgames"))
	updated.Started = started
	updated.Finished = finished
	updated.SetRating(rating)
	updated.Draft = r.FormValue("draft") == "1"

	if mutate.SameGameInfo(doc.FM, updated) {
		redirectOK(w, r, gamePath(slug), "No changes.")
		return
	}
	doc.FM = updated

	// Front-matter scalar fields aren't omitempty, so clearing one leaves the
	// key present rather than dropping it — nothing here is ever a legitimate
	// removal to declare, matching doEditGameInfo in main.go.
	syncing := pf.SyncStatus(doc.FM.Status, doc.FM.Finished)
	if err := mutate.WriteFrontMatter(doc, pf, syncing); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	redirectOK(w, r, gamePath(slug), "Saved.")
}

// handleUpdateProviders edits the game's provider IDs — RetroAchievements,
// Steam, PSN, Ubisoft, Xbox — the fields that drive both achievement-fetch
// linking (Doc.ProviderLinks) and the `suggest` picker. Split out from
// handleEditInfo since these fields have their own validation (RA/Steam
// accept a pasted URL or bare ID; PSN/Ubisoft/Xbox are free text since PSN's
// NPWR… ids aren't numeric) and their own field-loss story: PSNID/UbisoftID/
// XboxID are `omitempty`, so clearing one is a real key removal that must be
// declared, unlike every field handleEditInfo touches.
func (s *server) handleUpdateProviders(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	doc, pf, ok := s.gameOr404(w, r, slug)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}

	raID, err := externalid.ParseExternalID(r.FormValue("ra_id"))
	if err != nil {
		redirectErr(w, r, gamePath(slug), fmt.Errorf("RetroAchievements ID: %w", err))
		return
	}
	steamID, err := externalid.ParseExternalID(r.FormValue("steam_id"))
	if err != nil {
		redirectErr(w, r, gamePath(slug), fmt.Errorf("Steam appid: %w", err))
		return
	}
	psnID := strings.TrimSpace(r.FormValue("psn_id"))
	ubisoftID := strings.TrimSpace(r.FormValue("ubisoft_id"))
	xboxID := strings.TrimSpace(r.FormValue("xbox_id"))

	var allowedRemovals []string
	if psnID == "" && doc.FM.PSNID != nil {
		allowedRemovals = append(allowedRemovals, "psn_id")
	}
	if ubisoftID == "" && doc.FM.UbisoftID != nil {
		allowedRemovals = append(allowedRemovals, "ubisoft_id")
	}
	if xboxID == "" && doc.FM.XboxID != nil {
		allowedRemovals = append(allowedRemovals, "xbox_id")
	}

	doc.FM.RetroAchievementsID = model.ScalarFromInput(raID)
	doc.FM.SteamAppID = model.ScalarFromInput(steamID)
	doc.FM.PSNID = model.ScalarFromInput(psnID)
	doc.FM.UbisoftID = model.ScalarFromInput(ubisoftID)
	doc.FM.XboxID = model.ScalarFromInput(xboxID)

	if err := mutate.WriteFrontMatter(doc, pf, false, allowedRemovals...); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	redirectOK(w, r, gamePath(slug), "Saved.")
}

// handleQuickEditGame is the games-index row's Save button: the same
// Status/Started/Finished/Platform/Rating edit as handleEditInfo, minus
// Title/Draft, always answering with the row fragment (game_row.html)
// instead of a redirect — the index page swaps it in with htmx instead of
// reloading, so a sweep down the whole list never has to visit
// /games/{slug} at all. A failed validation re-renders the row with
// whatever was typed (not the saved values) plus an inline error, so a typo
// doesn't cost the rest of the edit.
func (s *server) handleQuickEditGame(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	doc, pf, ok := s.gameOr404(w, r, slug)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	g := model.GameSummaryFor(slug, doc.Path, doc, pf)
	status := r.FormValue("status")
	started, finished := doc.FM.Started, doc.FM.Finished
	if g.StartedEditable {
		started = r.FormValue("started")
	}
	if g.FinishedEditable {
		finished = r.FormValue("finished")
	}
	platform, rating := r.FormValue("platform"), r.FormValue("rating")

	fail := func(err error) {
		row := model.GameSummaryFor(slug, doc.Path, doc, pf)
		row.Status, row.FMStarted, row.FMFinished, row.Platform, row.Rating = status, started, finished, platform, rating
		s.renderRow(w, gameRowView{GameSummary: row, Error: err.Error()})
	}

	if g.StartedEditable {
		if err := forms.ValidateDate(false)(started); err != nil {
			fail(fmt.Errorf("started: %w", err))
			return
		}
	}
	if g.FinishedEditable {
		if err := forms.ValidateDate(false)(finished); err != nil {
			fail(fmt.Errorf("finished: %w", err))
			return
		}
	}
	if err := forms.ValidateRating(rating); err != nil {
		fail(fmt.Errorf("rating: %w", err))
		return
	}

	updated := doc.FM
	updated.Status = status
	updated.Started = started
	updated.Finished = finished
	updated.Platform = platform
	updated.SetRating(rating)

	if !mutate.SameGameInfo(doc.FM, updated) {
		doc.FM = updated
		// Front-matter scalar fields aren't omitempty, so clearing one leaves
		// the key present rather than dropping it — same reasoning as
		// handleEditInfo above: nothing here is ever a legitimate removal to
		// declare.
		syncing := pf.SyncStatus(doc.FM.Status, doc.FM.Finished)
		if err := mutate.WriteFrontMatter(doc, pf, syncing); err != nil {
			fail(err)
			return
		}
	}

	s.renderRow(w, gameRowView{GameSummary: model.GameSummaryFor(slug, doc.Path, doc, pf), Saved: true})
}

// handleUndraftGame is the games-index row's Undraft button — a one-field
// version of handleEditInfo's draft checkbox, for clearing the flag without
// opening the game's own page. Only ever clears the flag; there's no
// corresponding "mark as draft" action here since drafts are set at
// creation, not reinstated.
//
// The row's button includes the index page's "drafts only" checkbox
// (hx-include="#drafts" in game_row.html) so this handler knows whether the
// row it just undrafted still belongs in the list it was clicked from. Under
// that filter, an empty response tells htmx to remove the row outright
// instead of swapping in the updated (now driftless) one — otherwise
// undrafting would leave the game sitting in a "drafts only" view.
func (s *server) handleUndraftGame(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	doc, pf, ok := s.gameOr404(w, r, slug)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	draftsOnly := r.FormValue("drafts") == "1"
	if doc.FM.Draft {
		doc.FM.Draft = false
		if err := mutate.WriteFrontMatter(doc, pf, pf.SyncStatus(doc.FM.Status, doc.FM.Finished)); err != nil {
			s.renderRow(w, gameRowView{GameSummary: model.GameSummaryFor(slug, doc.Path, doc, pf), Error: err.Error()})
			return
		}
	}
	if draftsOnly {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		return
	}
	s.renderRow(w, gameRowView{GameSummary: model.GameSummaryFor(slug, doc.Path, doc, pf), Saved: true})
}

// ── Playthroughs ─────────────────────────────────────────────────────────

func parsePlaythroughFields(r *http.Request) model.PlaythroughFields {
	return model.PlaythroughFields{
		Started:  r.FormValue("started"),
		Finished: r.FormValue("finished"),
		Status:   r.FormValue("status"),
		Platform: r.FormValue("platform"),
		Subgame:  r.FormValue("subgame"),
		Rating:   r.FormValue("rating"),
		Notes:    r.FormValue("notes"),
	}
}

func validatePlaythroughFields(f model.PlaythroughFields, requireStarted bool) error {
	if err := forms.ValidateDate(requireStarted)(f.Started); err != nil {
		return fmt.Errorf("started: %w", err)
	}
	if err := forms.ValidateDate(false)(f.Finished); err != nil {
		return fmt.Errorf("finished: %w", err)
	}
	if err := forms.ValidateRating(f.Rating); err != nil {
		return fmt.Errorf("rating: %w", err)
	}
	return nil
}

func (s *server) handleNewPlaythrough(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	doc, pf, ok := s.gameOr404(w, r, slug)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	f := parsePlaythroughFields(r)
	if err := validatePlaythroughFields(f, true); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	if conflict := mutate.OneShotConflict(pf.Playthroughs, f.Status, f.Platform, doc.FM.Platform, doc.FM.Status, f.Subgame); conflict != nil {
		want := model.EffectivePlatform(f.Platform, doc.FM.Platform)
		redirectErr(w, r, gamePath(slug), fmt.Errorf("%s already has a %s playthrough on %s — log a new session instead",
			doc.FM.Title, f.Status, forms.OrDash(want)))
		return
	}
	pf.AddPlaythrough(f)
	if err := mutate.WriteEntry(pf); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	redirectOK(w, r, gamePath(slug), "Playthrough started.")
}

// handleUpdatePlaythrough answers with the playthrough_header fragment
// (htmx-swapped by the form's own hx-target) instead of redirecting, so
// updating one playthrough doesn't reload the whole game-detail page and
// discard in-progress edits elsewhere on it — see the "playthrough_header"
// template in playthrough_fragments.html.
func (s *server) handleUpdatePlaythrough(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	doc, pf, ok := s.gameOr404(w, r, slug)
	if !ok {
		return
	}
	idx, err := strconv.Atoi(r.PathValue("idx"))
	if err != nil || idx < 0 || idx >= len(pf.Playthroughs) {
		http.Error(w, "no such playthrough", http.StatusNotFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p := pf.Views()[idx]
	displayFinished := p.Finished
	if p.HasSessions() {
		displayFinished = p.Sessions[len(p.Sessions)-1].Finished
	}

	finished, status := r.FormValue("finished"), r.FormValue("status")
	platform, subgame, rating, notes := r.FormValue("platform"), r.FormValue("subgame"), r.FormValue("rating"), r.FormValue("notes")

	fail := func(err error) {
		view := buildPthUpdateView(slug, doc.FM.Platform, doc.FM.Subgames, p)
		view.DisplayFinished, view.Status, view.Platform, view.Subgame, view.Rating, view.Notes = finished, status, platform, subgame, rating, notes
		view.Error = err.Error()
		s.renderPlaythroughFragment(w, "playthrough_header", view)
	}

	if err := forms.ValidateDate(false)(finished); err != nil {
		fail(err)
		return
	}
	if err := forms.ValidateRating(rating); err != nil {
		fail(err)
		return
	}
	if finished == displayFinished && status == p.Status && platform == p.Platform &&
		subgame == p.Subgame && rating == p.Rating && notes == p.Notes {
		s.renderPlaythroughFragment(w, "playthrough_header", buildPthUpdateView(slug, doc.FM.Platform, doc.FM.Subgames, p))
		return
	}

	if err := pf.UpdatePlaythrough(idx, finished, status, platform, subgame, rating, notes); err != nil {
		fail(err)
		return
	}

	// Clearing a field drops its key — that's the intent, not loss.
	var allowed []string
	base := fmt.Sprintf("playthroughs[%d]", idx)
	if strings.TrimSpace(notes) == "" {
		allowed = append(allowed, base+".notes")
	}
	if strings.TrimSpace(rating) == "" {
		allowed = append(allowed, base+".rating")
	}
	if strings.TrimSpace(status) == "" {
		allowed = append(allowed, base+".status")
	}
	if strings.TrimSpace(platform) == "" {
		allowed = append(allowed, base+".platform")
	}
	if strings.TrimSpace(subgame) == "" {
		allowed = append(allowed, base+".subgame")
	}
	if strings.TrimSpace(finished) == "" {
		allowed = append(allowed, base+".finished",
			fmt.Sprintf("%s.sessions[%d].finished", base, len(p.Sessions)-1))
	}
	if err := mutate.WriteEntry(pf, allowed...); err != nil {
		fail(err)
		return
	}

	view := buildPthUpdateView(slug, doc.FM.Platform, doc.FM.Subgames, pf.Views()[idx])
	view.Saved = true
	s.renderPlaythroughFragment(w, "playthrough_header", view)
}

// handleSplitPlaythrough answers with the whole playthroughs_section fragment
// rather than one playthrough's own — a split changes the total playthrough
// count, which no single playthrough's fragment can represent — so it's the
// one action in this file that still discards unsaved edits elsewhere in the
// playthroughs section (though not the game-info form or planned-replays
// list above it, unlike the old full-page reload).
func (s *server) handleSplitPlaythrough(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	doc, pf, ok := s.gameOr404(w, r, slug)
	if !ok {
		return
	}
	idx, err := strconv.Atoi(r.PathValue("idx"))
	if err != nil || idx < 0 || idx >= len(pf.Playthroughs) {
		http.Error(w, "no such playthrough", http.StatusNotFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	fail := func(err error) {
		view := buildPlaythroughsSectionView(slug, doc.FM.Platform, doc.FM.Subgames, pf)
		view.Error = err.Error()
		s.renderPlaythroughFragment(w, "playthroughs_section", view)
	}

	keepLast, err := strconv.Atoi(r.FormValue("keep_last"))
	if err != nil {
		fail(fmt.Errorf("invalid session to split at"))
		return
	}
	newStatus := r.FormValue("new_status")

	before := append([]model.SessionEntry(nil), pf.Playthroughs[idx].Sessions...)
	splitFrom := keepLast + 1
	if _, err := pf.SplitPlaythrough(idx, splitFrom, newStatus); err != nil {
		fail(err)
		return
	}
	allowed := model.TruncateSessionAllowedPaths(idx, before, splitFrom)
	if err := mutate.WriteSplit(pf, allowed...); err != nil {
		fail(err)
		return
	}
	s.renderPlaythroughFragment(w, "playthroughs_section", buildPlaythroughsSectionView(slug, doc.FM.Platform, doc.FM.Subgames, pf))
}

// ── Sessions ─────────────────────────────────────────────────────────────

func (s *server) handleAddSession(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	_, pf, ok := s.gameOr404(w, r, slug)
	if !ok {
		return
	}
	idx, err := strconv.Atoi(r.PathValue("idx"))
	if err != nil || idx < 0 || idx >= len(pf.Playthroughs) {
		redirectErr(w, r, gamePath(slug), fmt.Errorf("no such playthrough"))
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	started, finished, title := r.FormValue("started"), r.FormValue("finished"), r.FormValue("title")
	if err := forms.ValidateDate(true)(started); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	if err := forms.ValidateDate(false)(finished); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}

	hadSessions := pf.Views()[idx].HasSessions()
	if err := pf.AddSession(idx, started, finished, title); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	var allowed []string
	if !hadSessions {
		allowed = []string{
			fmt.Sprintf("playthroughs[%d].started", idx),
			fmt.Sprintf("playthroughs[%d].finished", idx),
		}
	}
	if err := mutate.WriteEntry(pf, allowed...); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	redirectOK(w, r, gamePath(slug), "Session logged.")
}

// handleEditSession answers with just the one session_row fragment it
// changed — same reasoning as handleUpdatePlaythrough above.
func (s *server) handleEditSession(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	_, pf, ok := s.gameOr404(w, r, slug)
	if !ok {
		return
	}
	idx, j, ok := s.sessionIndices(r, pf)
	if !ok {
		http.Error(w, "no such session", http.StatusNotFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	e := pf.Playthroughs[idx]
	hadSessions := len(e.Sessions) > 0
	var orig model.SessionEntry
	numSessions := len(e.Sessions)
	if hadSessions {
		orig = e.Sessions[j]
	} else {
		// The flat started/finished pair shown as an implicit session 1 —
		// see EditSession and sessionIndices.
		orig = model.SessionEntry{Started: e.Started, Finished: e.Finished}
		numSessions = 1
	}
	started, finished, title := r.FormValue("started"), r.FormValue("finished"), r.FormValue("title")

	fail := func(err error) {
		s.renderPlaythroughFragment(w, "session_row", sessionRowView{
			Session:     model.Session{Index: j, Started: started, Finished: finished, Title: title},
			Slug:        slug,
			PthIndex:    idx,
			NumSessions: numSessions,
			Error:       err.Error(),
		})
	}

	if err := forms.ValidateDate(true)(started); err != nil {
		fail(err)
		return
	}
	if err := forms.ValidateDate(false)(finished); err != nil {
		fail(err)
		return
	}
	if started == orig.Started && finished == orig.Finished && title == orig.Title {
		s.renderPlaythroughFragment(w, "session_row", sessionRowView{
			Session:     model.Session{Index: j, Started: started, Finished: finished, Title: title},
			Slug:        slug,
			PthIndex:    idx,
			NumSessions: numSessions,
		})
		return
	}
	if err := pf.EditSession(idx, j, started, finished, title); err != nil {
		fail(err)
		return
	}
	var allowed []string
	if !hadSessions {
		// Converting the flat pair to sessions[0] moves those two paths
		// rather than dropping them — same allowance as AddSession's own
		// first-time conversion in handleAddSession.
		allowed = append(allowed,
			fmt.Sprintf("playthroughs[%d].started", idx),
			fmt.Sprintf("playthroughs[%d].finished", idx))
	}
	if strings.TrimSpace(title) == "" && orig.Title != "" {
		allowed = append(allowed, model.SessionPath(idx, j, "title"))
	}
	if err := mutate.WriteEntry(pf, allowed...); err != nil {
		fail(err)
		return
	}
	s.renderPlaythroughFragment(w, "session_row", sessionRowView{
		Session:     pf.Views()[idx].Sessions[j],
		Slug:        slug,
		PthIndex:    idx,
		NumSessions: numSessions,
		Saved:       true,
	})
}

// handleDeleteSession answers with the whole sessions_table fragment for
// that one playthrough — RemoveSession shifts every later session's index
// down (model.PlaythroughsFile.RemoveSession), which changes the URL every
// subsequent row is bound to, so a single-row fragment can't represent the
// result. Still scoped to just this playthrough's table, not the whole page.
func (s *server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	doc, pf, ok := s.gameOr404(w, r, slug)
	if !ok {
		return
	}
	idx, j, ok := s.sessionIndices(r, pf)
	if !ok {
		http.Error(w, "no such session", http.StatusNotFound)
		return
	}
	before := append([]model.SessionEntry(nil), pf.Playthroughs[idx].Sessions...)

	fail := func(err error) {
		view := buildPthUpdateView(slug, doc.FM.Platform, doc.FM.Subgames, pf.Views()[idx])
		view.Error = err.Error()
		s.renderPlaythroughFragment(w, "sessions_table", view)
	}

	if err := pf.RemoveSession(idx, j); err != nil {
		fail(err)
		return
	}
	if err := mutate.WriteEntry(pf, model.RemoveSessionAllowedPaths(idx, before, j)...); err != nil {
		fail(err)
		return
	}
	s.renderPlaythroughFragment(w, "sessions_table", buildPthUpdateView(slug, doc.FM.Platform, doc.FM.Subgames, pf.Views()[idx]))
}

func (s *server) handleMoveSessionUp(w http.ResponseWriter, r *http.Request) {
	s.moveSession(w, r, -1)
}

func (s *server) handleMoveSessionDown(w http.ResponseWriter, r *http.Request) {
	s.moveSession(w, r, 1)
}

// moveSession swaps session j with its neighbor in the given direction
// (-1 up, +1 down) — the only way to reorder sessions once logged, so a
// session entered out of order can be walked into its right place instead
// of being deleted and re-added (which would drop anything Extra was
// carrying). Answers with the whole sessions_table fragment, same as
// delete: the swap changes which URL every row's buttons are bound to.
func (s *server) moveSession(w http.ResponseWriter, r *http.Request, delta int) {
	slug := r.PathValue("slug")
	doc, pf, ok := s.gameOr404(w, r, slug)
	if !ok {
		return
	}
	idx, j, ok := s.sessionIndices(r, pf)
	if !ok {
		http.Error(w, "no such session", http.StatusNotFound)
		return
	}

	fail := func(err error) {
		view := buildPthUpdateView(slug, doc.FM.Platform, doc.FM.Subgames, pf.Views()[idx])
		view.Error = err.Error()
		s.renderPlaythroughFragment(w, "sessions_table", view)
	}

	p := j
	if delta < 0 {
		p = j - 1
	}
	before := append([]model.SessionEntry(nil), pf.Playthroughs[idx].Sessions...)
	if err := pf.MoveSession(idx, p); err != nil {
		fail(err)
		return
	}
	if err := mutate.WriteEntry(pf, model.MoveSessionAllowedPaths(idx, before, p)...); err != nil {
		fail(err)
		return
	}
	s.renderPlaythroughFragment(w, "sessions_table", buildPthUpdateView(slug, doc.FM.Platform, doc.FM.Subgames, pf.Views()[idx]))
}

// sessionIndices parses and bounds-checks the idx/j path values together,
// since every session handler needs the same pair validated the same way.
func (s *server) sessionIndices(r *http.Request, pf *model.PlaythroughsFile) (idx, j int, ok bool) {
	idx, err := strconv.Atoi(r.PathValue("idx"))
	if err != nil || idx < 0 || idx >= len(pf.Playthroughs) {
		return 0, 0, false
	}
	j, err = strconv.Atoi(r.PathValue("j"))
	if err != nil || j < 0 {
		return 0, 0, false
	}
	e := pf.Playthroughs[idx]
	if j < len(e.Sessions) {
		return idx, j, true
	}
	// j==0 on an entry with no sessions: yet is the flat started/finished
	// pair the UI shows as an implicit session 1 — see EditSession.
	if j == 0 && len(e.Sessions) == 0 && e.Started != "" {
		return idx, j, true
	}
	return 0, 0, false
}

// ── Planned replays ──────────────────────────────────────────────────────

func (s *server) handleAddPlanned(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	doc, pf, ok := s.gameOr404(w, r, slug)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	platform, subgame, notes := r.FormValue("platform"), r.FormValue("subgame"), r.FormValue("notes")
	if mutate.HasPlannedFor(pf.Playthroughs, platform, doc.FM.Platform, subgame) {
		want := model.EffectivePlatform(platform, doc.FM.Platform)
		redirectErr(w, r, gamePath(slug), fmt.Errorf("%s already has a planned replay on %s", doc.FM.Title, forms.OrDash(want)))
		return
	}
	pf.AddPlaythrough(model.PlaythroughFields{Status: "planned", Platform: platform, Subgame: subgame, Notes: notes})
	if err := mutate.WriteEntry(pf); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	redirectOK(w, r, gamePath(slug), "Planned replay added.")
}

func (s *server) handleStartPlanned(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	_, pf, ok := s.gameOr404(w, r, slug)
	if !ok {
		return
	}
	idx, err := strconv.Atoi(r.PathValue("idx"))
	if err != nil || idx < 0 || idx >= len(pf.Playthroughs) {
		redirectErr(w, r, gamePath(slug), fmt.Errorf("no such playthrough"))
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	f := parsePlaythroughFields(r)
	if err := validatePlaythroughFields(f, true); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	if err := pf.GraduatePlannedPlaythrough(idx, f); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	// Every field GraduatePlannedPlaythrough sets was previously empty (idx
	// was a bare "planned" placeholder), so this is a pure addition — no
	// removal to declare, matching doStartPlanned in main.go.
	if err := mutate.WriteEntry(pf); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	redirectOK(w, r, gamePath(slug), "Playthrough started.")
}

func (s *server) handleEditPlanned(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	_, pf, ok := s.gameOr404(w, r, slug)
	if !ok {
		return
	}
	idx, err := strconv.Atoi(r.PathValue("idx"))
	if err != nil || idx < 0 || idx >= len(pf.Playthroughs) {
		redirectErr(w, r, gamePath(slug), fmt.Errorf("no such playthrough"))
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	p := pf.Views()[idx]
	platform, subgame, notes := r.FormValue("platform"), r.FormValue("subgame"), r.FormValue("notes")
	if platform == p.Platform && subgame == p.Subgame && notes == p.Notes {
		redirectOK(w, r, gamePath(slug), "No changes.")
		return
	}
	if err := pf.EditPlanned(idx, platform, subgame, notes); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	var allowed []string
	base := fmt.Sprintf("playthroughs[%d]", idx)
	if strings.TrimSpace(platform) == "" {
		allowed = append(allowed, base+".platform")
	}
	if strings.TrimSpace(subgame) == "" {
		allowed = append(allowed, base+".subgame")
	}
	if strings.TrimSpace(notes) == "" {
		allowed = append(allowed, base+".notes")
	}
	if err := mutate.WriteEntry(pf, allowed...); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	redirectOK(w, r, gamePath(slug), "Saved.")
}

// ── Per-game achievement refresh ────────────────────────────────────────

func (s *server) handleRefreshAchievements(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	doc, _, ok := s.gameOr404(w, r, slug)
	if !ok {
		return
	}
	links := doc.ProviderLinks()
	if len(links) == 0 {
		redirectErr(w, r, gamePath(slug), fmt.Errorf("no retroachievements_id, steam_appid, psn_id, ubisoft_id or xbox_id set"))
		return
	}
	archiveDir := model.FindArchiveDir(s.gamesDir)
	saved, err := commands.SaveAchievements(r.Context(), archiveDir, doc.FM.Title, links, commands.LoadCredentials())
	if err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	if _, err := model.WriteAchievementSummary(archiveDir, doc.GameDir(), links); err != nil {
		fmt.Printf("  (achievement summary not updated: %v)\n", err)
	}

	var parts []string
	for _, sv := range saved {
		p := sv.Record
		if p.LastError != "" {
			parts = append(parts, fmt.Sprintf("%s: %s (existing data kept)", sv.Label(), p.LastError))
		} else {
			parts = append(parts, fmt.Sprintf("%s: %d/%d unlocked", sv.Label(), p.Unlocked, p.Total))
		}
	}
	sort.Strings(parts)
	redirectOK(w, r, gamePath(slug), "Refreshed — "+strings.Join(parts, "; "))
}
