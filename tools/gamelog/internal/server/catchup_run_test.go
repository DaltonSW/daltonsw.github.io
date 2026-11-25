package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.dalton.dog/gamelog/internal/model"
)

// None of these reach a provider: the one game has no links, and the refused
// requests never start a job.
func catchupRunServer(t *testing.T) (*server, string) {
	t.Helper()
	gamesDir := filepath.Join(t.TempDir(), "content", "games")
	dir := filepath.Join(gamesDir, "unlinked")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "_index.md"), []byte("---\ntitle: Unlinked\nstatus: playing\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return &server{gamesDir: gamesDir, jobs: newJobRegistry()}, model.FindArchiveDir(gamesDir)
}

func postCatchupRun(t *testing.T, s *server, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/catchup/run", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleCatchupRun(rec, req)
	return rec
}

// Loading one rarely-played source alone mustn't move "new since" for the
// sources it didn't load.
func TestCatchupRun_OwnButtonSourceIsNotRecordedAsARun(t *testing.T) {
	s, archiveDir := catchupRunServer(t)
	rec := postCatchupRun(t, s, url.Values{"only": {model.ProviderPSN}})
	if !strings.HasPrefix(rec.Header().Get("Location"), "/jobs/") {
		t.Fatalf("expected a redirect to the job, got %d %q", rec.Code, rec.Header().Get("Location"))
	}
	deadline := time.Now().Add(5 * time.Second)
	for s.jobs.running(s.catchupJobID) {
		if time.Now().After(deadline) {
			t.Fatal("job didn't finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if j := s.jobs.get(s.catchupJobID); j.Status != "done" || j.Title != "Catch up — loading PSN" {
		t.Errorf("job = %q %q", j.Status, j.Title)
	}
	st, err := model.LoadCatchup(archiveDir)
	if err != nil {
		t.Fatal(err)
	}
	if st.LastRun != "" {
		t.Errorf("last_run = %q; a single-source load must not be recorded", st.LastRun)
	}
}

func TestCatchupRun_RefusesWhatIsntOwnButtonOrTicked(t *testing.T) {
	for name, form := range map[string]url.Values{
		"regular source alone": {"only": {model.ProviderSteam}},
		// Own-button sources sent as checkboxes are dropped, leaving nothing.
		"only own-button boxes": {"load": {model.ProviderPSN, model.ProviderNadeo}},
	} {
		s, _ := catchupRunServer(t)
		rec := postCatchupRun(t, s, form)
		if s.catchupJobID != "" || strings.HasPrefix(rec.Header().Get("Location"), "/jobs/") {
			t.Errorf("%s: a job was started (%q)", name, rec.Header().Get("Location"))
		}
	}
}
