package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.dalton.dog/gamelog/internal/model"
	"go.dalton.dog/gamelog/internal/providers/ninjakiwi"
)

// ninjaKiwiClient builds a client from the environment, or explains what's
// missing.
func ninjaKiwiClient(creds Credentials) (*ninjakiwi.Client, error) {
	if !creds.OAKConfigured() {
		return nil, fmt.Errorf("set NINJA_KIWI_OAK — see .env.example")
	}
	return &ninjakiwi.Client{OAK: creds.NinjaKiwiOAK}, nil
}

// RunNinjaKiwiFetch captures a BTD6 save into archive/ninjakiwi/<userId>.json
// and refreshes the game's btd6-summary.yaml projection.
func RunNinjaKiwiFetch() error {
	gamesDir, err := model.FindGamesDir()
	if err != nil {
		return err
	}

	ctx, stop := interruptContext()
	defer stop()

	res, err := SaveNinjaKiwi(ctx, gamesDir)
	if err != nil {
		return err
	}
	if res.LastError != "" {
		fmt.Fprintf(os.Stderr, "  ninjakiwi: %s (existing data kept)\n", res.LastError)
	}
	fmt.Printf("%s\nSaved %s\n", DescribeNinjaKiwiRecord(res.Stored), res.Path)
	if res.SummaryErr != nil {
		fmt.Fprintf(os.Stderr, "  (btd6 summary not updated: %v)\n", res.SummaryErr)
	}
	return nil
}

// NinjaKiwiSaveResult is what one fetch-and-merge left behind.
type NinjaKiwiSaveResult struct {
	Game       model.GameSummary
	Stored     *model.BTD6Record // the merged record now on disk
	Path       string
	LastError  string // the fetch's own failure; existing data was kept
	SummaryErr error  // btd6-summary.yaml couldn't be rebuilt; the archive is still saved
}

// SaveNinjaKiwi is the fetch → merge → project sequence shared by the CLI and
// the web UI, for the game owning ninjakiwi_user_id.
func SaveNinjaKiwi(ctx context.Context, gamesDir string) (NinjaKiwiSaveResult, error) {
	c, err := ninjaKiwiClient(LoadCredentials())
	if err != nil {
		return NinjaKiwiSaveResult{}, err
	}
	game, err := findNinjaKiwiGame(gamesDir)
	if err != nil {
		return NinjaKiwiSaveResult{}, err
	}
	archiveDir := model.FindArchiveDir(gamesDir)

	rec, err := ninjakiwi.FetchRecord(ctx, c, game.NinjaKiwiUserID)
	if err != nil {
		return NinjaKiwiSaveResult{}, err
	}
	path, err := model.SaveNinjaKiwiRecord(archiveDir, rec.ID, rec)
	if err != nil {
		return NinjaKiwiSaveResult{}, err
	}
	stored, err := model.LoadNinjaKiwiRecord(archiveDir, rec.ID)
	if err != nil {
		return NinjaKiwiSaveResult{}, err
	}
	_, summaryErr := model.WriteBTD6Summary(archiveDir, filepath.Dir(game.Path), rec.ID)
	return NinjaKiwiSaveResult{
		Game: game, Stored: stored, Path: path,
		LastError: rec.LastError, SummaryErr: summaryErr,
	}, nil
}

// DescribeNinjaKiwiRecord is the one-line summary the CLI ends with and the
// web UI flashes after a refresh.
func DescribeNinjaKiwiRecord(rec *model.BTD6Record) string {
	if rec == nil {
		return "ninjakiwi: nothing archived"
	}
	towersUnlocked := 0
	for _, unlocked := range rec.UnlockedTowers {
		if unlocked {
			towersUnlocked++
		}
	}
	return fmt.Sprintf("ninjakiwi: rank %d, %d/%d towers, %d achievements",
		rec.Rank, towersUnlocked, len(rec.UnlockedTowers), len(rec.AchievementsClaimed))
}

// findNinjaKiwiGame locates the single game carrying a ninjakiwi_user_id.
func findNinjaKiwiGame(gamesDir string) (model.GameSummary, error) {
	games, err := model.ListGames(gamesDir)
	if err != nil {
		return model.GameSummary{}, err
	}
	var found []model.GameSummary
	for _, g := range games {
		if g.NinjaKiwiUserID != "" {
			found = append(found, g)
		}
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return model.GameSummary{}, fmt.Errorf(
			"no game has ninjakiwi_user_id set — add it to content/games/<slug>/_index.md")
	default:
		var slugs []string
		for _, g := range found {
			slugs = append(slugs, g.Slug)
		}
		return model.GameSummary{}, fmt.Errorf(
			"more than one game has ninjakiwi_user_id set (%s) — only one can own the BTD6 archive",
			strings.Join(slugs, ", "))
	}
}
