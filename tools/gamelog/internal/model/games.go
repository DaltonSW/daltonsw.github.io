package model

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// GameSummary is a lightweight view over one content/games/<slug>/_index.md,
// used to build the game picker.
type GameSummary struct {
	Slug   string
	Path   string
	Title  string
	Status string
	Draft  bool
	// Started/Finished are the span of every logged playthrough (earliest
	// date to latest), and FirstFinished is the day the first run with status
	// "finished" ended. All three are derived from playthroughs.yaml; a game's
	// front matter carries no dates.
	Started       string
	Finished      string
	FirstFinished string
	// StatusDerived reports that Status comes from the game's runs
	// (DeriveGameStatus) rather than being hand-set, so the forms show it
	// read-only — change a run to change it.
	StatusDerived   bool
	Platform        string
	Tags            []string // the game's own tags (FrontMatter.Tags)
	RunTags         []string // tags only some of its runs carry, beyond Tags
	Rating          string
	NumPlaythroughs int
	// IDs is this game's id on each provider it's linked to, keyed by provider
	// (ProviderOrder), blank ones left out.
	IDs map[string]string
	// NadeoAccountID links this game to a Trackmania campaign archive. It is
	// not part of ProviderLinks — see FrontMatter.NadeoAccountID.
	NadeoAccountID string
	// NinjaKiwiUserID links this game to a BTD6 save archive. It is not part
	// of ProviderLinks — see FrontMatter.NinjaKiwiUserID.
	NinjaKiwiUserID string
	// RASubsets are the RetroAchievements subset ids attached to this game
	// (see FrontMatter.RASubsets). They matter to the scan as well as to the
	// archive: a subset already attached here must stop being offered as an
	// unlogged game of its own.
	RASubsets []string
}

// ProviderLinks returns every provider this game is linked to, in
// ProviderOrder, skipping the ones with no ID set.
func (g GameSummary) ProviderLinks() []ProviderLink {
	return BuildProviderLinks(g.ProviderIDs(), g.RASubsets...)
}

// ProviderIDs is every provider id this game has set, keyed by provider —
// Doc.ProviderIDs' counterpart for a summary.
func (g GameSummary) ProviderIDs() map[string]string { return g.IDs }

// FindGamesDir locates content/games, walking up from the current
// directory so the tool works whether it's launched from the repo root or
// a subdirectory of it.
func FindGamesDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, "content", "games")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find content/games above %s", dir)
		}
		dir = parent
	}
}

// GameSummaryFor builds the GameSummary for one already-loaded game, the
// shared derivation ListGames uses per entry and the web UI reuses after a
// quick edit so a freshly-saved row reflects the exact same rules (e.g. the
// playthroughs-first started/finished fallback below) instead of a second,
// possibly-drifting copy of them.
func GameSummaryFor(slug, path string, doc *Doc, pf *PlaythroughsFile) GameSummary {
	// Every date is derived from playthroughs.yaml — see gameDateRange and
	// firstFinished. Front matter carries none.
	views := pf.Views()
	started, finished := gameDateRange(views)

	return GameSummary{
		Slug:            slug,
		Path:            path,
		Title:           doc.FM.Title,
		Status:          doc.FM.Status,
		Draft:           doc.FM.Draft,
		Started:         started,
		Finished:        finished,
		FirstFinished:   firstFinished(views),
		StatusDerived:   statusDerived(doc.FM.Status, views),
		Platform:        doc.FM.Platform,
		Tags:            doc.FM.Tags,
		RunTags:         runOnlyTags(doc.FM.Tags, views),
		Rating:          doc.FM.RatingString(),
		NumPlaythroughs: len(pf.Playthroughs),
		IDs:             doc.ProviderIDs(),
		NadeoAccountID:  doc.NadeoAccountIDString(),
		NinjaKiwiUserID: doc.NinjaKiwiUserIDString(),
		RASubsets:       doc.RASubsetIDs(),
	}
}

func statusDerived(current string, views []Playthrough) bool {
	_, ok := DeriveGameStatus(current, views)
	return ok
}

