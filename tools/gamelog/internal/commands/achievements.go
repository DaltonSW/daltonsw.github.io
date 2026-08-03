package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/charmbracelet/huh"

	"go.dalton.dog/gamelog/internal/forms"
	"go.dalton.dog/gamelog/internal/model"
	"go.dalton.dog/gamelog/internal/mutate"
	"go.dalton.dog/gamelog/internal/providers/exophase"
	"go.dalton.dog/gamelog/internal/providers/retroachievements"
	"go.dalton.dog/gamelog/internal/providers/steam"
)

// savedRecord is one provider's outcome from a fetch-and-archive run.
type savedRecord struct {
	Provider string
	ID       string
	Path     string
	Record   *model.ArchiveRecord
}

// saveAchievements fetches from every configured provider the game is linked
// to — not just the first — and merges each result into its own archive file.
func SaveAchievements(ctx context.Context, archiveDir, title string, links []model.ProviderLink, creds Credentials) ([]savedRecord, error) {
	fresh := map[string]*model.ProviderRecord{}
	ids := map[string]string{}

	byProvider := map[string]string{}
	for _, l := range links {
		if l.ID != "" {
			byProvider[l.Provider] = l.ID
		}
	}
	raID, steamAppID, psnID := byProvider[model.ProviderRA], byProvider[model.ProviderSteam], byProvider[model.ProviderPSN]

	if raID != "" && creds.RAConfigured() {
		client := &retroachievements.RAClient{Username: creds.RAUsername, APIKey: creds.RAAPIKey}
		rec, err := retroachievements.FetchRecord(ctx, client, raID)
		if err != nil {
			return nil, err
		}
		fresh[model.ProviderRA], ids[model.ProviderRA] = rec, raID
	}
	if steamAppID != "" && creds.SteamConfigured() {
		client := &steam.SteamClient{APIKey: creds.SteamAPIKey, SteamID: creds.SteamID}
		var owned *steam.SteamOwnedGame
		if games, err := client.GetOwnedGames(ctx); err == nil {
			for i := range games {
				if strconv.Itoa(games[i].AppID) == steamAppID {
					owned = &games[i]
					break
				}
			}
		}
		rec, err := steam.FetchRecord(ctx, client, steamAppID, owned)
		if err != nil {
			return nil, err
		}
		fresh[model.ProviderSteam], ids[model.ProviderSteam] = rec, steamAppID
	}
	if psnID != "" && creds.ExophaseConfigured() {
		client := &exophase.ExophaseClient{User: creds.ExophaseUser}
		rec, err := exophase.FetchPSNRecord(ctx, client, psnID)
		if err != nil {
			return nil, err
		}
		fresh[model.ProviderPSN], ids[model.ProviderPSN] = rec, psnID
	}

	if len(fresh) == 0 {
		return nil, fmt.Errorf("no configured provider for this game")
	}

	var saved []savedRecord
	for _, provider := range model.ProviderOrder {
		rec, ok := fresh[provider]
		if !ok {
			continue
		}
		path, err := model.SaveRecord(archiveDir, provider, ids[provider], title, rec)
		if err != nil {
			return nil, err
		}
		stored, err := model.LoadRecord(archiveDir, provider, ids[provider])
		if err != nil {
			return nil, err
		}
		saved = append(saved, savedRecord{Provider: provider, ID: ids[provider], Path: path, Record: stored})
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
		return fmt.Errorf("%s has no retroachievements_id, steam_appid or psn_id set", slug)
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
			fmt.Fprintf(os.Stderr, "  %s: %s (existing data kept)\n", s.Provider, p.LastError)
		} else {
			fmt.Printf("  %s: %d/%d unlocked, %s to %s\n", s.Provider, p.Unlocked, p.Total, p.First, p.Last)
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
				fmt.Fprintf(os.Stderr, "  %s: %s (existing data kept)\n", s.Provider, p.LastError)
			} else {
				fmt.Printf("  %s: %d/%d unlocked, %s to %s\n", s.Provider, p.Unlocked, p.Total, p.First, p.Last)
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
