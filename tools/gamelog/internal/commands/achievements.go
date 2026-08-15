package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/charmbracelet/huh"

	"go.dalton.dog/gamelog/internal/forms"
	"go.dalton.dog/gamelog/internal/model"
	"go.dalton.dog/gamelog/internal/mutate"
	"go.dalton.dog/gamelog/internal/providers/exophase"
	"go.dalton.dog/gamelog/internal/providers/psn"
	"go.dalton.dog/gamelog/internal/providers/retroachievements"
	"go.dalton.dog/gamelog/internal/providers/steam"
	"go.dalton.dog/gamelog/internal/providers/xbox"
)

// raRecentCache memoizes the user-level GetUserRecentlyPlayedGames list for
// the process — at RA's ~1.2s throttle, re-walking its pages per game would
// dominate an `--all` run.
var raRecentCache = struct {
	sync.Mutex
	byUser map[string]map[string]*retroachievements.RARecentGame
}{byUser: map[string]map[string]*retroachievements.RARecentGame{}}

// raRecentlyPlayed returns last-played dates keyed by RA game id, fetching
// them on first use. A failure isn't fatal: last_played just falls back to the
// newest unlock, as it did before this existed.
func raRecentlyPlayed(ctx context.Context, client *retroachievements.RAClient, username string) map[string]*retroachievements.RARecentGame {
	raRecentCache.Lock()
	defer raRecentCache.Unlock()
	if byID, ok := raRecentCache.byUser[username]; ok {
		return byID
	}

	games, err := client.GetRecentlyPlayed(ctx)
	if err != nil {
		// Cached anyway, so this warns once per run rather than per game.
		fmt.Fprintf(os.Stderr, "  (retroachievements last-played unavailable: %v)\n", err)
	}
	byID := map[string]*retroachievements.RARecentGame{}
	for i := range games {
		g := games[i]
		byID[g.ID()] = &g
	}
	raRecentCache.byUser[username] = byID
	return byID
}

// savedRecord is one provider's outcome from a fetch-and-archive run.
type savedRecord struct {
	Provider string
	ID       string
	Subset   bool
	Path     string
	Record   *model.ArchiveRecord
}

// Label names the record in a progress line. Every link on a game reports the
// same provider name until subsets exist, at which point "retroachievements"
// three times over says nothing about which set moved.
func (s savedRecord) Label() string {
	if !s.Subset {
		return s.Provider
	}
	name := s.ID
	if s.Record != nil {
		if _, sub, ok := model.SplitRASubsetTitle(s.Record.Title); ok {
			name = sub
		}
	}
	return s.Provider + " subset " + name
}

