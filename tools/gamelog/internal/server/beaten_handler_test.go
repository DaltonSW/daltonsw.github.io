package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.dalton.dog/gamelog/internal/model"
)

func postUpdatePlaythrough(t *testing.T, s *server, slug string, form url.Values) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/games/"+slug+"/playthroughs/0", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("slug", slug)
	req.SetPathValue("idx", "0")
	rec := httptest.NewRecorder()
	s.handleUpdatePlaythrough(rec, req)
	return rec.Body.String()
}

// The update form sets a run's beaten date, and clearing it again is a
// declared removal, not something the loss check refuses. First finished
// follows it either way.
func TestUpdatePlaythrough_SetsAndClearsBeaten(t *testing.T) {
	gamesDir := filepath.Join(t.TempDir(), "content", "games")
	dir := filepath.Join(gamesDir, "ghost-trick")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "_index.md"), []byte("---\ntitle: Ghost Trick\nstatus: finished\n---\n"), 0o644)
	os.WriteFile(filepath.Join(dir, model.PlaythroughsFilename),
		[]byte("playthroughs:\n  - started: \"2026-09-28\"\n    finished: \"2026-10-24\"\n    status: finished\n"), 0o644)
	s := &server{gamesDir: gamesDir}
	form := func(beaten string) url.Values {
		return url.Values{"finished": {"2026-10-24"}, "status": {"finished"}, "beaten": {beaten}}
	}
	firstFinished := func() string {
		games, err := model.ListGames(gamesDir)
		if err != nil {
			t.Fatal(err)
		}
		return games[0].FirstFinished
	}

	if body := postUpdatePlaythrough(t, s, "ghost-trick", form("2026-10-12")); !strings.Contains(body, "row--saved") {
		t.Fatalf("setting beaten failed: %s", body)
	}
	if got := firstFinished(); got != "2026-10-12" {
		t.Errorf("first finished = %q, want the beaten date", got)
	}

	if body := postUpdatePlaythrough(t, s, "ghost-trick", form("")); !strings.Contains(body, "row--saved") {
		t.Fatalf("clearing beaten was refused: %s", body)
	}
	if got := firstFinished(); got != "2026-10-24" {
		t.Errorf("first finished = %q, want the run's finish again", got)
	}

	// Outside the run is refused, with the typed value kept on the form.
	body := postUpdatePlaythrough(t, s, "ghost-trick", form("2026-11-01"))
	if !strings.Contains(body, "after the run finished") || !strings.Contains(body, `value="2026-11-01"`) {
		t.Errorf("want a refusal that keeps the typed value, got %s", body)
	}
}
