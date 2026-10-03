package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.dalton.dog/gamelog/internal/model"
	"go.dalton.dog/gamelog/internal/providers/retroachievements"
	"go.dalton.dog/gamelog/internal/providers/steam"
)

// DefaultMinHours is the Steam playtime floor for a game to be worth
// suggesting. A Steam library is mostly games that were never really played;
// this keeps the list to things there's plausibly something to say about.
const DefaultMinHours = 5

// Candidate is one game found on an external service that isn't in
// content/games yet — or is, under its title, but isn't linked to this
// service (see LinkTo).
type Candidate struct {
	Provider string // the archive's provider key, e.g. model.ProviderSteam
	Title    string
	Platform string
	ID       string // the game's id on Provider — what its front-matter field holds

	// Whatever finish signal the provider offers. Finished means a real
	// completion award, not merely "hasn't been touched in a while".
	Finished  bool
	AwardKind string // what earned the finish, e.g. "beaten-hardcore"
	Status    string // suggested front-matter status

	// FinishedOn is the award date, or "" when there's no finish signal.
	// There's deliberately no start date: the bulk endpoints don't carry
	// one, and fetching it per game is what `suggest` is for.
	FinishedOn string

	// Context for the report, never written to front matter.
	LastActivity  string
	PlaytimeMins  int
	AchievementsA int // earned
	AchievementsB int // total

	// Subset is set when this candidate is a RetroAchievements subset rather
	// than a game of its own — nil for everything else, which is nearly
	// everything. See SubsetInfo and ResolveSubsets.
	Subset *SubsetInfo

	// LinkTo is set when a logged game already has this candidate's title
	// but no id on this provider — Balatro logged from Steam, found again on
	// Google Play. The row then offers to link the id to that game rather
	// than create a second entry for it. Before this, a title match just hid
	// the row, which left the second service's history uncaptured for good.
	LinkTo *LinkTarget

	// Note is anything else the row has to explain, e.g. that a logged game
	// shares this title but is already linked to a different id here.
	Note string
}

// LinkTarget is the logged game a candidate would be linked to.
type LinkTarget struct {
	Slug, Title string
}

// IsLink reports whether this candidate's primary action is linking it to a
// logged game rather than creating one.
func (c Candidate) IsLink() bool { return c.LinkTo != nil }

// IsSubset reports whether this candidate belongs under an existing game
// rather than becoming one.
func (c Candidate) IsSubset() bool { return c.Subset != nil }

// sortWeight orders candidates so the ones most likely worth acting on come
// first: links (history for a game already on the site), then finished
// games, then by how much evidence there is.
func (c Candidate) sortWeight() int {
	switch {
	case c.IsLink():
		return 0
	case c.Finished:
		return 1
	}
	return 2
}

// ProviderKey is Provider, kept as a method because the templates and the
// create/ignore forms key rows on it.
func (c Candidate) ProviderKey() string { return c.Provider }

// ProviderLabel is Provider as a reader knows it.
func (c Candidate) ProviderLabel() string { return model.ProviderLabel(c.Provider) }

// WithStatus is the "create as X instead" correction: the same candidate as
// it would be created under a status chosen by hand rather than guessed.
//
// Overriding to a status that doesn't assert the game is over drops the
// inferred finish date with it. The date came from the award that justified
// the guess; keeping it under `status: playing` would write a self-
// contradicting entry — exactly the kind of inferred-then-forgotten field
// the draft flag exists to catch.
func (c Candidate) WithStatus(status string) Candidate {
	if status == "" || status == c.Status {
		return c
	}
	c.Status = status
	if !doneStatuses[status] {
		c.Finished = false
		c.FinishedOn = ""
	}
	return c
}

// NewGameFields maps a discovered game onto a new content file. Always a
// draft: everything here is inferred, so it stays off the built site until
// it's been looked at.
func (c Candidate) NewGameFields() model.NewGameFields {
	f := model.NewGameFields{
		Title:    c.Title,
		Platform: c.Platform,
		Status:   c.Status,
		Finished: c.FinishedOn,
		Draft:    true,
	}
	f.SetProviderID(c.Provider, c.ID)
	return f
}

// ScanMode selects which half of the Steam library a scan is interested in.
// The two modes partition the library on the same MinHours boundary, so a
// game can never show up under both — there's one threshold to reason about,
// not two that can drift out of step.
type ScanMode int

