package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// defaultMinHours is the Steam playtime floor for a game to be worth
// suggesting. A Steam library is mostly games that were never really played;
// this keeps the list to things there's plausibly something to say about.
const defaultMinHours = 5

// Candidate is one game found on an external service that isn't in
// content/games yet.
type Candidate struct {
	Provider string // "RetroAchievements" or "Steam"
	Title    string
	Platform string
	ID       string // retroachievements_id or steam_appid

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
}

// SortKey orders candidates so the ones most likely worth logging come first:
// finished games, then by how much evidence there is.
func (c Candidate) sortWeight() int {
	if c.Finished {
		return 0
	}
	return 1
}

// NewGameFields maps a discovered game onto a new content file. Always a
// draft: everything here is inferred, so it stays off the built site until
// it's been looked at.
func (c Candidate) NewGameFields() NewGameFields {
	f := NewGameFields{
		Title:    c.Title,
		Platform: c.Platform,
		Status:   c.Status,
		Finished: c.FinishedOn,
		Draft:    true,
	}
	if c.Provider == "Steam" {
		f.SteamAppID = c.ID
	} else {
		f.RetroAchievementsID = c.ID
	}
	return f
}

type ScanOptions struct {
	MinHours float64
}

func runScan(args []string) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	minHours := fs.Float64("min-hours", defaultMinHours, "minimum Steam playtime, in hours, for a game to be suggested")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: gamelog scan [flags]\n\nFinds games on RetroAchievements/Steam that aren't in content/games yet.\n\nFlags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	gamesDir, err := findGamesDir()
	if err != nil {
		return err
	}
	existing, err := ListGames(gamesDir)
	if err != nil {
		return err
	}
	creds := loadCredentials()

	if !creds.RAConfigured() && !creds.SteamConfigured() {
		return fmt.Errorf("no credentials configured; see tools/gamelog/README.md")
	}

	ctx := context.Background()
	opts := ScanOptions{MinHours: *minHours}
	index := newLoggedIndex(existing)

	var candidates []Candidate
	if creds.RAConfigured() {
		found, err := scanRA(ctx, creds, index)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gamelog: RetroAchievements scan failed: %v\n", err)
		} else {
			candidates = append(candidates, found...)
		}
	} else {
		fmt.Fprintln(os.Stderr, "gamelog: skipping RetroAchievements (RA_USERNAME/RA_API_KEY not set)")
	}

	if creds.SteamConfigured() {
		found, err := scanSteam(ctx, creds, index, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gamelog: Steam scan failed: %v\n", err)
		} else {
			candidates = append(candidates, found...)
		}
	} else {
		fmt.Fprintln(os.Stderr, "gamelog: skipping Steam (STEAM_API_KEY/STEAM_ID not set)")
	}

	sortCandidates(candidates)
	fmt.Print(formatScanReport(candidates, len(existing), opts))
	if len(candidates) == 0 {
		return nil
	}
	return offerToCreate(gamesDir, candidates)
}

// loggedIndex answers "is this game already in content/games?". External IDs
// are the reliable signal, but existing entries predate those fields, so a
// normalised title is the fallback.
type loggedIndex struct {
	raIDs    map[string]bool
	steamIDs map[string]bool
	titles   map[string]bool
}

func newLoggedIndex(games []GameSummary) loggedIndex {
	idx := loggedIndex{
		raIDs:    map[string]bool{},
		steamIDs: map[string]bool{},
		titles:   map[string]bool{},
	}
	for _, g := range games {
		if g.RAGameID != "" {
			idx.raIDs[g.RAGameID] = true
		}
		if g.SteamAppID != "" {
			idx.steamIDs[g.SteamAppID] = true
		}
		idx.titles[normalizeTitle(g.Title)] = true
		idx.titles[normalizeTitle(g.Slug)] = true
	}
	return idx
}

func (i loggedIndex) hasRA(id, title string) bool {
	return i.raIDs[id] || i.titles[normalizeTitle(title)]
}

func (i loggedIndex) hasSteam(id, title string) bool {
	return i.steamIDs[id] || i.titles[normalizeTitle(title)]
}

var titleNoise = regexp.MustCompile(`[^a-z0-9]+`)