// saveAchievements fetches from every configured provider the game is linked
// to — not just the first — and merges each result into its own archive file.
//
// It walks links in order rather than folding them into one id per provider,
// because a RetroAchievements subset is a second RA id on the same game (see
// model.FrontMatter.RASubsets). Order is BuildProviderLinks' order, which is
// ProviderOrder with each game's subsets behind its base RA set.
//
// An error aborts the rest of the walk with whatever was already written left
// in place. That's safe, and better than discarding it: every write is a merge
// that can only add (see model.mergeProvider), so a half-finished refresh is
// simply a refresh of fewer sets.
func SaveAchievements(ctx context.Context, archiveDir, title string, links []model.ProviderLink, creds Credentials) ([]savedRecord, error) {
	var (
		raClient    *retroachievements.RAClient
		steamClient *steam.SteamClient
		steamOwned  []steam.SteamOwnedGame
		saved       []savedRecord
	)

	for _, link := range links {
		if link.ID == "" {
			continue
		}
		var (
			rec *model.ProviderRecord
			err error
		)
		switch link.Provider {
		case model.ProviderRA:
			if !creds.RAConfigured() {
				continue
			}
			if raClient == nil {
				raClient = &retroachievements.RAClient{Username: creds.RAUsername, APIKey: creds.RAAPIKey}
			}
			rec, err = retroachievements.FetchRecord(ctx, raClient, link.ID,
				raRecentlyPlayed(ctx, raClient, creds.RAUsername)[link.ID])
		case model.ProviderSteam:
			if !creds.SteamConfigured() {
				continue
			}
			if steamClient == nil {
				steamClient = &steam.SteamClient{APIKey: creds.SteamAPIKey, SteamID: creds.SteamID}
				steamOwned, _ = steamClient.GetOwnedGames(ctx)
			}
			var owned *steam.SteamOwnedGame
			for i := range steamOwned {
				if strconv.Itoa(steamOwned[i].AppID) == link.ID {
					owned = &steamOwned[i]
					break
				}
			}
			rec, err = steam.FetchRecord(ctx, steamClient, link.ID, owned)
		case model.ProviderPSN:
			if !creds.PSNConfigured() {
				continue
			}
			rec, err = psn.FetchRecord(ctx, &psn.PSNClient{Npsso: creds.PSNNpsso}, link.ID)
		case model.ProviderUbisoft:
			if !creds.ExophaseConfigured() {
				continue
			}
			rec, err = exophase.FetchUbisoftRecord(ctx, &exophase.ExophaseClient{User: creds.ExophaseUser}, link.ID)
		case model.ProviderXbox:
			if !creds.XBLConfigured() {
				continue
			}
			rec, err = xbox.FetchRecord(ctx, &xbox.XBLClient{APIKey: creds.XBLAPIKey}, link.ID)
		default:
			continue
		}
		if err != nil {
			return nil, err
		}

		// A subset's record is titled by RetroAchievements, not by the site.
		// The game's own title names the *base* set, so writing it here would
		// erase the only place "Mouse Alley" is recorded — and a refresh of
		// the game runs through this same loop for every set it has. Blank
		// (a failed fetch) leaves whatever SaveRecord already had.
		recordTitle := title
		if link.Subset {
			recordTitle = rec.ProviderTitle
		}
		path, err := model.SaveRecord(archiveDir, link.Provider, link.ID, recordTitle, rec)
		if err != nil {
			return nil, err
		}
		stored, err := model.LoadRecord(archiveDir, link.Provider, link.ID)
		if err != nil {
			return nil, err
		}
		saved = append(saved, savedRecord{Provider: link.Provider, ID: link.ID, Subset: link.Subset, Path: path, Record: stored})
	}

	if len(saved) == 0 {
		return nil, fmt.Errorf("no configured provider for this game")
	}
	return saved, nil
}

// RunAchievements fetches (or refreshes) the achievement history for one game
// that already exists in content/games.
func RunAchievements(args []string) error {
	if len(args) > 0 && args[0] == "--all" {
		return runAchievementsAll()
	}

	gamesDir, err := model.FindGamesDir()
	if err != nil {
		return err
	}
	games, err := model.ListGames(gamesDir)
	if err != nil {
		return err
	}

	slug, err := resolveSuggestSlug(args, games)
	if err != nil {
		return err
	}
	var summary model.GameSummary
	found := false
	for _, g := range games {
		if g.Slug == slug {
			summary, found = g, true
			break
		}
	}
	if !found {
		return fmt.Errorf("no game %q found. Known games: %s", slug, joinSlugs(games))
	}

	doc, err := model.LoadDoc(summary.Path)
	if err != nil {
		return err
	}
	links := doc.ProviderLinks()
	if len(links) == 0 {
		return fmt.Errorf("%s has no retroachievements_id, steam_appid, psn_id, ubisoft_id or xbox_id set", slug)
	}

	archiveDir := model.FindArchiveDir(gamesDir)
	saved, err := SaveAchievements(context.Background(), archiveDir, summary.Title,
		links, LoadCredentials())
	if err != nil {
		return err
	}

	for _, s := range saved {
		p := s.Record
		if p.LastError != "" {
			fmt.Fprintf(os.Stderr, "  %s: %s (existing data kept)\n", s.Label(), p.LastError)
		} else {
			fmt.Printf("  %s: %d/%d unlocked, %s to %s\n", s.Label(), p.Unlocked, p.Total, p.First, p.Last)
		}
		fmt.Printf("Saved %s\n", s.Path)
	}

	if _, err := model.WriteAchievementSummary(archiveDir, filepath.Dir(summary.Path), links); err != nil {
		fmt.Fprintf(os.Stderr, "  (achievement summary not updated: %v)\n", err)
	}

	if forms.IsOneShot(doc.FM.Status) {
		if err := maybePromptNewSession(filepath.Dir(summary.Path), summary.Title, saved); err != nil {
			fmt.Fprintf(os.Stderr, "  (session check skipped: %v)\n", err)
		}
	}
	return nil
}