const (
	// ScanModePlayed keeps games at or above MinHours: things there's
	// plausibly something to say about. The zero value, so an unset Mode
	// means the original scan behaviour.
	ScanModePlayed ScanMode = iota
	// ScanModeBacklog keeps everything below MinHours: owned, effectively
	// untouched, and therefore backlog rather than history.
	ScanModeBacklog
)

type ScanOptions struct {
	MinHours float64
	Mode     ScanMode
}

// keeps reports whether a game with this much recorded playtime belongs to
// the half of the library these options select. The threshold itself counts
// as played, so the two modes are a strict partition — no game is dropped by
// both, none is offered by both.
func (o ScanOptions) keeps(playtimeMins int) bool {
	below := playtimeMins < int(o.MinHours*60)
	return below == (o.Mode == ScanModeBacklog)
}

// loggedIndex answers "what should the scan do with this game?" — leave it
// out (already linked, or explicitly ignored), offer to link it to a logged
// game with the same title, or offer to create it.
type loggedIndex struct {
	// ids is every provider id already in front matter, by provider. A
	// game's attached RA subsets count as covered under ProviderRA.
	ids map[string]map[string]bool

	// titles maps a normalised title (and slug) to the games carrying it.
	// Existing entries predate the id fields, so a title is how a game
	// logged one way is recognised when it turns up on another service.
	titles map[string][]model.GameSummary

	// raGames maps a game's *own* RetroAchievements id to it, so a subset can
	// be traced to the base game it belongs under. Subset ids are deliberately
	// not keys here (they are in ids, which is the "already covered?"
	// question): a subset is not a game a further subset could hang off.
	raGames map[string]model.GameSummary

	// ignored is matched on provider ID only. Every entry is created from a
	// candidate that had one, so unlike logged games there's no legacy case
	// needing the title fallback — and matching titles here would risk
	// silently hiding a different game that happens to share a name.
	ignored model.IgnoredList
}

func NewLoggedIndex(games []model.GameSummary) loggedIndex {
	idx := loggedIndex{
		ids:     map[string]map[string]bool{},
		titles:  map[string][]model.GameSummary{},
		raGames: map[string]model.GameSummary{},
	}
	cover := func(provider, id string) {
		if idx.ids[provider] == nil {
			idx.ids[provider] = map[string]bool{}
		}
		idx.ids[provider][id] = true
	}
	for _, g := range games {
		for p, id := range g.ProviderIDs() {
			cover(p, id)
		}
		if g.RAGameID != "" {
			idx.raGames[g.RAGameID] = g
		}
		// An attached subset is covered by the game it hangs off, so the scan
		// must stop offering it as an unlogged game of its own — the whole
		// point of attaching it.
		for _, id := range g.RASubsets {
			cover(model.ProviderRA, id)
		}
		keys := []string{normalizeTitle(g.Title)}
		if k := normalizeTitle(g.Slug); k != keys[0] {
			keys = append(keys, k)
		}
		for _, k := range keys {
			idx.titles[k] = append(idx.titles[k], g)
		}
	}
	return idx
}

// WithIgnored returns the index with an ignore list applied. Separate from
// NewLoggedIndex so callers that don't have an archive directory to hand
// (tests, chiefly) keep the plain behaviour.
func (i loggedIndex) WithIgnored(list model.IgnoredList) loggedIndex {
	i.ignored = list
	return i
}

// covered reports whether a game should stay out of the scan entirely: its id
// is already in some game's front matter, or it's been ignored.
func (i loggedIndex) covered(provider, id string) bool {
	return i.ids[provider][id] || i.ignored.Has(provider, id)
}

// place fills in what the scan should offer for an uncovered candidate: a
// link when a logged game has its title and no id on its provider, and
// otherwise an ordinary create — with a note when the title is taken by a game
// already linked to a different id here (a PS4 and a PS5 trophy set are two
// ids for one game; two different games can also share a name — only a human
// can tell which this is, so the row says so rather than hiding it).
func (i loggedIndex) place(c *Candidate) {
	for _, g := range i.titles[normalizeTitle(c.Title)] {
		if g.ProviderIDs()[c.Provider] == "" {
			c.LinkTo = &LinkTarget{Slug: g.Slug, Title: g.Title}
			return
		}
	}
	for _, g := range i.titles[normalizeTitle(c.Title)] {
		c.Note = fmt.Sprintf("%s has this title but is already linked to %s %s",
			g.Slug, model.ProviderLabel(c.Provider), g.ProviderIDs()[c.Provider])
		return
	}
}

