package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.dalton.dog/gamelog/internal/model"
)

func TestHasArchiveRecord(t *testing.T) {
	dir := t.TempDir()
	if HasArchiveRecord(dir, model.BuildProviderLinks("4650", "1145360", "")) {
		t.Fatal("expected no archive record for either ID before any is saved")
	}

	if _, err := model.SaveRecord(dir, model.ProviderRA, "4650", "Hades II", &model.ProviderRecord{}); err != nil {
		t.Fatal(err)
	}
	if !HasArchiveRecord(dir, model.BuildProviderLinks("4650", "1145360", "")) {
		t.Fatal("expected the saved RA record to be found")
	}
	if HasArchiveRecord(dir, model.BuildProviderLinks("", "1145360", "")) {
		t.Fatal("a blank ID should never match a record")
	}
}

// The delete rule is the load-bearing safety check: a stub with captured
// data is no longer a plausible false-positive scan match, and CLAUDE.md's
// "never lose captured data" invariant must win.
func TestDeleteGameStub_RefusesWhenUnsafe(t *testing.T) {
	gamesDir := t.TempDir()
	gameDir := filepath.Join(gamesDir, "some-game")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gameDir, "_index.md"), []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := DeleteGameStub(gamesDir, model.GameSummary{Slug: "some-game", NumPlaythroughs: 1}, false)
	if err == nil {
		t.Fatal("expected deletion to be refused")
	}
	if !strings.Contains(err.Error(), "captured data") {
		t.Errorf("error should explain why, got: %v", err)
	}
	if _, statErr := os.Stat(gameDir); statErr != nil {
		t.Errorf("refused delete must not touch the directory: %v", statErr)
	}
}

func TestDeleteGameStub_RemovesACleanStub(t *testing.T) {
	gamesDir := t.TempDir()
	gameDir := filepath.Join(gamesDir, "some-game")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gameDir, "_index.md"), []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := DeleteGameStub(gamesDir, model.GameSummary{Slug: "some-game", NumPlaythroughs: 0}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(gameDir); !os.IsNotExist(err) {
		t.Error("expected the game directory to be gone")
	}
}
