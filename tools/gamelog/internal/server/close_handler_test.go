package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.dalton.dog/gamelog/internal/commands"
	"go.dalton.dog/gamelog/internal/model"
)

// writeOpenGame creates content/games/<slug> with one open playthrough
// started on the given date, old enough to clear the default threshold.
func writeOpenGame(t *testing.T, gamesDir, slug, started string) {
	t.Helper()
	dir := filepath.Join(gamesDir, slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	fm := "---\ntitle: " + slug + "\nstatus: endless\n---\n"
	if err := os.WriteFile(filepath.Join(dir, "_index.md"), []byte(fm), 0o644); err != nil {
		t.Fatal(err)
	}
	pt := "playthroughs:\n  - started: \"" + started + "\"\n    finished: \"\"\n    status: playing\n"
	if err := os.WriteFile(filepath.Join(dir, model.PlaythroughsFilename), []byte(pt), 0o644); err != nil {
		t.Fatal(err)
	}
}

func finishedOf(t *testing.T, gamesDir, slug string) string {
	t.Helper()
	pf, err := model.LoadPlaythroughs(filepath.Join(gamesDir, slug))
	if err != nil {
		t.Fatal(err)
	}
	return pf.Playthroughs[0].Finished
}

func postClose(t *testing.T, s *server, entries ...string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"tab": {"close"}}
	for _, e := range entries {
		form.Add("entry", e)
	}
	req := httptest.NewRequest(http.MethodPost, "/close", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleCloseSelected(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	return rec
}

// The Close form used to post each row's position in the list, and the
// handler re-derived the list on submit. Anything that dropped an earlier row
// in between — closing it from its game page, say — shifted the positions,
// and the finished date landed on the row after the one that was ticked.
func TestCloseSelected_MatchesByKeyNotPosition(t *testing.T) {
	gamesDir := filepath.Join(t.TempDir(), "content", "games")
	// Oldest first, which is the list's sort order: alpha, bravo, charlie.
	writeOpenGame(t, gamesDir, "alpha", "2015-01-01")
	writeOpenGame(t, gamesDir, "bravo", "2016-01-01")
	writeOpenGame(t, gamesDir, "charlie", "2017-01-01")
	s := &server{gamesDir: gamesDir}

	// The page renders with bravo ticked alone; then alpha is closed
	// elsewhere before the form is submitted.
	games, err := model.ListGames(gamesDir)
	if err != nil {
		t.Fatal(err)
	}
	open, err := commands.FindOpenEntries(model.FindArchiveDir(gamesDir), gamesDir, games, commands.DefaultStaleDays, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 3 || open[1].Game.Slug != "bravo" {
		t.Fatalf("unexpected list: %+v", open)
	}
	if _, err := commands.CloseEntry(open[0]); err != nil {
		t.Fatal(err)
	}

	rec := postClose(t, s, open[1].Key())

	if got := finishedOf(t, gamesDir, "bravo"); got != "2016-01-01" {
		t.Errorf("bravo finished = %q, want it closed on 2016-01-01", got)
	}
	if got := finishedOf(t, gamesDir, "charlie"); got != "" {
		t.Errorf("charlie was closed (finished = %q) though it was never ticked", got)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, url.QueryEscape("Closed 1 of 1 selected.")) {
		t.Errorf("flash = %q", loc)
	}
}

// A ticked row that's no longer open by submit time is skipped and said so,
// and a bare position (what a page rendered before this fix would post) never
// resolves to anything.
func TestCloseSelected_SkipsStaleAndPositionalValues(t *testing.T) {
	gamesDir := filepath.Join(t.TempDir(), "content", "games")
	writeOpenGame(t, gamesDir, "alpha", "2015-01-01")
	s := &server{gamesDir: gamesDir}

	rec := postClose(t, s, "0", "gone:0")

	if got := finishedOf(t, gamesDir, "alpha"); got != "" {
		t.Errorf("alpha was closed by a positional value (finished = %q)", got)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, url.QueryEscape("2 no longer open, skipped.")) {
		t.Errorf("flash = %q", loc)
	}
}

// The checkbox has to carry the key, or the handler has nothing to match.
func TestHousekeepingCloseRowsPostKeys(t *testing.T) {
	tmpl, err := parsePage(pageSpecs["housekeeping"].name, pageSpecs["housekeeping"].partials...)
	if err != nil {
		t.Fatal(err)
	}
	data := housekeepingData{CloseEntries: []commands.OpenEntry{
		{Game: model.GameSummary{Slug: "alpha", Title: "Alpha"}, Index: 2, CloseOn: "2015-01-01"},
	}}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "layout", data); err != nil {
		t.Fatal(err)
	}
	if want := `name="entry" value="alpha:2"`; !strings.Contains(buf.String(), want) {
		t.Errorf("close row missing %q", want)
	}
}