// normalizeTitle reduces a title to a comparable form, so "Hades II" matches
// "hades-ii", "Elden Ring®" matches "Elden Ring", and Exophase's "Assassins
// Creed Odyssey" matches the logged "Assassin's Creed Odyssey". It folds
// accents and symbols the way slugs do (model.Slugify) and then drops the
// separators too, since services disagree about apostrophes and punctuation
// far more often than two different games differ only by them.
func normalizeTitle(s string) string {
	return strings.ReplaceAll(model.Slugify(s), "-", "")
}

func ScanRA(ctx context.Context, creds Credentials, index loggedIndex) ([]Candidate, error) {
	played, err := cachedList("ra:"+creds.RAUsername, func() ([]retroachievements.RAPlayedGame, error) {
		client := &retroachievements.RAClient{Username: creds.RAUsername, APIKey: creds.RAAPIKey}
		return client.GetUserCompletionProgress(ctx)
	})
	if err != nil {
		return nil, err
	}

	var out []Candidate
	for _, g := range played {
		id := strconv.Itoa(g.GameID)
		if index.covered(model.ProviderRA, id) {
			continue
		}
		c := Candidate{
			Provider:      model.ProviderRA,
			Title:         g.Title,
			Platform:      g.ConsoleName,
			ID:            id,
			Finished:      g.Finished(),
			AchievementsA: g.NumAwarded,
			AchievementsB: g.MaxPossible,
			LastActivity:  raDay(g.MostRecentDate),
		}
		if c.Finished {
			c.FinishedOn = raDay(g.HighestAwardDate)
			c.AwardKind = strings.ToLower(g.HighestAwardKind)
			c.Status = statusForAward(c.AwardKind)
		} else {
			c.Status = "playing"
		}
		// The title convention is all this endpoint carries — see
		// model.SplitRASubsetTitle. ResolveSubsets turns it into a real
		// parent id; until then BaseTitle is a guess, good for a label and
		// nothing else.
		if base, name, ok := model.SplitRASubsetTitle(g.Title); ok {
			c.Subset = &SubsetInfo{Name: name, BaseTitle: base}
		} else {
			index.place(&c)
		}
		out = append(out, c)
	}
	return out, nil
}

// statusForAward maps an RA HighestAwardKind onto a front-matter status —
// mastered/completed means every achievement was earned, not just beaten.
func statusForAward(kind string) string {
	if kind == "mastered" || kind == "completed" {
		return "mastered"
	}
	return "finished"
}

func ScanSteam(ctx context.Context, creds Credentials, index loggedIndex, opts ScanOptions) ([]Candidate, error) {
	owned, err := cachedList("steam:"+creds.SteamID, func() ([]steam.SteamOwnedGame, error) {
		client := &steam.SteamClient{APIKey: creds.SteamAPIKey, SteamID: creds.SteamID}
		return client.GetOwnedGames(ctx)
	})
	if err != nil {
		return nil, err
	}

	backlog := opts.Mode == ScanModeBacklog
	var out []Candidate
	for _, g := range owned {
		if !opts.keeps(g.PlaytimeMins) {
			continue
		}
		id := strconv.Itoa(g.AppID)
		if index.covered(model.ProviderSteam, id) {
			continue
		}
		c := Candidate{
			Provider:     model.ProviderSteam,
			Title:        g.Name,
			Platform:     "PC",
			ID:           id,
			PlaytimeMins: g.PlaytimeMins,
			// Steam has no completion signal, so a scanned Steam game is
			// never marked finished on the strength of playtime alone.
			Status: "playing",
		}
		if backlog {
			// Owned but not really played: that's the backlog, and there's
			// no finish signal to guess from either way.
			c.Status = "backlog"
		}
		if g.LastPlayed > 0 {
			c.LastActivity = day(time.Unix(g.LastPlayed, 0))
		}
		index.place(&c)
		out = append(out, c)
	}
	return out, nil
}