// maybePromptNewSession closes the gap this command otherwise leaves open:
// the timeline's visible bars come only from playthroughs.yaml, which
// refreshing provider data never touches on its own, so a burst of new play
// on a one-shot game (ongoing/multiplayer/software — the statuses
// whose entire record *is* its sessions list) can sit invisible until
// someone remembers to log a session by hand. If this refresh pulled in
// activity past what's already logged, offer to log it right now instead.
//
// Only handles the unambiguous case: exactly one playthrough entry. A game
// with none yet, or more than one (a rare multi-platform one-shot), is left
// to "Log a new session" by hand rather than guessing which entry a new
// session belongs to.
func maybePromptNewSession(gameDir, title string, saved []savedRecord) error {
	pf, err := model.LoadPlaythroughs(gameDir)
	if err != nil {
		return err
	}
	views := pf.Views()
	if len(views) != 1 {
		return nil
	}

	var latest string
	for _, s := range saved {
		for _, d := range []string{s.Record.LastPlayed, s.Record.Last} {
			if d > latest {
				latest = d
			}
		}
	}
	known := views[0].LatestDate()
	if latest == "" || latest <= known {
		return nil
	}

	var log bool
	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("%s: activity through %s isn't logged yet (last session %s). Log a session now?",
					title, latest, forms.OrDash(known))).
				Value(&log),
		),
	).Run(); err != nil {
		return err
	}
	if !log {
		return nil
	}

	started, finished, sessTitle, err := forms.SessionForm(latest, latest)
	if err != nil {
		return err
	}
	return mutate.AddSessionAndWrite(pf, 0, views[0].HasSessions(), started, finished, sessTitle)
}

// runAchievementsAll refreshes every game that has a provider link, in the
// order model.ListGames returns them. It's the entry point a scheduled, unattended
// run would call instead of naming one slug at a time — the loop is nothing
// more than saveAchievements/model.WriteAchievementSummary called per game, so it
// inherits the same merge-only, never-lose-data guarantees as the one-game
// path rather than needing its own.
func runAchievementsAll() error {
	gamesDir, err := model.FindGamesDir()
	if err != nil {
		return err
	}
	games, err := model.ListGames(gamesDir)
	if err != nil {
		return err
	}
	archiveDir := model.FindArchiveDir(gamesDir)
	creds := LoadCredentials()

	var refreshed, skipped, failed int
	for _, g := range games {
		links := g.ProviderLinks()
		if len(links) == 0 {
			skipped++
			continue
		}

		fmt.Println(g.Title)
		saved, err := SaveAchievements(context.Background(), archiveDir, g.Title, links, creds)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %v\n", err)
			failed++
			continue
		}
		for _, s := range saved {
			p := s.Record
			if p.LastError != "" {
				fmt.Fprintf(os.Stderr, "  %s: %s (existing data kept)\n", s.Label(), p.LastError)
			} else {
				fmt.Printf("  %s: %d/%d unlocked, %s to %s\n", s.Label(), p.Unlocked, p.Total, p.First, p.Last)
			}
		}
		if _, err := model.WriteAchievementSummary(archiveDir, filepath.Dir(g.Path), links); err != nil {
			fmt.Fprintf(os.Stderr, "  (achievement summary not updated: %v)\n", err)
		}
		refreshed++
	}
	fmt.Printf("%d refreshed, %d with no provider link, %d failed\n", refreshed, skipped, failed)
	return nil
}

// RunProject regenerates every game's achievement-summary.yaml purely from
// what's already archived — no API calls, so it's safe and fast to run any
// time the projection needs rebuilding (after a manual archive edit, or a
// change to what the summary contains) without re-fetching anything.
func RunProject(args []string) error {
	gamesDir, err := model.FindGamesDir()
	if err != nil {
		return err
	}
	games, err := model.ListGames(gamesDir)
	if err != nil {
		return err
	}
	archiveDir := model.FindArchiveDir(gamesDir)

	var written, empty, failed int
	for _, g := range games {
		links := g.ProviderLinks()
		if len(links) == 0 {
			continue
		}
		wrote, err := model.WriteAchievementSummary(archiveDir, filepath.Dir(g.Path), links)
		switch {
		case err != nil:
			fmt.Fprintf(os.Stderr, "  %s: %v\n", g.Slug, err)
			failed++
		case wrote:
			written++
		default:
			empty++
		}
	}
	fmt.Printf("%d summaries written, %d with nothing archived yet, %d failed\n", written, empty, failed)
	return nil
}
