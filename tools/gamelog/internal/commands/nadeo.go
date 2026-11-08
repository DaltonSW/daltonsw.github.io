package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.dalton.dog/gamelog/internal/model"
	"go.dalton.dog/gamelog/internal/providers/nadeo"
)

// nadeoClient builds a client from the environment, or explains what's missing.
func nadeoClient(creds Credentials) (*nadeo.Client, error) {
	if !creds.NadeoConfigured() {
		return nil, fmt.Errorf("set NADEO_SERVICE_LOGIN and NADEO_SERVICE_PASSWORD — see .env.example")
	}
	return &nadeo.Client{Login: creds.NadeoLogin, Password: creds.NadeoPassword}, nil
}

// RunNadeoWhoami prints the authenticated account's id, which is what goes in
// a game's nadeo_account_id front matter. One request.
func RunNadeoWhoami() error {
	c, err := nadeoClient(LoadCredentials())
	if err != nil {
		return err
	}
	id, err := c.AccountID(context.Background())
	if err != nil {
		return err
	}
	fmt.Println(id)
	fmt.Fprintf(os.Stderr, "\nAdd to content/games/<slug>/_index.md:\n  nadeo_account_id: %s\n", id)
	return nil
}

// RunNadeoSeasons lists every official campaign next to what's already
// archived, so a fetch can be aimed at one season rather than all of them.
// Read-only, and one request beyond auth.
func RunNadeoSeasons() error {
	c, err := nadeoClient(LoadCredentials())
	if err != nil {
		return err
	}
	ctx := context.Background()

	campaigns, _, err := c.Campaigns(ctx)
	if err != nil {
		return err
	}

	// Pair each campaign with what the archive already holds for it, if the
	// game has been linked yet. Missing linkage isn't an error — listing the
	// seasons is exactly what you do *before* linking.
	archived := map[string]string{}
	if gamesDir, err := model.FindGamesDir(); err == nil {
		if game, gerr := findNadeoGame(gamesDir); gerr == nil {
			rec, rerr := model.LoadNadeoRecord(model.FindArchiveDir(gamesDir), game.NadeoAccountID)
			if rerr == nil && rec != nil {
				for _, camp := range rec.Campaigns {
					driven := 0
					for _, t := range camp.Tracks {
						if t.Driven() {
							driven++
						}
					}
					archived[camp.SeasonUID] = fmt.Sprintf("%d/%d driven", driven, len(camp.Tracks))
				}
			}
		}
	}

	// Newest first: that's the season you'd usually be reaching for.
	for _, camp := range slices.Backward(campaigns) {
		status := archived[camp.SeasonUID]
		if status == "" {
			status = "— not fetched"
		}
		fmt.Printf("%s   %s\n", nadeo.DescribeCampaign(camp), status)
	}
	return nil
}

// NadeoFetchOptions is what the CLI collected, before it's turned into the
// provider's own options.
type NadeoFetchOptions struct {
	Seasons []string
	All     bool
	Since   int
	DumpRaw string
	Plain   bool // force the non-TTY output path
}

// RunNadeoFetch captures campaign history into archive/nadeo/<accountId>.json
// and refreshes the game's campaigns.yaml projection.
func RunNadeoFetch(opts NadeoFetchOptions) error {
	gamesDir, err := model.FindGamesDir()
	if err != nil {
		return err
	}
	game, err := findNadeoGame(gamesDir)
	if err != nil {
		return err
	}

	// Ctrl-C cancels the fetch rather than killing the process, so a partial
	// capture is still saved. The merge makes that strictly additive.
	ctx, stop := interruptContext()
	defer stop()

	progress := newNadeoProgress(game.Title, opts.Plain, stop)
	res, err := SaveNadeo(ctx, gamesDir, nadeo.FetchOptions{
		Seasons: opts.Seasons,
		All:     opts.All,
		Since:   opts.Since,
		OnEvent: progress.Handle,
	}, opts.DumpRaw)
	progress.Close()
	if err != nil {
		return err
	}

	if res.LastError != "" {
		fmt.Fprintf(os.Stderr, "  nadeo: %s (existing data kept)\n", res.LastError)
	}
	fmt.Printf("%s\nSaved %s\n", DescribeNadeoRecord(res.Stored), res.Path)
	if res.SummaryErr != nil {
		fmt.Fprintf(os.Stderr, "  (campaign summary not updated: %v)\n", res.SummaryErr)
	}
	if opts.DumpRaw != "" {
		fmt.Fprintf(os.Stderr, "  raw responses written to %s\n", opts.DumpRaw)
	}
	return nil
}