// raDay renders an RFC3339 award date as a calendar day in the site's
// timezone, or "" if it can't be parsed.
func raDay(s string) string {
	t, err := retroachievements.ParseRAAwardDate(s)
	if err != nil {
		return ""
	}
	return day(t)
}

func SortCandidates(candidates []Candidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.sortWeight() != b.sortWeight() {
			return a.sortWeight() < b.sortWeight()
		}
		if a.Provider != b.Provider {
			return a.Provider < b.Provider
		}
		if a.PlaytimeMins != b.PlaytimeMins {
			return a.PlaytimeMins > b.PlaytimeMins
		}
		return a.Title < b.Title
	})
}

// Evidence is what's known about a candidate from the bulk endpoints, as one
// line for the scan table: achievements, playtime, last activity. Empty when
// the provider offered none of the three.
//
// This is the row's whole justification for existing, so it's built here
// rather than assembled in the template — the "· " joining is conditional in
// a way template `if` chains get unreadable about.
func (c Candidate) Evidence() string {
	var facts []string
	if c.AchievementsB > 0 {
		facts = append(facts, fmt.Sprintf("%d/%d achievements", c.AchievementsA, c.AchievementsB))
	}
	if c.PlaytimeMins > 0 {
		facts = append(facts, FormatHours(c.PlaytimeMins))
	}
	if c.LastActivity != "" {
		facts = append(facts, "last active "+c.LastActivity)
	}
	return strings.Join(facts, " · ")
}

// Outcome is the finish this candidate would be created with, or "" when
// there isn't one. The award kind is named because "12/189 achievements ->
// finished" only makes sense once you can see it was `beaten-hardcore`
// rather than a mastery.
func (c Candidate) Outcome() string {
	if !c.Finished {
		return ""
	}
	out := c.Status
	if c.FinishedOn != "" {
		out += " " + c.FinishedOn
	}
	if c.AwardKind != "" {
		out += " (" + c.AwardKind + ")"
	}
	return out
}

// CreatedGame is one game CreateFromCandidates made, for a caller (the web
// UI) that wants to report more than a bare count.
type CreatedGame struct {
	Title, Path string
}

// CreateFromCandidates is offerToCreate's no-prompt half: given a set of
// already-chosen indexes into candidates, create each one's game file and
// capture its unlock history into the archive. Split out so a web handler
// can reuse it with a selection that came from a submitted checkbox form
// instead of SelectCandidates' huh multi-select.
//
// status overrides the guessed status for every game created in this call —
// the "create as X instead" correction. Blank keeps each candidate's own
// guess. Callers must have validated it (forms.IsGameStatus): it goes
// straight into front matter.
//
// creds is passed in rather than loaded here so a caller that already has them
// doesn't re-read .env mid-operation — and so a test can hand over empty ones
// and get a local, offline run instead of silently reaching the live APIs
// through whatever .env happens to be above the working directory.
func CreateFromCandidates(gamesDir string, candidates []Candidate, chosen []int, status string, creds Credentials) (createdList []CreatedGame, skipped int) {
	ctx := context.Background()
	archiveDir := model.FindArchiveDir(gamesDir)

	for _, i := range chosen {
		c := candidates[i].WithStatus(status)
		path, err := model.CreateGameFile(gamesDir, model.Slugify(c.Title), c.NewGameFields())
		if err != nil {
			fmt.Fprintf(os.Stderr, "  skipped %s: %v\n", c.Title, err)
			skipped++
			continue
		}
		fmt.Printf("  created %s\n", path)
		createdList = append(createdList, CreatedGame{Title: c.Title, Path: path})

		// Capture the unlock history into the archive. A failure here costs
		// the history, not the game, so it's reported and stepped over —
		// `gamelog achievements <slug>` can retry later.
		links := []model.ProviderLink{{Provider: c.Provider, ID: c.ID}}
		if _, err := SaveAchievements(ctx, archiveDir, c.Title, links, creds); err != nil {
			fmt.Fprintf(os.Stderr, "    (no achievements saved: %v)\n", err)
		} else if _, err := model.WriteAchievementSummary(archiveDir, filepath.Dir(path), links); err != nil {
			fmt.Fprintf(os.Stderr, "    (achievement summary not written: %v)\n", err)
		}
	}
	return createdList, skipped
}