// runOnlyTags returns the tags runs carry that the game itself doesn't, in
// first-seen order.
func runOnlyTags(gameTags []string, views []Playthrough) []string {
	var out []string
	for _, p := range views {
		for _, t := range p.Tags {
			if !slices.Contains(gameTags, t) && !slices.Contains(out, t) {
				out = append(out, t)
			}
		}
	}
	return out
}

// KnownTags is every tag in use across games and their runs, sorted — what
// the forms offer as suggestions.
func KnownTags(games []GameSummary) []string {
	var out []string
	for _, g := range games {
		for _, t := range slices.Concat(g.Tags, g.RunTags) {
			if !slices.Contains(out, t) {
				out = append(out, t)
			}
		}
	}
	slices.Sort(out)
	return out
}

func ListGames(gamesDir string) ([]GameSummary, error) {
	entries, err := os.ReadDir(gamesDir)
	if err != nil {
		return nil, err
	}

	var out []GameSummary
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(gamesDir, e.Name(), "_index.md")
		if _, err := os.Stat(path); err != nil {
			continue
		}
		doc, err := LoadDoc(path)
		if err != nil {
			// One unparseable file shouldn't take down the whole picker —
			// skip it loudly and let the rest of the log stay editable.
			fmt.Fprintf(os.Stderr, "gamelog: skipping %s: %v\n", path, err)
			continue
		}
		// Playthroughs are their own file now; a game without one simply has
		// none logged, which is not worth skipping the game over.
		pf, err := LoadPlaythroughs(filepath.Dir(path))
		if err != nil {
			fmt.Fprintf(os.Stderr, "gamelog: skipping %s: %v\n", PlaythroughsPath(filepath.Dir(path)), err)
			pf = &PlaythroughsFile{}
		}

		out = append(out, GameSummaryFor(e.Name(), path, doc, pf))
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Title < out[j].Title })
	return out, nil
}

var slugNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// slugLigatures covers the letters Unicode decomposition can't reduce to
// ASCII: they have no combining-mark form, so NFD leaves them whole and the
// [^a-z0-9]+ pass would otherwise drop them entirely.
var slugLigatures = strings.NewReplacer(
	"ß", "ss", "æ", "ae", "œ", "oe", "ø", "o",
	"ł", "l", "đ", "d", "ð", "d", "þ", "th",
)

// Slugify converts a game title into its content directory name, which is
// also its permanent URL. Accents are folded rather than stripped, so
// "Ōkami" gives "okami" and "Pokémon Red" gives "pokemon-red" instead of
// losing the letter outright. Trademark and copyright signs are not letters
// and still fall away, leaving "ELDEN RING®" as "elden-ring".
func Slugify(title string) string {
	s := slugLigatures.Replace(strings.ToLower(title))
	// NFD splits an accented letter into its base plus a combining mark;
	// dropping the marks leaves the ASCII base behind.
	s = strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		return r
	}, norm.NFD.String(s))
	s = slugNonAlnum.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// validateSlug rejects slugs that would write somewhere other than a game's
