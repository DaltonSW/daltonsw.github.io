package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCountDraftsAndNextDraft(t *testing.T) {
	games := []GameSummary{
		{Slug: "a", Draft: true},
		{Slug: "b", Draft: false},
		{Slug: "c", Draft: true},
	}
	if got := countDrafts(games); got != 2 {
		t.Fatalf("countDrafts = %d, want 2", got)
	}

	next := nextDraft(games, map[string]bool{})
	if next == nil || next.Slug != "a" {
		t.Fatalf("nextDraft = %v, want a", next)
	}

	next = nextDraft(games, map[string]bool{"a": true})
	if next == nil || next.Slug != "c" {
		t.Fatalf("nextDraft with a skipped = %v, want c", next)
	}

	next = nextDraft(games, map[string]bool{"a": true, "c": true})
	if next != nil {
		t.Fatalf("nextDraft with everything skipped = %v, want nil", next)
	}
}

func TestHasArchiveRecord(t *testing.T) {
	dir := t.TempDir()
	if hasArchiveRecord(dir, "4650", "1145360") {
		t.Fatal("expected no archive record for either ID before any is saved")
	}

	if _, err := SaveRecord(dir, providerRA, "4650", "Hades II", &ProviderRecord{}); err != nil {
		t.Fatal(err)
	}
	if !hasArchiveRecord(dir, "4650", "1145360") {
		t.Fatal("expected the saved RA record to be found")
	}
	if hasArchiveRecord(dir, "", "1145360") {
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

	err := deleteGameStub(gamesDir, GameSummary{Slug: "some-game", NumPlaythroughs: 1}, false)
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

	if err := deleteGameStub(gamesDir, GameSummary{Slug: "some-game", NumPlaythroughs: 0}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(gameDir); !os.IsNotExist(err) {
		t.Error("expected the game directory to be gone")
	}
}

func TestFormatReviewCard_ShowsProviderLinksAndPlaythroughs(t *testing.T) {
	g := GameSummary{Title: "Hades", RAGameID: "4650", SteamAppID: "1145360"}
	fm := FrontMatter{Platform: "PC", Status: "playing"}
	pf := &PlaythroughsFile{Playthroughs: []PlaythroughEntry{
		{Started: "2026-01-01", Status: "playing"},
	}}

	card := formatReviewCard(g, fm, pf)
	for _, want := range []string{"Hades", "RA 4650", "Steam 1145360", "PC", "1 playthrough"} {
		if !strings.Contains(card, want) {
			t.Errorf("review card missing %q, got:\n%s", want, card)
		}
	}
}

func TestFormatReviewCard_NoPlaythroughsIsNotLinked(t *testing.T) {
	g := GameSummary{Title: "Unlinked Game"}
	fm := FrontMatter{}
	card := formatReviewCard(g, fm, &PlaythroughsFile{})
	if !strings.Contains(card, "not linked") {
		t.Errorf("expected 'not linked', got:\n%s", card)
	}
	if !strings.Contains(card, "no playthroughs logged") {
		t.Errorf("expected 'no playthroughs logged', got:\n%s", card)
	}
}
