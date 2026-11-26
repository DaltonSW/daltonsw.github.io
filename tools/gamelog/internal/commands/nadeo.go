package commands

import (
	"context"
	"fmt"
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

// NadeoSeason is one row of the Trackmania page's season picker.
type NadeoSeason struct {
	SeasonUID string
	Name      string
	Started   string // YYYY-MM-DD, "" when unknown
	Ended     string
	Tracks    int
	Driven    int
	Archived  bool // the archive holds this season's tracks
}

// NadeoSeasons lists every official campaign next to how much of it is
// archived, newest first, without making a request. The campaign list comes
// from the archive's raw.campaigns — every fetch captures the whole list, even
// a one-season fetch — so a season newer than the last fetch is missing until
// the next one; the picker offers "current season" for exactly that case.
// Archived seasons are listed even if the stored list somehow lacks them.
func NadeoSeasons(gamesDir string) ([]NadeoSeason, error) {
	game, err := findNadeoGame(gamesDir)
	if err != nil {
		return nil, err
	}
	rec, err := model.LoadNadeoRecord(model.FindArchiveDir(gamesDir), game.NadeoAccountID)
	if err != nil || rec == nil {
		return nil, err
	}

	byUID := map[string]*NadeoSeason{}
	var rows []*NadeoSeason
	row := func(uid string) *NadeoSeason {
		if r, ok := byUID[uid]; ok {
			return r
		}
		r := &NadeoSeason{SeasonUID: uid}
		byUID[uid] = r
		rows = append(rows, r)
		return r
	}

	if rec.Raw != nil {
		listed, err := nadeo.DecodeCampaigns(rec.Raw.Campaigns)
		if err != nil {
			return nil, err
		}
		for _, c := range listed {
			r := row(c.SeasonUID)
			r.Name = nadeo.SeasonLabel(c)
			r.Started, r.Ended = nadeo.CampaignDates(c)
			r.Tracks = len(c.Playlist)
		}
	}
	for _, camp := range rec.Campaigns {
		r := row(camp.SeasonUID)
		r.Name = model.FirstNonEmpty(camp.Name, r.Name, camp.SeasonUID)
		r.Started = model.FirstNonEmpty(camp.Started, r.Started)
		r.Ended = model.FirstNonEmpty(camp.Ended, r.Ended)
		r.Archived = true
		r.Tracks = max(r.Tracks, len(camp.Tracks))
		for _, t := range camp.Tracks {
			if t.Driven() {
				r.Driven++
			}
		}
	}

	out := make([]NadeoSeason, len(rows))
	for i, r := range rows {
		out[i] = *r
	}
	// Newest first: that's the season you'd usually be reaching for. Dates
	// are YYYY-MM-DD, so they sort as text.
	slices.SortStableFunc(out, func(a, b NadeoSeason) int { return strings.Compare(b.Started, a.Started) })
	return out, nil
}

// NadeoSaveResult is what one fetch-and-merge left behind.
type NadeoSaveResult struct {
	Game       model.GameSummary
	Stored     *model.NadeoRecord // the merged record now on disk
	Path       string
	LastError  string // the fetch's own failure; existing data was kept
	SummaryErr error  // campaigns.yaml couldn't be rebuilt; the archive is still saved
}

// SaveNadeo is the fetch → merge → project sequence shared by Catch up and the
// web UI: capture what opts selects for the game owning nadeo_account_id,
// merge it into the archive, and rebuild that game's campaigns.yaml.
func SaveNadeo(ctx context.Context, gamesDir string, opts nadeo.FetchOptions) (NadeoSaveResult, error) {
	c, err := nadeoClient(LoadCredentials())
	if err != nil {
		return NadeoSaveResult{}, err
	}

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

// DescribeNadeoRecord is the one-line summary Catch up logs and the web
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
			"no game has nadeo_account_id set — add the Trackmania account ID to content/games/<slug>/_index.md")
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
