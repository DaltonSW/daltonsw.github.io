package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"go.dalton.dog/gamelog/internal/commands"
	"go.dalton.dog/gamelog/internal/externalid"
	"go.dalton.dog/gamelog/internal/model"
	"go.dalton.dog/gamelog/internal/mutate"
	"go.dalton.dog/gamelog/internal/providers/nadeo"
)

// loadGame loads a game's Doc and PlaythroughsFile by slug.
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
	// Title is the page's title. Page and NewGameFields both have one, and a
	// template can't resolve the tie, so this shallower field settles it for
	// the layout; the form reads .NewGameFields.Title.
	Title     string
	KnownTags []string // suggestions for the tags field
}

func newGameData(r *http.Request, f model.NewGameFields, knownTags []string) gameNewData {
	page := newPage(r, "New game", "games")
	return gameNewData{Page: page, NewGameFields: f, Title: page.Title, KnownTags: knownTags}
}

// knownTags is every tag in use, for the tag fields' suggestions. A listing
// failure only costs the suggestions, so it isn't an error.
func (s *server) knownTags() []string {
	games, err := model.ListGames(s.gamesDir)
	if err != nil {
		return nil
	}
	return model.KnownTags(games)
}

func (s *server) handleNewGameForm(w http.ResponseWriter, r *http.Request) {
	data := newGameData(r, model.NewGameFields{Status: "playing", Started: model.Today()}, s.knownTags())
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
		Subgames: model.ParseSubgames(r.FormValue("subgames")),
		Tags:     model.ParseTags(r.FormValue("tags")),
		Started:  r.FormValue("started"),
		Finished: r.FormValue("finished"),
		Rating:   r.FormValue("rating"),
		Overview: r.FormValue("overview"),
	}

	fail := func(msg string) {
		data := newGameData(r, f, s.knownTags())
		data.Flash.Error = msg
		s.render(w, "game_new", data)
	}

	if f.Title == "" {
		fail("title is required")
		return
	}
	for _, key := range []string{model.ProviderRA, model.ProviderSteam} {
		id, err := externalid.ParseExternalID(r.FormValue(key))
		if err != nil {
			fail(model.ProviderLabel(key) + " ID: " + err.Error())
			return
		}
		f.SetProviderID(key, id)
	}
	if err := model.ValidateDate(false)(f.Started); err != nil {
		fail("started: " + err.Error())
		return
	}
	if err := model.ValidateDate(false)(f.Finished); err != nil {
		fail("finished: " + err.Error())
		return
	}
	if err := model.ValidateRating(f.Rating); err != nil {
		fail("rating: " + err.Error())
		return
	}
	slug := model.Slugify(f.Title)
	path, err := model.CreateGameFile(s.gamesDir, slug, f)
	if err != nil {
		fail(err.Error())
		return
	}
	fmt.Printf("Created %s\n", path)
	redirectOK(w, r, gamePath(slug), "Created.")
}

// providerField is one input of the provider-IDs form.
type providerField struct {
	Key, Label, Placeholder, Value string
}

