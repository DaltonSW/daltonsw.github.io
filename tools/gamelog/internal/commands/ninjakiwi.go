package commands

import (
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
	creds := LoadCredentials()
	c, err := ninjaKiwiClient(creds)
	if err != nil {
		return err
	}

	gamesDir, err := model.FindGamesDir()
	if err != nil {
		return err
	}
	game, err := findNinjaKiwiGame(gamesDir)
	if err != nil {
		return err
	}
	archiveDir := model.FindArchiveDir(gamesDir)

	ctx, stop := interruptContext()
	defer stop()

	rec, err := ninjakiwi.FetchRecord(ctx, c, game.NinjaKiwiUserID)
	if err != nil {
		return err
	}
	if rec.LastError != "" {
		fmt.Fprintf(os.Stderr, "  ninjakiwi: %s (existing data kept)\n", rec.LastError)
	}

	path, err := model.SaveNinjaKiwiRecord(archiveDir, rec.ID, rec)
	if err != nil {
		return err
	}

	stored, err := model.LoadNinjaKiwiRecord(archiveDir, rec.ID)
	if err != nil {
		return err
	}
	fmt.Printf("%s\nSaved %s\n", describeNinjaKiwiRecord(stored), path)

	if _, err := model.WriteBTD6Summary(archiveDir, filepath.Dir(game.Path), rec.ID); err != nil {
		fmt.Fprintf(os.Stderr, "  (btd6 summary not updated: %v)\n", err)
	}
	return nil
}

func describeNinjaKiwiRecord(rec *model.BTD6Record) string {
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
