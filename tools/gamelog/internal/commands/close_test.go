package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.dalton.dog/gamelog/internal/model"
)

// writeGameWithPlaythroughs creates a game bundle with the given
// playthroughs.yaml body and returns a model.GameSummary pointing at it.
func writeGameWithPlaythroughs(t *testing.T, gamesDir, slug, status, body string) model.GameSummary {
	t.Helper()
	dir := filepath.Join(gamesDir, slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "_index.md")
	if err := os.WriteFile(path, []byte("---\ntitle: "+slug+"\nstatus: "+status+"\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if body != "" {
		if err := os.WriteFile(filepath.Join(dir, model.PlaythroughsFilename), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return model.GameSummary{Slug: slug, Path: path, Title: slug, Status: status}
}

const openSessionsYAML = `playthroughs:
  - status: playing
    sessions:
      - started: "2015-08-16"
        finished: "2015-08-16"
      - started: "2022-01-05"
        finished: ""
`

const openFlatYAML = `playthroughs:
  - started: "2019-03-02"
    finished: ""
    status: playing
`

// The closing date lands on the trailing session, not the entry, when the
// entry has a sessions list — that's where the timeline reads "ongoing" from.
func TestCloseEntry_ClosesTrailingSession(t *testing.T) {
	dir := t.TempDir()
	g := writeGameWithPlaythroughs(t, dir, "sandbox", "ongoing", openSessionsYAML)

	did, err := CloseEntry(OpenEntry{Game: g, Index: 0, CloseOn: "2022-02-23"})
	if err != nil {
		t.Fatal(err)
	}
	if !did {
		t.Fatal("expected the entry to be closed")
	}

	pf, err := model.LoadPlaythroughs(filepath.Dir(g.Path))
	if err != nil {
		t.Fatal(err)
	}
	sessions := pf.Playthroughs[0].Sessions
	if got := sessions[len(sessions)-1].Finished; got != "2022-02-23" {
		t.Errorf("trailing session finished = %q, want 2022-02-23", got)
	}
	// The earlier session must be untouched — this is the operation that
	// historically adopted a neighbour's fields.
	if sessions[0].Finished != "2015-08-16" {
		t.Errorf("earlier session was disturbed: %+v", sessions[0])
	}
	// Closing a date says when play stopped, never that the game was
	// completed; for a ongoing game there is no completion to claim.
	if pf.Playthroughs[0].Status != "playing" {
		t.Errorf("status changed to %q, want it left alone", pf.Playthroughs[0].Status)
	}
}

func TestCloseEntry_ClosesFlatEntry(t *testing.T) {
	dir := t.TempDir()
	g := writeGameWithPlaythroughs(t, dir, "flat", "multiplayer", openFlatYAML)

	if _, err := CloseEntry(OpenEntry{Game: g, Index: 0, CloseOn: "2020-07-16"}); err != nil {
		t.Fatal(err)
	}
	pf, err := model.LoadPlaythroughs(filepath.Dir(g.Path))
	if err != nil {
		t.Fatal(err)
	}
	if got := pf.Playthroughs[0].Finished; got != "2020-07-16" {
		t.Errorf("finished = %q, want 2020-07-16", got)
	}
	if len(pf.Playthroughs[0].Sessions) != 0 {
		t.Errorf("a flat entry must not gain a sessions list: %+v", pf.Playthroughs[0])
	}
}

// An entry closed between building the list and confirming it is left exactly
// as it is — a stale selection must never overwrite a real date.
func TestCloseEntry_NeverOverwritesAnExistingDate(t *testing.T) {
	dir := t.TempDir()
	g := writeGameWithPlaythroughs(t, dir, "already", "ongoing", `playthroughs:
  - started: "2019-03-02"
    finished: "2019-04-01"
    status: playing
`)
	did, err := CloseEntry(OpenEntry{Game: g, Index: 0, CloseOn: "2024-01-01"})
	if err != nil {
		t.Fatal(err)
	}
	if did {
		t.Fatal("expected no write for an already-closed entry")
	}
	pf, err := model.LoadPlaythroughs(filepath.Dir(g.Path))
	if err != nil {
		t.Fatal(err)
	}
	if got := pf.Playthroughs[0].Finished; got != "2019-04-01" {
		t.Errorf("finished = %q, want the original 2019-04-01", got)
	}
}

// Hand-added fields the tool doesn't model must survive the rewrite — the
// inline Extra catch-all is what makes that true, and this is a whole-file
// encode like every other playthrough write.
func TestCloseEntry_PreservesUnknownFields(t *testing.T) {
	dir := t.TempDir()
	g := writeGameWithPlaythroughs(t, dir, "extra", "ongoing", `playthroughs:
  - started: "2019-03-02"
    finished: ""
    status: playing
    mood: nostalgic
`)
	if _, err := CloseEntry(OpenEntry{Game: g, Index: 0, CloseOn: "2020-01-01"}); err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(filepath.Join(dir, "extra", model.PlaythroughsFilename))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "mood: nostalgic") {
		t.Errorf("unknown field was dropped:\n%s", out)
	}
}

// Only quiet, genuinely-open entries qualify. "playing" is excluded because an
// open date is correct there; "paused" because it's on hold, not over;
// "planned" because it hasn't been started yet.
func TestFindOpenEntries_FiltersToEligibleGames(t *testing.T) {
	dir := t.TempDir()
	archiveDir := t.TempDir()

	games := []model.GameSummary{
		writeGameWithPlaythroughs(t, dir, "quiet", "ongoing", openFlatYAML),
		writeGameWithPlaythroughs(t, dir, "playing-now", "playing", openFlatYAML),
		writeGameWithPlaythroughs(t, dir, "on-hold", "paused", openFlatYAML),
		writeGameWithPlaythroughs(t, dir, "planned", "planned", openFlatYAML),
		writeGameWithPlaythroughs(t, dir, "closed", "ongoing", `playthroughs:
  - started: "2019-03-02"
    finished: "2019-04-01"
    status: playing
`),
		writeGameWithPlaythroughs(t, dir, "no-file", "ongoing", ""),
		writeGameWithPlaythroughs(t, dir, "recent", "ongoing", `playthroughs:
  - started: "`+daysAgo(5)+`"
    finished: ""
    status: playing
`),
	}

	got, err := FindOpenEntries(archiveDir, dir, games, 30, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Game.Slug != "quiet" {
		var slugs []string
		for _, e := range got {
			slugs = append(slugs, e.Game.Slug)
		}
		t.Fatalf("expected only [quiet], got %v", slugs)
	}
	if got[0].CloseOn != "2019-03-02" {
		t.Errorf("CloseOn = %q, want the logged start 2019-03-02", got[0].CloseOn)
	}
}

// --all reaches "playing" games but must still leave "paused" and "planned" alone.
func TestFindOpenEntries_AllIncludesPlayingButNotPausedOrPlanned(t *testing.T) {
	dir := t.TempDir()
	games := []model.GameSummary{
		writeGameWithPlaythroughs(t, dir, "playing-now", "playing", openFlatYAML),
		writeGameWithPlaythroughs(t, dir, "on-hold", "paused", openFlatYAML),
	}
	got, err := FindOpenEntries(t.TempDir(), dir, games, 30, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Game.Slug != "playing-now" {
		t.Fatalf("expected only [playing-now], got %+v", got)
	}
}

// A refreshed archive date is more recent than any hand-logged session, so it
// wins — but only when it really is later.
func TestFindOpenEntries_PrefersTheLaterOfArchiveAndLoggedStart(t *testing.T) {
	dir := t.TempDir()
	archiveDir := t.TempDir()
	if _, err := model.SaveRecord(archiveDir, model.ProviderSteam, "7", "Sandbox", &model.ProviderRecord{
		LastPlayed: "2022-02-23",
	}); err != nil {
		t.Fatal(err)
	}

	g := writeGameWithPlaythroughs(t, dir, "sandbox", "ongoing", openSessionsYAML)
	g.SteamAppID = "7"
	got, err := FindOpenEntries(archiveDir, dir, []model.GameSummary{g}, 30, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one open entry, got %d", len(got))
	}
	if got[0].CloseOn != "2022-02-23" || got[0].Source != "archive" {
		t.Errorf("got %s from %q, want 2022-02-23 from archive", got[0].CloseOn, got[0].Source)
	}

	// With no archive link at all, the open session's own start date is the
	// last day we can prove it was played.
	g2 := writeGameWithPlaythroughs(t, dir, "unlinked", "multiplayer", openSessionsYAML)
	got2, err := FindOpenEntries(archiveDir, dir, []model.GameSummary{g2}, 30, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got2) != 1 || got2[0].CloseOn != "2022-01-05" || got2[0].Source != "logged start" {
		t.Fatalf("expected 2022-01-05 from logged start, got %+v", got2)
	}
}
