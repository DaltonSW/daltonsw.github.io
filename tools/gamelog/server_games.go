package main

import (
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// loadGame loads a game's Doc and PlaythroughsFile by slug, the web
// equivalent of what the TUI's SelectGame + logForGame do together.
func (s *server) loadGame(slug string) (*Doc, *PlaythroughsFile, error) {
	path := filepath.Join(s.gamesDir, slug, "_index.md")
	doc, err := LoadDoc(path)
	if err != nil {
		return nil, nil, err
	}
	pf, err := LoadPlaythroughs(filepath.Dir(path))
	if err != nil {
		return nil, nil, err
	}
	return doc, pf, nil
}

// gameOr404 loads a game or writes a 404, returning ok=false when the
// handler should stop.
func (s *server) gameOr404(w http.ResponseWriter, r *http.Request, slug string) (*Doc, *PlaythroughsFile, bool) {
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
	GameSummary
	Error string
	Saved bool // true right after a successful quickedit, for a one-shot CSS flash
}

type indexData struct {
	Page
	Games []gameRowView
	Query string
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	games, err := ListGames(s.gamesDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q != "" {
		lower := strings.ToLower(q)
		var filtered []GameSummary
		for _, g := range games {
			if strings.Contains(strings.ToLower(g.Title), lower) {
				filtered = append(filtered, g)
			}
		}
		games = filtered
	}
	rows := make([]gameRowView, len(games))
	for i, g := range games {
		rows[i] = gameRowView{GameSummary: g}
	}
	s.render(w, "index", indexData{Page: newPage(r, "Games", "games"), Games: rows, Query: q})
}

type gameNewData struct {
	Page
	NewGameFields
}

func (s *server) handleNewGameForm(w http.ResponseWriter, r *http.Request) {
	data := gameNewData{
		Page:          newPage(r, "New game", "games"),
		NewGameFields: NewGameFields{Status: "playing", Started: today()},
	}
	s.render(w, "game_new", data)
}

func (s *server) handleCreateGame(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f := NewGameFields{
		Title:    strings.TrimSpace(r.FormValue("title")),
		Platform: r.FormValue("platform"),
		Status:   r.FormValue("status"),
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
	raID, err := parseExternalID(r.FormValue("ra_id"))
	if err != nil {
		fail("RetroAchievements ID: " + err.Error())
		return
	}
	steamID, err := parseExternalID(r.FormValue("steam_id"))
	if err != nil {
		fail("Steam appid: " + err.Error())
		return
	}
	if err := validateDate(false)(f.Started); err != nil {
		fail("started: " + err.Error())
		return
	}
	if err := validateDate(false)(f.Finished); err != nil {
		fail("finished: " + err.Error())
		return
	}
	if err := validateRating(f.Rating); err != nil {
		fail("rating: " + err.Error())
		return
	}
	f.RetroAchievementsID, f.SteamAppID = raID, steamID

	slug := Slugify(f.Title)
	path, err := CreateGameFile(s.gamesDir, slug, f)
	if err != nil {
		fail(err.Error())
		return
	}
	fmt.Printf("Created %s\n", path)
	redirectOK(w, r, gamePath(slug), "Created.")
}

// ── Game detail ─────────────────────────────────────────────────────────────

// ptView adds the one derived field the update form needs — the finished
// date to display/edit, which lives on the trailing session once a
// playthrough has been converted to sessions (see doUpdate in main.go).
type ptView struct {
	Playthrough
	DisplayFinished string
}

type gameDetailData struct {
	Page
	Slug             string
	FM               FrontMatter
	RatingStr        string
	HasProviderLink  bool
	Planned          []Playthrough
	Playthroughs     []ptView
	Today            string
	DefaultPthStatus string
}

func (s *server) buildGameDetail(r *http.Request, slug string, doc *Doc, pf *PlaythroughsFile) gameDetailData {
	views := pf.Views()
	var planned []Playthrough
	var played []ptView
	for _, p := range views {
		if p.Status == "planned" {
			planned = append(planned, p)
			continue
		}
		v := ptView{Playthrough: p, DisplayFinished: p.Finished}
		if p.HasSessions() {
			v.DisplayFinished = p.Sessions[len(p.Sessions)-1].Finished
		}
		played = append(played, v)
	}

	defaultStatus := "playing"
	if doc.FM.Status == "ongoing" || doc.FM.Status == "multiplayer" {
		defaultStatus = doc.FM.Status
	}

	return gameDetailData{
		Page:             newPage(r, doc.FM.Title, "games"),
		Slug:             slug,
		FM:               doc.FM,
		RatingStr:        doc.FM.RatingString(),
		HasProviderLink:  len(doc.ProviderLinks()) > 0,
		Planned:          planned,
		Playthroughs:     played,
		Today:            today(),
		DefaultPthStatus: defaultStatus,
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
	started, finished, rating := r.FormValue("started"), r.FormValue("finished"), r.FormValue("rating")
	if err := validateDate(false)(started); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	if err := validateDate(false)(finished); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	if err := validateRating(rating); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}

	updated := doc.FM
	updated.Title = title
	updated.Platform = r.FormValue("platform")
	updated.Status = r.FormValue("status")
	updated.Started = started
	updated.Finished = finished
	updated.SetRating(rating)
	updated.Draft = r.FormValue("draft") == "1"

	if sameGameInfo(doc.FM, updated) {
		redirectOK(w, r, gamePath(slug), "No changes.")
		return
	}
	doc.FM = updated

	// Front-matter scalar fields aren't omitempty, so clearing one leaves the
	// key present rather than dropping it — nothing here is ever a legitimate
	// removal to declare, matching doEditGameInfo in main.go.
	syncing := pf.SyncStatus(doc.FM.Status, doc.FM.Finished)
	if err := writeFrontMatter(doc, pf, syncing); err != nil {
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

	status := r.FormValue("status")
	started, finished := r.FormValue("started"), r.FormValue("finished")
	platform, rating := r.FormValue("platform"), r.FormValue("rating")

	fail := func(err error) {
		row := gameSummaryFor(slug, doc.Path, doc, pf)
		row.Status, row.FMStarted, row.FMFinished, row.Platform, row.Rating = status, started, finished, platform, rating
		s.renderRow(w, gameRowView{GameSummary: row, Error: err.Error()})
	}

	if err := validateDate(false)(started); err != nil {
		fail(fmt.Errorf("started: %w", err))
		return
	}
	if err := validateDate(false)(finished); err != nil {
		fail(fmt.Errorf("finished: %w", err))
		return
	}
	if err := validateRating(rating); err != nil {
		fail(fmt.Errorf("rating: %w", err))
		return
	}

	updated := doc.FM
	updated.Status = status
	updated.Started = started
	updated.Finished = finished
	updated.Platform = platform
	updated.SetRating(rating)

	if !sameGameInfo(doc.FM, updated) {
		doc.FM = updated
		// Front-matter scalar fields aren't omitempty, so clearing one leaves
		// the key present rather than dropping it — same reasoning as
		// handleEditInfo above: nothing here is ever a legitimate removal to
		// declare.
		syncing := pf.SyncStatus(doc.FM.Status, doc.FM.Finished)
		if err := writeFrontMatter(doc, pf, syncing); err != nil {
			fail(err)
			return
		}
	}

	s.renderRow(w, gameRowView{GameSummary: gameSummaryFor(slug, doc.Path, doc, pf), Saved: true})
}

// ── Playthroughs ─────────────────────────────────────────────────────────

func parsePlaythroughFields(r *http.Request) PlaythroughFields {
	return PlaythroughFields{
		Started:  r.FormValue("started"),
		Finished: r.FormValue("finished"),
		Status:   r.FormValue("status"),
		Platform: r.FormValue("platform"),
		Rating:   r.FormValue("rating"),
		Notes:    r.FormValue("notes"),
	}
}

func validatePlaythroughFields(f PlaythroughFields, requireStarted bool) error {
	if err := validateDate(requireStarted)(f.Started); err != nil {
		return fmt.Errorf("started: %w", err)
	}
	if err := validateDate(false)(f.Finished); err != nil {
		return fmt.Errorf("finished: %w", err)
	}
	if err := validateRating(f.Rating); err != nil {
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
	if conflict := oneShotConflict(pf.Playthroughs, f.Status, f.Platform, doc.FM.Platform, doc.FM.Status); conflict != nil {
		want := effectivePlatform(f.Platform, doc.FM.Platform)
		redirectErr(w, r, gamePath(slug), fmt.Errorf("%s already has a %s playthrough on %s — log a new session instead",
			doc.FM.Title, f.Status, orDash(want)))
		return
	}
	pf.AddPlaythrough(f)
	if err := writeEntry(pf); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	redirectOK(w, r, gamePath(slug), "Playthrough started.")
}

func (s *server) handleUpdatePlaythrough(w http.ResponseWriter, r *http.Request) {
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
	displayFinished := p.Finished
	if p.HasSessions() {
		displayFinished = p.Sessions[len(p.Sessions)-1].Finished
	}

	finished, status := r.FormValue("finished"), r.FormValue("status")
	platform, rating, notes := r.FormValue("platform"), r.FormValue("rating"), r.FormValue("notes")
	if err := validateDate(false)(finished); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	if err := validateRating(rating); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	if finished == displayFinished && status == p.Status && platform == p.Platform &&
		rating == p.Rating && notes == p.Notes {
		redirectOK(w, r, gamePath(slug), "No changes.")
		return
	}

	if err := pf.UpdatePlaythrough(idx, finished, status, platform, rating, notes); err != nil {
		redirectErr(w, r, gamePath(slug), err)
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
	if strings.TrimSpace(finished) == "" {
		allowed = append(allowed, base+".finished",
			fmt.Sprintf("%s.sessions[%d].finished", base, len(p.Sessions)-1))
	}
	if err := writeEntry(pf, allowed...); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	redirectOK(w, r, gamePath(slug), "Updated.")
}

func (s *server) handleSplitPlaythrough(w http.ResponseWriter, r *http.Request) {
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
	keepLast, err := strconv.Atoi(r.FormValue("keep_last"))
	if err != nil {
		redirectErr(w, r, gamePath(slug), fmt.Errorf("invalid session to split at"))
		return
	}
	newStatus := r.FormValue("new_status")

	before := append([]SessionEntry(nil), pf.Playthroughs[idx].Sessions...)
	splitFrom := keepLast + 1
	newIdx, err := pf.SplitPlaythrough(idx, splitFrom, newStatus)
	if err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	allowed := truncateSessionAllowedPaths(idx, before, splitFrom)
	if err := writeSplit(pf, allowed...); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	_ = newIdx
	redirectOK(w, r, gamePath(slug), "Split into a new playthrough.")
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
	if err := validateDate(true)(started); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	if err := validateDate(false)(finished); err != nil {
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
	if err := writeEntry(pf, allowed...); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	redirectOK(w, r, gamePath(slug), "Session logged.")
}

func (s *server) handleEditSession(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	_, pf, ok := s.gameOr404(w, r, slug)
	if !ok {
		return
	}
	idx, j, ok := s.sessionIndices(r, pf)
	if !ok {
		redirectErr(w, r, gamePath(slug), fmt.Errorf("no such session"))
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	orig := pf.Playthroughs[idx].Sessions[j]
	started, finished, title := r.FormValue("started"), r.FormValue("finished"), r.FormValue("title")
	if err := validateDate(true)(started); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	if err := validateDate(false)(finished); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	if started == orig.Started && finished == orig.Finished && title == orig.Title {
		redirectOK(w, r, gamePath(slug), "No changes.")
		return
	}
	if err := pf.EditSession(idx, j, started, finished, title); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	var allowed []string
	if strings.TrimSpace(title) == "" && orig.Title != "" {
		allowed = append(allowed, sessionPath(idx, j, "title"))
	}
	if err := writeEntry(pf, allowed...); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	redirectOK(w, r, gamePath(slug), "Session updated.")
}

func (s *server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	_, pf, ok := s.gameOr404(w, r, slug)
	if !ok {
		return
	}
	idx, j, ok := s.sessionIndices(r, pf)
	if !ok {
		redirectErr(w, r, gamePath(slug), fmt.Errorf("no such session"))
		return
	}
	before := append([]SessionEntry(nil), pf.Playthroughs[idx].Sessions...)
	if err := pf.RemoveSession(idx, j); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	if err := writeEntry(pf, removeSessionAllowedPaths(idx, before, j)...); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	redirectOK(w, r, gamePath(slug), "Session deleted.")
}

// sessionIndices parses and bounds-checks the idx/j path values together,
// since every session handler needs the same pair validated the same way.
func (s *server) sessionIndices(r *http.Request, pf *PlaythroughsFile) (idx, j int, ok bool) {
	idx, err := strconv.Atoi(r.PathValue("idx"))
	if err != nil || idx < 0 || idx >= len(pf.Playthroughs) {
		return 0, 0, false
	}
	j, err = strconv.Atoi(r.PathValue("j"))
	if err != nil || j < 0 || j >= len(pf.Playthroughs[idx].Sessions) {
		return 0, 0, false
	}
	return idx, j, true
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
	platform, notes := r.FormValue("platform"), r.FormValue("notes")
	if hasPlannedFor(pf.Playthroughs, platform, doc.FM.Platform) {
		want := effectivePlatform(platform, doc.FM.Platform)
		redirectErr(w, r, gamePath(slug), fmt.Errorf("%s already has a planned replay on %s", doc.FM.Title, orDash(want)))
		return
	}
	pf.AddPlaythrough(PlaythroughFields{Status: "planned", Platform: platform, Notes: notes})
	if err := writeEntry(pf); err != nil {
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
	if err := writeEntry(pf); err != nil {
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
	platform, notes := r.FormValue("platform"), r.FormValue("notes")
	if platform == p.Platform && notes == p.Notes {
		redirectOK(w, r, gamePath(slug), "No changes.")
		return
	}
	if err := pf.EditPlanned(idx, platform, notes); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	var allowed []string
	base := fmt.Sprintf("playthroughs[%d]", idx)
	if strings.TrimSpace(platform) == "" {
		allowed = append(allowed, base+".platform")
	}
	if strings.TrimSpace(notes) == "" {
		allowed = append(allowed, base+".notes")
	}
	if err := writeEntry(pf, allowed...); err != nil {
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
		redirectErr(w, r, gamePath(slug), fmt.Errorf("no retroachievements_id, steam_appid or psn_id set"))
		return
	}
	archiveDir := findArchiveDir(s.gamesDir)
	saved, err := saveAchievements(r.Context(), archiveDir, doc.FM.Title, links, loadCredentials())
	if err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	if _, err := writeAchievementSummary(archiveDir, doc.GameDir(), links); err != nil {
		fmt.Printf("  (achievement summary not updated: %v)\n", err)
	}

	var parts []string
	for _, sv := range saved {
		p := sv.Record
		if p.LastError != "" {
			parts = append(parts, fmt.Sprintf("%s: %s (existing data kept)", sv.Provider, p.LastError))
		} else {
			parts = append(parts, fmt.Sprintf("%s: %d/%d unlocked", sv.Provider, p.Unlocked, p.Total))
		}
	}
	sort.Strings(parts)
	redirectOK(w, r, gamePath(slug), "Refreshed — "+strings.Join(parts, "; "))
}
