package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// First finished is the earliest *finished* run, not the first run: BG3's
// shape is a dropped attempt, then finished runs, then a later replay.
func TestFirstFinished_EarliestFinishedRunOnly(t *testing.T) {
	views := []Playthrough{
		{Status: "dropped", Started: "2022-01-01", Finished: "2022-02-01"},
		{Status: "finished", Sessions: []Session{
			{Started: "2023-10-15", Finished: "2023-11-11"},
			{Started: "2023-12-31", Finished: "2024-02-11"},
		}},
		{Status: "finished", Started: "2024-11-02", Finished: "2024-12-25"},
		{Status: "playing", Started: "2026-08-29"},
	}
	if got := firstFinished(views); got != "2024-02-11" {
		t.Errorf("firstFinished = %q, want the session-ended 2024-02-11", got)
	}
	if got := firstFinished(views[:1]); got != "" {
		t.Errorf("a dropped-only game has no first finish, got %q", got)
	}
}

// A new game's dates become its first playthrough; the front matter has none.
func TestCreateGameFile_DatesBecomeTheFirstPlaythrough(t *testing.T) {
	dir := t.TempDir()
	path, err := CreateGameFile(dir, "undertale", NewGameFields{
		Title: "Undertale", Platform: "PC", Status: "finished",
		Started: "2016-01-14", Finished: "2016-01-17",
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "started:") || strings.Contains(string(raw), "finished:") {
		t.Errorf("front matter must carry no dates:\n%s", raw)
	}
	pf, err := LoadPlaythroughs(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(pf.Playthroughs) != 1 {
		t.Fatalf("want one playthrough, got %+v", pf.Playthroughs)
	}
	e := pf.Playthroughs[0]
	if e.Started != "2016-01-14" || e.Finished != "2016-01-17" || e.Status != "finished" {
		t.Errorf("entry = %+v", e)
	}
}

func TestCreateGameFile_NoDatesNoPlaythrough(t *testing.T) {
	dir := t.TempDir()
	path, err := CreateGameFile(dir, "outer-wilds", NewGameFields{Title: "Outer Wilds", Status: "backlog"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(PlaythroughsPath(filepath.Dir(path))); !os.IsNotExist(err) {
		t.Errorf("an undated game must get no playthroughs.yaml (err=%v)", err)
	}
}
