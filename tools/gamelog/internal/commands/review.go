package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"go.dalton.dog/gamelog/internal/model"
)

// DeleteGameStub removes a game's whole content directory. Re-checks
// canDelete independently as defense in depth, and only ever targets a slug
// that came from ListGames, never typed input.
func DeleteGameStub(gamesDir string, g model.GameSummary, canDelete bool) error {
	if !canDelete {
		return fmt.Errorf("refusing to delete %s: it has captured data (playthroughs=%d)", g.Slug, g.NumPlaythroughs)
	}
	dir := filepath.Join(gamesDir, g.Slug)
	if filepath.Dir(dir) != gamesDir {
		return fmt.Errorf("refusing to delete outside %s", gamesDir)
	}
	return os.RemoveAll(dir)
}

func HasArchiveRecord(archiveDir string, links []model.ProviderLink) bool {
	for _, l := range links {
		if l.ID == "" {
			continue
		}
		if rec, _ := model.LoadRecord(archiveDir, l.Provider, l.ID); rec != nil {
			return true
		}
	}
	return false
}
