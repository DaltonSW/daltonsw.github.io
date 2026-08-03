package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// GameSummary is a lightweight view over one content/games/<slug>/_index.md,
// used to build the game picker.
type GameSummary struct {
	Slug            string
	Path            string
	Title           string
	Status          string
	Draft           bool
	Started         string
	Finished        string
	NumPlaythroughs int
	RAGameID        string
	SteamAppID      string
	PSNID           string
}

// ProviderLinks returns every provider this game is linked to, in
// providerOrder, skipping the ones with no ID set.
func (g GameSummary) ProviderLinks() []providerLink {
	return buildProviderLinks(g.RAGameID, g.SteamAppID, g.PSNID)
}

func (g GameSummary) Label() string {
	switch g.Status {
	case "finished":
		return fmt.Sprintf("%s — finished %s", g.Title, g.Finished)
	case "mastered":
		return fmt.Sprintf("%s — mastered %s", g.Title, g.Finished)
	case "playing":
		return fmt.Sprintf("%s — playing since %s", g.Title, g.Started)
	default:
		return fmt.Sprintf("%s — %s", g.Title, g.Status)
	}
}

// SuggestLabel is Label plus which external services this game is linked to,
// so the `suggest` picker doesn't invite you to choose a game that can't
// produce a suggestion.
func (g GameSummary) SuggestLabel() string {
	var linked []string
	if g.RAGameID != "" {
		linked = append(linked, "RA")
	}
	if g.SteamAppID != "" {
		linked = append(linked, "Steam")
	}
	if g.PSNID != "" {
		linked = append(linked, "PSN")
	}
	if len(linked) == 0 {
		return g.Label() + "  [not linked]"
	}
	return fmt.Sprintf("%s  [%s]", g.Label(), strings.Join(linked, "+"))
}

// findGamesDir locates content/games, walking up from the current
// directory so the tool works whether it's launched from the repo root or
// a subdirectory of it.
func findGamesDir() (string, error) {
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
		raID, steamAppID := doc.ExternalIDs()
		psnID := scalarString(doc.FM.PSNID)
		// Playthroughs are their own file now; a game without one simply has
		// none logged, which is not worth skipping the game over.
		pf, err := LoadPlaythroughs(filepath.Dir(path))
		if err != nil {
			fmt.Fprintf(os.Stderr, "gamelog: skipping %s: %v\n", PlaythroughsPath(filepath.Dir(path)), err)
			pf = &PlaythroughsFile{}
		}
		out = append(out, GameSummary{
			Slug:            e.Name(),
			Path:            path,
			Title:           doc.FM.Title,
			Status:          doc.FM.Status,
			Draft:           doc.FM.Draft,
			Started:         doc.FM.Started,
			Finished:        doc.FM.Finished,
			NumPlaythroughs: len(pf.Playthroughs),
			RAGameID:        raID,
			SteamAppID:      steamAppID,
			PSNID:           psnID,
		})
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
	Title               string
	Platform            string
	RetroAchievementsID string
	SteamAppID          string
	PSNID               string
	Status              string // backlog|playing|finished|dropped
	Started             string
	Finished            string
	Rating              string
	Overview            string
	// Draft keeps a generated entry off the built site until it's been
	// reviewed. Only `scan` sets it; the interactive form never does.
	Draft bool
}

func formatNewGameFile(f NewGameFields, slug string) string {
	lines := trimTrailingBlanks([]string{
		"---",
		fmt.Sprintf("title: %q", f.Title),
		fmt.Sprintf("platform: %q", f.Platform),
		"retroachievements_id: " + f.RetroAchievementsID,
		"steam_appid: " + f.SteamAppID,
		"psn_id: " + f.PSNID,
		fmt.Sprintf("status: %q", f.Status),
		"started: " + f.Started,
		"finished: " + f.Finished,
		"rating: " + f.Rating,
		"cover:",
		fmt.Sprintf("draft: %t", f.Draft),
		"cascade:",
		"  params:",
		fmt.Sprintf("    games: [%q]", slug),
		"---",
		"",
	})
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
	return path, nil
}