// NadeoSaveResult is what one fetch-and-merge left behind.
type NadeoSaveResult struct {
	Game       model.GameSummary
	Stored     *model.NadeoRecord // the merged record now on disk
	Path       string
	LastError  string // the fetch's own failure; existing data was kept
	SummaryErr error  // campaigns.yaml couldn't be rebuilt; the archive is still saved
}

// SaveNadeo is the fetch → merge → project sequence shared by the CLI and the
// web UI: capture what opts selects for the game owning nadeo_account_id,
// merge it into the archive, and rebuild that game's campaigns.yaml.
func SaveNadeo(ctx context.Context, gamesDir string, opts nadeo.FetchOptions, dumpRaw string) (NadeoSaveResult, error) {
	c, err := nadeoClient(LoadCredentials())
	if err != nil {
		return NadeoSaveResult{}, err
	}
	c.DumpRaw = dumpRaw

	game, err := findNadeoGame(gamesDir)
	if err != nil {
		return NadeoSaveResult{}, err
	}
	archiveDir := model.FindArchiveDir(gamesDir)

	rec, err := nadeo.FetchRecord(ctx, c, opts)
	if err != nil {
		return NadeoSaveResult{}, err
	}

	path, err := model.SaveNadeoRecord(archiveDir, rec.ID, rec)
	if err != nil {
		return NadeoSaveResult{}, err
	}
	stored, err := model.LoadNadeoRecord(archiveDir, rec.ID)
	if err != nil {
		return NadeoSaveResult{}, err
	}
	_, summaryErr := model.WriteCampaignSummary(archiveDir, filepath.Dir(game.Path), rec.ID)
	return NadeoSaveResult{
		Game: game, Stored: stored, Path: path,
		LastError: rec.LastError, SummaryErr: summaryErr,
	}, nil
}

// DescribeNadeoRecord is the one-line summary the CLI ends with and the web
// UI flashes after a refresh.
func DescribeNadeoRecord(rec *model.NadeoRecord) string {
	if rec == nil {
		return "nadeo: nothing archived"
	}
	tracks, driven := 0, 0
	for _, c := range rec.Campaigns {
		for _, t := range c.Tracks {
			tracks++
			if t.Driven() {
				driven++
			}
		}
	}
	return fmt.Sprintf("nadeo: %d seasons, %d tracks, %d personal bests",
		len(rec.Campaigns), tracks, driven)
}

// findNadeoGame locates the single game carrying a nadeo_account_id.
func findNadeoGame(gamesDir string) (model.GameSummary, error) {
	games, err := model.ListGames(gamesDir)
	if err != nil {
		return model.GameSummary{}, err
	}
	var found []model.GameSummary
	for _, g := range games {
		if g.NadeoAccountID != "" {
			found = append(found, g)
		}
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return model.GameSummary{}, fmt.Errorf(
			"no game has nadeo_account_id set — run `gamelog nadeo whoami` and add it to content/games/<slug>/_index.md")
	default:
		var slugs []string
		for _, g := range found {
			slugs = append(slugs, g.Slug)
		}
		// Nadeo's API covers exactly one game, so two linked games means two
		// content entries would fight over the same archive record.
		return model.GameSummary{}, fmt.Errorf(
			"more than one game has nadeo_account_id set (%s) — only one can own the Trackmania archive",
			strings.Join(slugs, ", "))
	}
}
