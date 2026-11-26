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
		{Status: "dropped", Sessions: []Session{{Started: "2022-01-01", Finished: "2022-02-01"}}},
		{Status: "finished", Sessions: []Session{
			{Started: "2023-10-15", Finished: "2023-11-11"},
			{Started: "2023-12-31", Finished: "2024-02-11"},
		}},
		{Status: "finished", Sessions: []Session{{Started: "2024-11-02", Finished: "2024-12-25"}}},
		{Status: "playing", Sessions: []Session{{Started: "2026-08-29"}}},
	}
	if got := firstFinished(views); got != "2024-02-11" {
		t.Errorf("firstFinished = %q, want the session-ended 2024-02-11", got)
	}
	if got := firstFinished(views[:1]); got != "" {
		t.Errorf("a dropped-only game has no first finish, got %q", got)
	}
}

// A run that beat the game partway through — credits, then post-game toward
// mastery — ends later than the day it was beaten. Its beaten: date is what
// first-finished reads, whatever the run's status.
func TestFirstFinished_BeatenOverridesTheRunsEnd(t *testing.T) {
	views := []Playthrough{
		{Status: "finished", Beaten: "2024-02-10", Sessions: []Session{{Started: "2024-01-01", Finished: "2024-06-30"}}},
		{Status: "finished", Sessions: []Session{{Started: "2024-03-01", Finished: "2024-03-20"}}},
	}
	if got := firstFinished(views); got != "2024-02-10" {
		t.Errorf("firstFinished = %q, want the beaten date 2024-02-10", got)
	}
	views[0].Beaten = ""
	if got := firstFinished(views); got != "2024-03-20" {
		t.Errorf("without beaten, firstFinished = %q, want the earliest finish 2024-03-20", got)
	}
	// Still playing for mastery, but already beaten.
	playing := []Playthrough{{Status: "playing", Beaten: "2026-10-20", Sessions: []Session{{Started: "2026-10-01"}}}}
	if got := firstFinished(playing); got != "2026-10-20" {
		t.Errorf("an open run with a beaten date = %q, want 2026-10-20", got)
	}
}

func TestBeaten_RoundTripsAndClears(t *testing.T) {
	dir := t.TempDir()
	pf, err := LoadPlaythroughs(dir)
	if err != nil {
		t.Fatal(err)
	}
	pf.AddPlaythrough(PlaythroughFields{Started: "2024-01-01", Finished: "2024-06-30", Beaten: "2024-02-10", Status: "finished"})
	if err := pf.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := LoadPlaythroughs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if v := got.Views()[0]; v.Beaten != "2024-02-10" || v.FinishDate() != "2024-06-30" {
		t.Fatalf("round trip = %+v", v)
	}
	if err := got.SetBeaten(0, ""); err != nil {
		t.Fatal(err)
	}
	raw, _ := got.Encode()
	if strings.Contains(string(raw), "beaten") {
		t.Errorf("a cleared beaten date should drop its key:\n%s", raw)
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
	if len(e.Sessions) != 1 || e.Sessions[0].Started != "2016-01-14" || e.Sessions[0].Finished != "2016-01-17" ||
		e.Status != "finished" || e.Started != "" || e.Finished != "" {
		t.Errorf("entry = %+v, want one session and no flat dates", e)
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