func providerFields(ids map[string]string) []providerField {
	var out []providerField
	for _, p := range model.Providers() {
		out = append(out, providerField{Key: p.Key, Label: p.FormLabel, Placeholder: p.Placeholder, Value: ids[p.Key]})
	}
	return out
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

// buildSessionRows renders a playthrough's sessions table rows.
func buildSessionRows(slug string, p model.Playthrough) []sessionRowView {
	rows := make([]sessionRowView, len(p.Sessions))
	for i, sess := range p.Sessions {
		rows[i] = sessionRowView{Session: sess, Slug: slug, PthIndex: p.Index, NumSessions: len(p.Sessions)}
	}
	return rows
}

func buildPthUpdateView(slug, gamePlatform string, gameSubgames []string, p model.Playthrough) pthUpdateView {
	return pthUpdateView{
		Playthrough:     p,
		DisplayFinished: p.FinishDate(),
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
	// Started/FirstFinished are derived from playthroughs.yaml and shown
	// read-only; the game itself carries no dates. StatusDerived marks the
	// status as derived from runs too — see model.GameSummary.
	Started       string
	FirstFinished string
	StatusDerived bool
	Achievements  []achievementView
	// Overview is the read-only completion/playtime/stats panel; nil when the
	// game has no captured achievements or stats.
	Overview *gameOverview
	// Lists feed the add-to-list selects; ListRanks is where this game itself is ranked.
	Lists        []model.GameList
	ListRanks    []model.ListRank
	SelectedList string   // preselects the selectors after an add (?list=)
	KnownTags    []string // suggestions for the tags fields
	// IDs is this game's provider ids as strings, whatever the YAML scalar
	// underneath decoded to, and ProviderFields is the provider-IDs form built
	// from them — one input per model.Providers() entry.
	IDs            map[string]string
	ProviderFields []providerField
	// NadeoAccountID/NinjaKiwiUserID are the non-achievement links (see
	// README's Trackmania and BTD6 sections: they stay out of ProviderLinks); each gets its own
	// refresh button.
	NadeoAccountID  string
	NinjaKiwiUserID string
	// NadeoSeasons feeds the Trackmania season picker, read from the archive
	// (no request); NadeoSeasonsErr says why it's empty when it couldn't be
	// read. NadeoJob is a season fetch still running, if any.
	NadeoSeasons    []commands.NadeoSeason
	NadeoSeasonsErr string
	NadeoJob        string
	// Subsets are the RetroAchievements subsets attached to this game, with
	// the counts the summary keeps separate from the game's own. Read-only
	// here: attaching one is Housekeeping's job, because it needs the parent
	// check only the provider can answer (see commands.AttachSubset).
	Subsets []model.SubsetBreakdown
	// AutoSuggest fetches the suggest panel as soon as the page loads — set
	// by ?suggest=1, which is where every "Suggest" link points.
	AutoSuggest bool
}

// achievementView is one earned achievement on a game's page, plus the lists it's on.
type achievementView struct {
	model.EarnedAchievement
	Ranks []model.ListRank
}

func (s *server) buildGameDetail(r *http.Request, slug string, doc *model.Doc, pf *model.PlaythroughsFile) gameDetailData {
	var planned []model.Playthrough
	for _, p := range pf.Views() {
		if p.Status == "planned" {
			planned = append(planned, p)
		}
	}

	defaultStatus := model.DefaultPlaythroughStatus(doc.FM.Status)

	// Same rule as the (now-removed) review flow's delete action: a game
	// with captured playthroughs or archive history isn't a stub, and
	// deleting it would risk real data — see commands.DeleteGameStub.
	g := model.GameSummaryFor(slug, doc.Path, doc, pf)
	canDelete := g.NumPlaythroughs == 0 && !commands.HasArchiveRecord(model.FindArchiveDir(s.gamesDir), g.ProviderLinks())

	lists, _ := model.LoadLists(s.gamesDir) // a broken lists file shouldn't block editing a game
	var achievements []achievementView
	var subsets []model.SubsetBreakdown
	if summary, err := model.LoadAchievementSummary(doc.GameDir()); err == nil && summary != nil {
		subsets = summary.Subsets
		// Display newest-first; the stored file is oldest-first (see
		// AchievementSummary.Earned's doc comment) since display order is a
		// reader's choice, not the archive's.
		achievements = make([]achievementView, len(summary.Earned))
		for i, a := range summary.Earned {
			achievements[len(summary.Earned)-1-i] = achievementView{
				EarnedAchievement: a,
				Ranks:             lists.AchievementRanks(slug, a.Provider, a.ID, a.Key),
			}
		}
	}

	var nadeoSeasons []commands.NadeoSeason
	var nadeoSeasonsErr, nadeoJob string
	if doc.NadeoAccountIDString() != "" {
		var err error
		if nadeoSeasons, err = commands.NadeoSeasons(s.gamesDir); err != nil {
			nadeoSeasonsErr = err.Error()
		}
		nadeoJob = s.runningNadeoJob()
	}

	return gameDetailData{
		Page:             newPage(r, doc.FM.Title, "games"),
		Slug:             slug,
		FM:               doc.FM,
		RatingStr:        doc.FM.RatingString(),
		HasProviderLink:  len(doc.ProviderLinks()) > 0,
		Planned:          planned,
		Playthroughs:     buildPlaythroughsSectionView(slug, doc.FM.Platform, doc.FM.Subgames, pf),
		Today:            model.Today(),
		DefaultPthStatus: defaultStatus,
		CanDelete:        canDelete,
		Started:          g.Started,
		FirstFinished:    g.FirstFinished,
		StatusDerived:    g.StatusDerived,
		Achievements:     achievements,
		Overview:         buildGameOverview(doc.GameDir()),
		Lists:            lists.Lists,
		ListRanks:        lists.GameRanks(slug),
		SelectedList:     r.URL.Query().Get("list"),
		KnownTags:        s.knownTags(),
		IDs:              g.IDs,
		ProviderFields:   providerFields(g.IDs),
		NadeoAccountID:   doc.NadeoAccountIDString(),
		NinjaKiwiUserID:  doc.NinjaKiwiUserIDString(),
		NadeoSeasons:     nadeoSeasons,
		NadeoSeasonsErr:  nadeoSeasonsErr,
		NadeoJob:         nadeoJob,
		Subsets:          subsets,
		AutoSuggest:      r.URL.Query().Get("suggest") == "1",
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
	redirectOK(w, r, "/games", doc.FM.Title+": deleted.")
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

	g := model.GameSummaryFor(slug, doc.Path, doc, pf)
	rating := r.FormValue("rating")
	if err := model.ValidateRating(rating); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}

	updated := doc.FM
	updated.Title = title
	updated.Platform = r.FormValue("platform")
	if !g.StatusDerived { // a derived status follows the runs; a posted one is ignored
		updated.Status = r.FormValue("status")
	}
	updated.Subgames = model.ParseSubgames(r.FormValue("subgames"))
	updated.Tags = model.ParseTags(r.FormValue("tags"))
	updated.SetRating(rating)
	updated.Draft = r.FormValue("draft") == "1"

	if mutate.SameGameInfo(doc.FM, updated) {
		redirectOK(w, r, gamePath(slug), "No changes.")
		return
	}
	old := doc.FM
	doc.FM = updated

	// Front-matter scalar fields aren't omitempty, so clearing one leaves the
	// key present. The two lists are: a shortened subgames or tags list drops
	// its tail items, which is the edit's intent.
	removed := slices.Concat(
		model.ShrunkListPaths("subgames", len(old.Subgames), len(updated.Subgames)),
		model.ShrunkListPaths("tags", len(old.Tags), len(updated.Tags)),
	)
	if err := mutate.WriteFrontMatter(doc, removed...); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	redirectOK(w, r, gamePath(slug), "Saved.")
}

// handleUpdateProviders edits the game's provider IDs (model.Providers) — the
// fields that drive both achievement-fetch linking (Doc.ProviderLinks) and the
// `suggest` picker. Split out from handleEditInfo since these fields have their
// own validation (RA/Steam accept a pasted URL or bare ID; the rest are free
// text since PSN's NPWR… ids aren't numeric) and their own field-loss story:
// the later providers are `omitempty`, so clearing one is a real key removal
// that must be declared, unlike every field handleEditInfo touches.
func (s *server) handleUpdateProviders(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	doc, _, ok := s.gameOr404(w, r, slug)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}

	// Every provider's input is named by its key. A cleared field that was set
	// drops its key (psn_id and the later ones are omitempty), which the loss
	// check must be told about; for RA and Steam the key stays and this is moot.
	var allowedRemovals []string
	for _, p := range model.Providers() {
		raw := r.FormValue(p.Key)
		id := strings.TrimSpace(raw)
		if p.NumericID {
			parsed, err := externalid.ParseExternalID(raw)
			if err != nil {
				redirectErr(w, r, gamePath(slug), fmt.Errorf("%s: %w", p.FormLabel, err))
				return
			}
			id = parsed
		}
		if id == "" && doc.ProviderID(p.Key) != "" {
			allowedRemovals = append(allowedRemovals, p.FMKey)
		}
		if err := doc.SetProviderID(p.Key, id); err != nil {
			redirectErr(w, r, gamePath(slug), err)
			return
		}
	}

	if err := mutate.WriteFrontMatter(doc, allowedRemovals...); err != nil {
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
	platform, rating := r.FormValue("platform"), r.FormValue("rating")

	fail := func(err error) {
		row := model.GameSummaryFor(slug, doc.Path, doc, pf)
		row.Status, row.Platform, row.Rating = status, platform, rating
		s.renderFragment(w, "game_row.html", "game_row", gameRowView{GameSummary: row, Error: err.Error()})
	}

	if g := model.GameSummaryFor(slug, doc.Path, doc, pf); g.StatusDerived {
		status = doc.FM.Status // follows the runs; a posted value is ignored
	}
	if err := model.ValidateRating(rating); err != nil {
		fail(fmt.Errorf("rating: %w", err))
		return
	}

	updated := doc.FM
	updated.Status = status
	updated.Platform = platform
	updated.SetRating(rating)

	if !mutate.SameGameInfo(doc.FM, updated) {
		doc.FM = updated
		// Front-matter scalar fields aren't omitempty, so clearing one leaves
		// the key present rather than dropping it — same reasoning as
		// handleEditInfo above: nothing here is ever a legitimate removal to
		// declare.
		if err := mutate.WriteFrontMatter(doc); err != nil {
			fail(err)
			return
		}
	}

	s.renderFragment(w, "game_row.html", "game_row", gameRowView{GameSummary: model.GameSummaryFor(slug, doc.Path, doc, pf), Saved: true})
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
		if err := mutate.WriteFrontMatter(doc); err != nil {
			s.renderFragment(w, "game_row.html", "game_row", gameRowView{GameSummary: model.GameSummaryFor(slug, doc.Path, doc, pf), Error: err.Error()})
			return
		}
	}
	if draftsOnly {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		return
	}
	s.renderFragment(w, "game_row.html", "game_row", gameRowView{GameSummary: model.GameSummaryFor(slug, doc.Path, doc, pf), Saved: true})
}

// ── Playthroughs ─────────────────────────────────────────────────────────

func parsePlaythroughFields(r *http.Request) model.PlaythroughFields {
	return model.PlaythroughFields{
		Started:  r.FormValue("started"),
		Finished: r.FormValue("finished"),
		Beaten:   strings.TrimSpace(r.FormValue("beaten")),
		Status:   r.FormValue("status"),
		Platform: r.FormValue("platform"),
		Subgame:  r.FormValue("subgame"),
		Tags:     model.ParseTags(r.FormValue("tags")),
		Rating:   r.FormValue("rating"),
		Notes:    r.FormValue("notes"),
	}
}

func validatePlaythroughFields(f model.PlaythroughFields, requireStarted bool) error {
	if err := model.ValidateDate(requireStarted)(f.Started); err != nil {
		return fmt.Errorf("started: %w", err)
	}
	if err := model.ValidateDate(false)(f.Finished); err != nil {
		return fmt.Errorf("finished: %w", err)
	}
	if err := model.ValidateRating(f.Rating); err != nil {
		return fmt.Errorf("rating: %w", err)
	}
	return validateBeaten(f.Beaten, f.Started, f.Finished)
}

// validateBeaten checks a run's beaten: date is a date inside the run — on or
// after it started, and on or before it finished when it has. Compared at the
// coarser date's precision, so a partial "2018-09" start doesn't reject a day
// in September.
func validateBeaten(beaten, started, finished string) error {
	if beaten == "" {
		return nil
	}
	if err := model.ValidateDate(false)(beaten); err != nil {
		return fmt.Errorf("beaten: %w", err)
	}
	cmp := func(a, b string) int {
		n := min(len(a), len(b))
		return strings.Compare(a[:n], b[:n])
	}
	if started != "" && cmp(beaten, started) < 0 {
		return fmt.Errorf("beaten %s is before the run started (%s)", beaten, started)
	}
	if finished != "" && cmp(beaten, finished) > 0 {
		return fmt.Errorf("beaten %s is after the run finished (%s) — leave it blank to use the finish date", beaten, finished)
	}
	return nil
}

func (s *server) handleNewPlaythrough(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	_, pf, ok := s.gameOr404(w, r, slug)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	if err := logNewPlaythrough(pf, parsePlaythroughFields(r)); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	redirectOK(w, r, gamePath(slug), "Playthrough started.")
}

// logNewPlaythrough validates and appends one playthrough, then writes it
// through the pre-write loss check (the game's status follows via
// SyncGameStatus). Shared by the game page and Catch up's activity cards, so
// there's one path that can add a run.
func logNewPlaythrough(pf *model.PlaythroughsFile, f model.PlaythroughFields) error {
	if err := validatePlaythroughFields(f, true); err != nil {
		return err
	}
	pf.AddPlaythrough(f)
	return mutate.WriteEntry(pf)
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
	displayFinished := p.FinishDate()

	finished, status := r.FormValue("finished"), r.FormValue("status")
	platform, subgame, rating, notes := r.FormValue("platform"), r.FormValue("subgame"), r.FormValue("rating"), r.FormValue("notes")
	beaten := strings.TrimSpace(r.FormValue("beaten"))
	tags := model.ParseTags(r.FormValue("tags"))

	fail := func(err error) {
		view := buildPthUpdateView(slug, doc.FM.Platform, doc.FM.Subgames, p)
		view.DisplayFinished, view.Status, view.Platform, view.Subgame, view.Rating, view.Notes = finished, status, platform, subgame, rating, notes
		view.Beaten, view.Tags = beaten, tags
		view.Error = err.Error()
		s.renderFragment(w, "playthrough_fragments.html", "playthrough_header", view)
	}

	if err := model.ValidateDate(false)(finished); err != nil {
		fail(err)
		return
	}
	if err := model.ValidateRating(rating); err != nil {
		fail(err)
		return
	}
	if err := validateBeaten(beaten, p.EarliestDate(), finished); err != nil {
		fail(err)
		return
	}
	if finished == displayFinished && status == p.Status && platform == p.Platform &&
		subgame == p.Subgame && slices.Equal(tags, p.Tags) && rating == p.Rating && notes == p.Notes && beaten == p.Beaten {
		s.renderFragment(w, "playthrough_fragments.html", "playthrough_header", buildPthUpdateView(slug, doc.FM.Platform, doc.FM.Subgames, p))
		return
	}

	if err := pf.UpdatePlaythrough(idx, finished, status, platform, subgame, tags, rating, notes); err != nil {
		fail(err)
		return
	}
	if err := pf.SetBeaten(idx, beaten); err != nil {
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
	if beaten == "" {
		allowed = append(allowed, base+".beaten")
	}
	allowed = append(allowed, model.ShrunkListPaths(base+".tags", len(p.Tags), len(tags))...)
	if err := mutate.WriteEntry(pf, allowed...); err != nil {
		fail(err)
		return
	}

	view := buildPthUpdateView(slug, doc.FM.Platform, doc.FM.Subgames, pf.Views()[idx])
	view.Saved = true
	s.renderFragment(w, "playthrough_fragments.html", "playthrough_header", view)
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
		s.renderFragment(w, "playthrough_fragments.html", "playthroughs_section", view)
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
	if err := mutate.WriteEntry(pf, allowed...); err != nil {
		fail(err)
		return
	}
	s.renderFragment(w, "playthrough_fragments.html", "playthroughs_section", buildPlaythroughsSectionView(slug, doc.FM.Platform, doc.FM.Subgames, pf))
}

// ── Sessions ─────────────────────────────────────────────────────────────

func (s *server) handleAddSession(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	_, pf, ok := s.gameOr404(w, r, slug)
	if !ok {
		return
	}
	idx, err := strconv.Atoi(r.PathValue("idx"))
	if err != nil {
		idx = -1
	}
	if err := r.ParseForm(); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	if err := logSession(pf, idx, r.FormValue("started"), r.FormValue("finished"), r.FormValue("title")); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	redirectOK(w, r, gamePath(slug), "Session logged.")
}

// logSession validates and appends one session to playthrough idx, then
// writes it through the loss check. Shared with Catch up's activity cards.
func logSession(pf *model.PlaythroughsFile, idx int, started, finished, title string) error {
	if idx < 0 || idx >= len(pf.Playthroughs) {
		return fmt.Errorf("no such playthrough")
	}
	if err := model.ValidateDate(true)(started); err != nil {
		return err
	}
	if err := model.ValidateDate(false)(finished); err != nil {
		return err
	}
	if err := pf.AddSession(idx, started, finished, title); err != nil {
		return err
	}
	return mutate.WriteEntry(pf)
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
	orig := e.Sessions[j]
	numSessions := len(e.Sessions)
	started, finished, title := r.FormValue("started"), r.FormValue("finished"), r.FormValue("title")

	fail := func(err error) {
		s.renderFragment(w, "playthrough_fragments.html", "session_row", sessionRowView{
			Session:     model.Session{Index: j, Started: started, Finished: finished, Title: title},
			Slug:        slug,
			PthIndex:    idx,
			NumSessions: numSessions,
			Error:       err.Error(),
		})
	}

	if err := model.ValidateDate(true)(started); err != nil {
		fail(err)
		return
	}
	if err := model.ValidateDate(false)(finished); err != nil {
		fail(err)
		return
	}
	if started == orig.Started && finished == orig.Finished && title == orig.Title {
		s.renderFragment(w, "playthrough_fragments.html", "session_row", sessionRowView{
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
	if strings.TrimSpace(title) == "" && orig.Title != "" {
		allowed = append(allowed, model.SessionPath(idx, j, "title"))
	}
	if err := mutate.WriteEntry(pf, allowed...); err != nil {
		fail(err)
		return
	}
	s.renderFragment(w, "playthrough_fragments.html", "session_row", sessionRowView{
		Session:     pf.Views()[idx].Sessions[j],
		Slug:        slug,
		PthIndex:    idx,
		NumSessions: numSessions,
		Saved:       true,
	})
}

// handleSplitSession cuts one session into two at a gap the user names. It
// answers with the whole sessions_table, since later sessions shift down.
func (s *server) handleSplitSession(w http.ResponseWriter, r *http.Request) {
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
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	fail := func(err error) {
		view := buildPthUpdateView(slug, doc.FM.Platform, doc.FM.Subgames, pf.Views()[idx])
		view.Error = err.Error()
		s.renderFragment(w, "playthrough_fragments.html", "sessions_table", view)
	}
	firstEnd, secondStart := r.FormValue("split_end"), r.FormValue("split_start")
	if err := model.ValidateDate(true)(firstEnd); err != nil {
		fail(fmt.Errorf("first session end: %w", err))
		return
	}
	if err := model.ValidateDate(true)(secondStart); err != nil {
		fail(fmt.Errorf("second session start: %w", err))
		return
	}
	before := append([]model.SessionEntry(nil), pf.Playthroughs[idx].Sessions...)
	if err := pf.SplitSession(idx, j, firstEnd, secondStart); err != nil {
		fail(err)
		return
	}
	allowed := model.InsertSessionAllowedPaths(idx, before, pf.Playthroughs[idx].Sessions)
	if err := mutate.WriteEntry(pf, allowed...); err != nil {
		fail(err)
		return
	}
	s.renderFragment(w, "playthrough_fragments.html", "sessions_table", buildPthUpdateView(slug, doc.FM.Platform, doc.FM.Subgames, pf.Views()[idx]))
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
		s.renderFragment(w, "playthrough_fragments.html", "sessions_table", view)
	}

	if err := pf.RemoveSession(idx, j); err != nil {
		fail(err)
		return
	}
	if err := mutate.WriteEntry(pf, model.RemoveSessionAllowedPaths(idx, before, j)...); err != nil {
		fail(err)
		return
	}
	s.renderFragment(w, "playthrough_fragments.html", "sessions_table", buildPthUpdateView(slug, doc.FM.Platform, doc.FM.Subgames, pf.Views()[idx]))
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
		s.renderFragment(w, "playthrough_fragments.html", "sessions_table", view)
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
	s.renderFragment(w, "playthrough_fragments.html", "sessions_table", buildPthUpdateView(slug, doc.FM.Platform, doc.FM.Subgames, pf.Views()[idx]))
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
	if j < len(pf.Playthroughs[idx].Sessions) {
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
		redirectErr(w, r, gamePath(slug), fmt.Errorf("%s already has a planned replay on %s", doc.FM.Title, model.OrDash(want)))
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
		redirectErr(w, r, gamePath(slug), fmt.Errorf("no provider id set (retroachievements_id, steam_appid, psn_id, ubisoft_id, xbox_id or googleplay_id)"))
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
			parts = append(parts, fmt.Sprintf("%s: %s", sv.Label(), commands.DescribeUnlocks(p)))
		}
	}
	sort.Strings(parts)
	redirectOK(w, r, gamePath(slug), "Refreshed — "+strings.Join(parts, "; "))
}

// ── Per-game Trackmania / BTD6 refresh ──────────────────────────────────
//
// Neither is an achievement provider, so neither rides the achievements
// button. Trackmania fetches the seasons ticked in the page's picker as a
// background job: every request is throttled and made against a real
// player's account, and a backfill of several seasons takes a while. Only the
// current season is ticked by default. Ninja Kiwi is a single save request
// and runs inline.

func (s *server) handleRefreshNadeo(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	doc, _, ok := s.gameOr404(w, r, slug)
	if !ok {
		return
	}
	if doc.NadeoAccountIDString() == "" {
		redirectErr(w, r, gamePath(slug), fmt.Errorf("this game has no nadeo_account_id"))
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	rows, _ := commands.NadeoSeasons(s.gamesDir) // an unreadable list still allows "current season"
	opts, n, err := nadeoOptionsFromForm(r.Form, rows)
	if err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}

	s.nadeoMu.Lock()
	defer s.nadeoMu.Unlock()
	if s.jobs.running(s.nadeoJobID) {
		http.Redirect(w, r, "/jobs/"+s.nadeoJobID, http.StatusSeeOther)
		return
	}
	title := "Trackmania: fetch current season"
	if n > 1 || len(opts.Seasons) > 0 {
		title = fmt.Sprintf("Trackmania: fetch %d season%s", n, pluralS(n))
	}
	id, j := s.jobs.create(title, gamePath(slug), "Back to "+doc.FM.Title)
	j.Nav = "games"
	s.nadeoJobID = id
	go s.runNadeoJob(j, opts)
	http.Redirect(w, r, "/jobs/"+id, http.StatusSeeOther)
}

// runningNadeoJob is the id of a Trackmania fetch still in flight, or "".
func (s *server) runningNadeoJob() string {
	s.nadeoMu.Lock()
	defer s.nadeoMu.Unlock()
	if s.jobs != nil && s.jobs.running(s.nadeoJobID) {
		return s.nadeoJobID
	}
	return ""
}

// nadeoOptionsFromForm turns the season picker's ticks into fetch options,
// and how many seasons that is. Ticking nothing is an error rather than
// quietly meaning "current season": the zero options fetch the current
// season, so an empty form must not reach them by accident. A season UID the
// picker didn't offer is refused before any request is made.
func nadeoOptionsFromForm(form url.Values, rows []commands.NadeoSeason) (nadeo.FetchOptions, int, error) {
	current := form.Get("current") == "1"
	known := make(map[string]bool, len(rows))
	for _, row := range rows {
		known[row.SeasonUID] = true
	}
	var opts nadeo.FetchOptions
	for _, uid := range form["season"] {
		if !known[uid] {
			return nadeo.FetchOptions{}, 0, fmt.Errorf("unknown season %q", uid)
		}
		if !slices.Contains(opts.Seasons, uid) {
			opts.Seasons = append(opts.Seasons, uid)
		}
	}
	switch {
	case len(opts.Seasons) == 0 && !current:
		return nadeo.FetchOptions{}, 0, fmt.Errorf("tick at least one season to fetch")
	case len(opts.Seasons) == 0:
		return nadeo.FetchOptions{}, 1, nil // the zero value is the current season
	}
	opts.Current = current
	n := len(opts.Seasons)
	if current {
		n++ // an upper bound: the current season may be one of the ticked ones
	}
	return opts, n, nil
}

// runNadeoJob is one season fetch: the same fetch → merge → project sequence
// as Catch up's Trackmania step, with the provider's progress events turned
// into the job's progress bar and log.
func (s *server) runNadeoJob(j *job, opts nadeo.FetchOptions) {
	// Not the request's context: that ends with the redirect to the monitor.
	ctx := context.Background()
	stageLabel := map[nadeo.Stage]string{
		nadeo.StageAuth:      "signing in",
		nadeo.StageCampaigns: "listing campaigns",
		nadeo.StageMaps:      "track details",
		nadeo.StageRecords:   "personal bests",
	}
	opts.OnEvent = func(e nadeo.Event) {
		switch {
		case e.Stage == nadeo.StageSeason:
			j.log("Captured %s", e.Detail)
		case e.Detail != "":
			j.log("%s", e.Detail)
		default:
			j.progress(e.Done, e.Total, stageLabel[e.Stage])
		}
	}

	res, err := commands.SaveNadeo(ctx, s.gamesDir, opts)
	if err != nil {
		j.finish(err)
		return
	}
	j.progress(1, 1, "")
	j.log("")
	if res.LastError != "" {
		j.log("%s (existing data kept)", res.LastError)
	}
	j.log("Archive now: %s", commands.DescribeNadeoRecord(res.Stored))
	if res.SummaryErr != nil {
		j.log("campaigns.yaml not updated: %v", res.SummaryErr)
	}
	j.finish(nil)
}

func (s *server) handleRefreshNinjaKiwi(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	doc, _, ok := s.gameOr404(w, r, slug)
	if !ok {
		return
	}
	if doc.NinjaKiwiUserIDString() == "" {
		redirectErr(w, r, gamePath(slug), fmt.Errorf("this game has no ninjakiwi_user_id"))
		return
	}
	res, err := commands.SaveNinjaKiwi(r.Context(), s.gamesDir)
	if err != nil {
		redirectErr(w, r, gamePath(slug), err)
		return
	}
	redirectRefreshed(w, r, slug, commands.DescribeNinjaKiwiRecord(res.Stored), res.LastError, res.SummaryErr, "btd6-summary.yaml")
}

// redirectRefreshed flashes a refresh's outcome. A fetch failure still saved
// (only the attempt metadata changed), so it's reported as an error flash
// that says the archive was kept rather than as a failed request.
func redirectRefreshed(w http.ResponseWriter, r *http.Request, slug, summary, lastError string, summaryErr error, projection string) {
	if lastError != "" {
		redirectErr(w, r, gamePath(slug), fmt.Errorf("%s (existing data kept) — archive now: %s", lastError, summary))
		return
	}
	msg := "Refreshed — " + summary
	if summaryErr != nil {
		msg += fmt.Sprintf(" (%s not updated: %v)", projection, summaryErr)
	}
	redirectOK(w, r, gamePath(slug), msg)
}
