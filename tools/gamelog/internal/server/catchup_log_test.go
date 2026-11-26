package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.dalton.dog/gamelog/internal/model"
)

// writeActivityGame creates a Steam-linked game and an archive record with an
// unlock on each given day, so it has activity Catch up can offer.
func writeActivityGame(t *testing.T, gamesDir, slug, status, playthroughs string, unlockDays ...string) {
	t.Helper()
	dir := filepath.Join(gamesDir, slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	fm := "---\ntitle: " + slug + "\nstatus: " + status + "\nsteam_appid: 42\n---\n"
	if err := os.WriteFile(filepath.Join(dir, "_index.md"), []byte(fm), 0o644); err != nil {
		t.Fatal(err)
	}
	if playthroughs != "" {
		if err := os.WriteFile(filepath.Join(dir, model.PlaythroughsFilename), []byte(playthroughs), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rec := model.ArchiveRecord{Provider: "steam", Title: slug}
	rec.ID = "42"
	for i, d := range unlockDays {
		rec.Achievements = append(rec.Achievements, model.ArchivedAchievement{
			Key: string(rune('a' + i)), Name: "ach", Unlocked: true, Date: d + "T20:00:00-06:00",
		})
	}
	path := model.RecordPath(model.FindArchiveDir(gamesDir), "steam", "42")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(rec)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func postCatchup(t *testing.T, h http.HandlerFunc, path, slug string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	form.Set("tab", "activity")
	form.Set("from", "catchup")
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("slug", slug)
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func TestCatchupLog_NewPlaythrough(t *testing.T) {
	gamesDir := filepath.Join(t.TempDir(), "content", "games")
	writeActivityGame(t, gamesDir, "tunic", "backlog", "", "2026-10-01", "2026-10-09")
	s := &server{gamesDir: gamesDir}

	rec := postCatchup(t, s.handleCatchupLog, "/catchup/log/tunic", "tunic", url.Values{
		"target": {"new"}, "started": {"2026-10-01"}, "finished": {"2026-10-09"}, "status": {"finished"},
	})
	if !strings.Contains(rec.Body.String(), "row--saved") {
		t.Fatalf("want a saved result, got %s", rec.Body.String())
	}
	pf, err := model.LoadPlaythroughs(filepath.Join(gamesDir, "tunic"))
	if err != nil {
		t.Fatal(err)
	}
	if len(pf.Playthroughs) != 1 || len(pf.Playthroughs[0].Sessions) != 1 || pf.Playthroughs[0].Sessions[0].Started != "2026-10-01" || pf.Playthroughs[0].Sessions[0].Finished != "2026-10-09" {
		t.Fatalf("playthroughs = %+v", pf.Playthroughs)
	}
	// The game's status follows its new run (WriteEntry → SyncGameStatus).
	doc, err := model.LoadDoc(filepath.Join(gamesDir, "tunic", "_index.md"))
	if err != nil {
		t.Fatal(err)
	}
	if doc.FM.Status != "finished" {
		t.Errorf("game status = %q, want finished (derived from the run)", doc.FM.Status)
	}
}

func TestCatchupLog_Session(t *testing.T) {
	gamesDir := filepath.Join(t.TempDir(), "content", "games")
	writeActivityGame(t, gamesDir, "spelunky", "endless",
		"playthroughs:\n  - status: endless\n    sessions:\n      - started: \"2024-01-01\"\n        finished: \"2024-01-03\"\n",
		"2026-11-01")
	s := &server{gamesDir: gamesDir}

	rec := postCatchup(t, s.handleCatchupLog, "/catchup/log/spelunky", "spelunky", url.Values{
		"target": {"0"}, "started": {"2026-11-01"}, "finished": {"2026-11-01"},
	})
	if !strings.Contains(rec.Body.String(), "row--saved") {
		t.Fatalf("want a saved result, got %s", rec.Body.String())
	}
	pf, err := model.LoadPlaythroughs(filepath.Join(gamesDir, "spelunky"))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(pf.Playthroughs[0].Sessions); n != 2 {
		t.Fatalf("want the session appended to run #1, got %d sessions", n)
	}
}

// Extending a bordering session widens it in place — no new session, and a
// started date inside it doesn't pull its start forward.
func TestCatchupLog_ExtendSession(t *testing.T) {
	gamesDir := filepath.Join(t.TempDir(), "content", "games")
	writeActivityGame(t, gamesDir, "hades", "playing",
		"playthroughs:\n  - status: paused\n    sessions:\n      - started: \"2026-11-10\"\n        finished: \"2026-11-14\"\n        title: first stint\n",
		"2026-11-15")
	s := &server{gamesDir: gamesDir}

	rec := postCatchup(t, s.handleCatchupLog, "/catchup/log/hades", "hades", url.Values{
		"target": {"extend:0:0"}, "started": {"2026-11-15"}, "finished": {"2026-11-15"},
	})
	if !strings.Contains(rec.Body.String(), "row--saved") {
		t.Fatalf("want a saved result, got %s", rec.Body.String())
	}
	pf, err := model.LoadPlaythroughs(filepath.Join(gamesDir, "hades"))
	if err != nil {
		t.Fatal(err)
	}
	ss := pf.Playthroughs[0].Sessions
	if len(ss) != 1 || ss[0].Started != "2026-11-10" || ss[0].Finished != "2026-11-15" || ss[0].Title != "first stint" {
		t.Fatalf("sessions = %+v, want one 2026-11-10 → 2026-11-15 keeping its title", ss)
	}
}

// A refused write re-renders the card with what was typed and the reason,
// rather than swapping it for a one-line error that throws the form away.
func TestCatchupLog_ErrorKeepsTheCard(t *testing.T) {
	gamesDir := filepath.Join(t.TempDir(), "content", "games")
	writeActivityGame(t, gamesDir, "hades", "endless",
		"playthroughs:\n  - started: \"2024-01-01\"\n    finished: \"2024-01-03\"\n    status: endless\n",
		"2026-11-01")
	s := &server{gamesDir: gamesDir}

	// A second endless entry on the same platform is a one-shot conflict.
	rec := postCatchup(t, s.handleCatchupLog, "/catchup/log/hades", "hades", url.Values{
		"target": {"new"}, "started": {"2026-11-01"}, "status": {"endless"}, "notes": {"typed by hand"},
	})
	body := rec.Body.String()
	for _, want := range []string{"row--error", "log a new session instead", `value="typed by hand"`, `id="log-hades"`} {
		if !strings.Contains(body, want) {
			t.Errorf("errored card missing %q:\n%s", want, body)
		}
	}
	pf, _ := model.LoadPlaythroughs(filepath.Join(gamesDir, "hades"))
	if len(pf.Playthroughs) != 1 {
		t.Error("a refused write must not have written anything")
	}
}

func TestCatchupDismiss_WritesReviewed(t *testing.T) {
	gamesDir := filepath.Join(t.TempDir(), "content", "games")
	writeActivityGame(t, gamesDir, "celeste", "finished",
		"playthroughs:\n  - started: \"2020-01-01\"\n    finished: \"2020-02-01\"\n    status: finished\n",
		"2018-06-01", "2018-07-01")
	s := &server{gamesDir: gamesDir}

	rec := postCatchup(t, s.handleCatchupDismiss, "/catchup/dismiss/celeste", "celeste", url.Values{})
	if !strings.Contains(rec.Body.String(), "dismissed activity through 2018-07-01") {
		t.Fatalf("got %s", rec.Body.String())
	}
	st, err := model.LoadCatchup(model.FindArchiveDir(gamesDir))
	if err != nil {
		t.Fatal(err)
	}
	if got := st.ReviewedThrough("steam", "42"); got != "2018-07-01" {
		t.Errorf("reviewed through = %q, want 2018-07-01", got)
	}
	if it, _, _ := s.activityFor("celeste", catchupThresholds{StaleDays: 30}); it != nil {
		t.Error("dismissed activity must not be offered again")
	}
	// Nothing in the game's own files changed.
	pf, _ := model.LoadPlaythroughs(filepath.Join(gamesDir, "celeste"))
	if len(pf.Playthroughs) != 1 || pf.Views()[0].FinishDate() != "2020-02-01" {
		t.Error("dismissing must not touch playthroughs.yaml")
	}
}