// normalizeTitle reduces a title to a comparable form, so "Hades II" matches
// "hades-ii" and "Elden Ring®" matches "Elden Ring".
func normalizeTitle(s string) string {
	return strings.Trim(titleNoise.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

func scanRA(ctx context.Context, creds Credentials, index loggedIndex) ([]Candidate, error) {
	client := &RAClient{Username: creds.RAUsername, APIKey: creds.RAAPIKey}
	played, err := client.GetUserCompletionProgress(ctx)
	if err != nil {
		return nil, err
	}

	var out []Candidate
	for _, g := range played {
		id := strconv.Itoa(g.GameID)
		if index.hasRA(id, g.Title) {
			continue
		}
		c := Candidate{
			Provider:      "RetroAchievements",
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
			c.Status = "finished"
		} else {
			c.Status = "playing"
		}
		out = append(out, c)
	}
	return out, nil
}

func scanSteam(ctx context.Context, creds Credentials, index loggedIndex, opts ScanOptions) ([]Candidate, error) {
	client := &SteamClient{APIKey: creds.SteamAPIKey, SteamID: creds.SteamID}
	owned, err := client.GetOwnedGames(ctx)
	if err != nil {
		return nil, err
	}

	minMinutes := int(opts.MinHours * 60)
	var out []Candidate
	for _, g := range owned {
		if g.PlaytimeMins < minMinutes {
			continue
		}
		id := strconv.Itoa(g.AppID)
		if index.hasSteam(id, g.Name) {
			continue
		}
		c := Candidate{
			Provider:     "Steam",
			Title:        g.Name,
			Platform:     "PC",
			ID:           id,
			PlaytimeMins: g.PlaytimeMins,
			// Steam has no completion signal, so a scanned Steam game is
			// never marked finished on the strength of playtime alone.
			Status: "playing",
		}
		if g.LastPlayed > 0 {
			c.LastActivity = day(time.Unix(g.LastPlayed, 0))
		}
		out = append(out, c)
	}
	return out, nil
}

// raDay renders an RFC3339 award date as a calendar day in the site's
// timezone, or "" if it can't be parsed.
func raDay(s string) string {
	t, err := parseRAAwardDate(s)
	if err != nil {
		return ""
	}
	return day(t)
}

func sortCandidates(candidates []Candidate) {
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

func formatScanReport(candidates []Candidate, numLogged int, opts ScanOptions) string {
	var b strings.Builder

	if len(candidates) == 0 {
		fmt.Fprintf(&b, "No unlogged games found (%d already logged).\n", numLogged)
		return b.String()
	}

	fmt.Fprintf(&b, "Found %d game%s not in content/games (%d already logged, Steam filtered to >=%gh)\n\n",
		len(candidates), plural(len(candidates)), numLogged, opts.MinHours)

	for _, c := range candidates {
		fmt.Fprintf(&b, "  %s\n", c.Title)
		fmt.Fprintf(&b, "    %s %s · %s\n", c.Provider, c.ID, c.Platform)

		var facts []string
		if c.AchievementsB > 0 {
			facts = append(facts, fmt.Sprintf("%d/%d achievements", c.AchievementsA, c.AchievementsB))
		}
		if c.PlaytimeMins > 0 {
			facts = append(facts, formatHours(c.PlaytimeMins))
		}
		if c.LastActivity != "" {
			facts = append(facts, "last active "+c.LastActivity)
		}
		if len(facts) > 0 {
			fmt.Fprintf(&b, "    %s\n", strings.Join(facts, " · "))
		}

		if c.Finished {
			// Name the award: "12/189 achievements" alongside "finished"
			// only makes sense once you can see it was a beaten award
			// rather than a mastery.
			fmt.Fprintf(&b, "    -> finished %s (%s)\n", c.FinishedOn, c.AwardKind)
		}
		b.WriteString("\n")
	}

	b.WriteString("Scanning uses bulk data only, so there are no start dates here — create an\n")
	b.WriteString("entry, then run `gamelog suggest <slug>` for its precise range.\n\n")
	b.WriteString("The dates and statuses above are guesses. Anything created is marked\n")
	b.WriteString("`draft: true`, so nothing publishes until you've reviewed it.\n\n")
	return b.String()
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// offerToCreate lets the user pick which discovered games to write out. This
// is the only part of scan that touches the filesystem, and it always asks
// first.
func offerToCreate(gamesDir string, candidates []Candidate) error {
	chosen, err := SelectCandidates(candidates)
	if err != nil {
		return err
	}
	if len(chosen) == 0 {
		fmt.Println("Nothing created.")
		return nil
	}

	creds := loadCredentials()
	ctx := context.Background()
	archiveDir := findArchiveDir(gamesDir)

	var created, skipped int
	for _, i := range chosen {
		c := candidates[i]
		path, err := CreateGameFile(gamesDir, Slugify(c.Title), c.NewGameFields())
		if err != nil {
			fmt.Fprintf(os.Stderr, "  skipped %s: %v\n", c.Title, err)
			skipped++
			continue
		}
		fmt.Printf("  created %s\n", path)
		created++

		// Capture the unlock history into the archive. A failure here costs
		// the history, not the game, so it's reported and stepped over —
		// `gamelog achievements <slug>` can retry later.
		raID, steamAppID := "", ""
		if c.Provider == "Steam" {
			steamAppID = c.ID
		} else {
			raID = c.ID
		}
		if _, err := saveAchievements(ctx, archiveDir, c.Title, raID, steamAppID, creds); err != nil {
			fmt.Fprintf(os.Stderr, "    (no achievements saved: %v)\n", err)
		}
	}

	fmt.Printf("\n%d created, %d skipped. Review them, then flip `draft: false` to publish.\n", created, skipped)
	return nil
}