// own directory. The empty slug is the dangerous one: it collapses
// filepath.Join(gamesDir, "", "_index.md") onto content/games/_index.md,
// the games section index page.
func validateSlug(slug string) error {
	switch {
	case slug == "":
		return fmt.Errorf("title has no letters or digits to build a slug from")
	case slug == "." || slug == "..":
		return fmt.Errorf("invalid slug %q", slug)
	case strings.ContainsAny(slug, `/\`):
		return fmt.Errorf("slug %q must not contain a path separator", slug)
	}
	return nil
}

// titleAt reads the title of a game that already occupies a slug. Distinct
// titles can collapse to the same one — "Hades: II" and "Hades II" both give
// "hades-ii" — so naming the occupant beats reporting a path collision.
// Falls back to the slug if the file won't parse.
func titleAt(path, slug string) string {
	doc, err := LoadDoc(path)
	if err != nil {
		return slug
	}
	if doc.FM.Title != "" {
		return doc.FM.Title
	}
	return slug
}

// trimTrailingBlanks strips the trailing space an empty field would otherwise
// leave behind, so `finished: ` is written as `finished:`.
func trimTrailingBlanks(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.TrimRight(l, " ")
	}
	return out
}

// NewGameFields holds the values for a brand-new game's _index.md.
type NewGameFields struct {
	Title    string
	Platform string
	// IDs is the game's id on each provider, keyed by provider; see SetProviderID.
	IDs    map[string]string
	Status string // backlog|playing|finished|dropped|unfinished
	// Subgames is the declared roster of a compilation's parts (see
	// FrontMatter.Subgames). Empty means this isn't a compilation.
	Subgames []string
	Tags     []string // see FrontMatter.Tags
	// Started/Finished aren't front matter: when either is set, CreateGameFile
	// writes them as the game's first playthrough entry.
	Started  string
	Finished string
	Rating   string
	Overview string
	// Draft keeps a generated entry off the built site until it's been
	// reviewed. Only `scan` sets it; the interactive form never does.
	Draft bool
}

// SetProviderID fills the id field for one provider — what the scan uses, since
// a candidate knows its provider as a key rather than as a field to set.
func (f *NewGameFields) SetProviderID(provider, id string) {
	if f.IDs == nil {
		f.IDs = map[string]string{}
	}
	f.IDs[provider] = id
}

func formatNewGameFile(f NewGameFields, slug string) string {
	lines := []string{
		"---",
		fmt.Sprintf("title: %q", f.Title),
		fmt.Sprintf("platform: %q", f.Platform),
	}
	for _, p := range providerTable {
		lines = append(lines, p.FMKey+": "+f.IDs[p.Key])
	}
	lines = append(lines,
		fmt.Sprintf("status: %q", f.Status),
		"subgames:",
	)
	for _, sg := range f.Subgames {
		lines = append(lines, fmt.Sprintf("  - %q", sg))
	}
	if len(f.Tags) > 0 {
		lines = append(lines, "tags:")
		for _, t := range f.Tags {
			lines = append(lines, fmt.Sprintf("  - %q", t))
		}
	}
	lines = append(lines,
		"rating: "+f.Rating,
		"cover:",
		fmt.Sprintf("draft: %t", f.Draft),
		"cascade:",
		"  params:",
		fmt.Sprintf("    games: [%q]", slug),
		"---",
		"",
	)
	lines = trimTrailingBlanks(lines)
	body := strings.TrimRight(f.Overview, "\n")
	return strings.Join(lines, "\n") + "\n" + body + "\n"
}

// CreateGameFile writes a new content/games/<slug>/_index.md. Refuses to
// overwrite an existing game, or to write outside one's own directory.
func CreateGameFile(gamesDir, slug string, f NewGameFields) (string, error) {
	if err := validateSlug(slug); err != nil {
		return "", err
	}
	dir := filepath.Join(gamesDir, slug)
	path := filepath.Join(dir, "_index.md")
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("slug %q is already used by %q", slug, titleAt(path, slug))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	content := formatNewGameFile(f, slug)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	if err := writeFirstPlaythrough(dir, f); err != nil {
		return path, fmt.Errorf("created %s, but its first playthrough wasn't written: %w", path, err)
	}
	return path, nil
}

// writeFirstPlaythrough records a new game's dates as its first playthrough,
// since a game's front matter carries none. A game with no dates (backlog,
// or a scan candidate with nothing to go on) gets no entry: an undated entry
// would only mark the game as played without saying when.
func writeFirstPlaythrough(dir string, f NewGameFields) error {
	if f.Started == "" && f.Finished == "" {
		return nil
	}
	status := f.Status
	switch status {
	case "backlog", "":
		status = "playing"
	case "software":
		// game-level only, so its entry says "playing" (the site reads it as software)
		status = "playing"
	}
	pf, err := LoadPlaythroughs(dir)
	if err != nil {
		return err
	}
	pf.AddPlaythrough(PlaythroughFields{Started: f.Started, Finished: f.Finished, Status: status})
	return pf.Save()
}
