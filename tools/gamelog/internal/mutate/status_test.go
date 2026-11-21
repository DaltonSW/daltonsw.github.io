package mutate

import (
	"os"
	"path/filepath"
	"testing"

	"go.dalton.dog/gamelog/internal/model"
)

func writeGame(t *testing.T, status, playthroughs string) string {
	t.Helper()
	dir := t.TempDir()
	fm := "---\ntitle: G\nplatform: PC\nstatus: " + status + "\nrating:\ndraft: false\n---\n\nBody stays.\n"
	if err := os.WriteFile(filepath.Join(dir, "_index.md"), []byte(fm), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(model.PlaythroughsPath(dir), []byte(playthroughs), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func statusOf(t *testing.T, dir string) string {
	t.Helper()
	doc, err := model.LoadDoc(filepath.Join(dir, "_index.md"))
	if err != nil {
		t.Fatal(err)
	}
	return doc.FM.Status
}

// Closing the only open run of a "playing" game makes the game finished,
// with nothing but the status field changing in its front matter.
func TestWriteEntry_GameStatusFollowsItsRuns(t *testing.T) {
	dir := writeGame(t, "playing", "playthroughs:\n  - started: \"2024-01-01\"\n    status: playing\n")
	pf, err := model.LoadPlaythroughs(dir)
	if err != nil {
		t.Fatal(err)
	}
	pf.CloseLatestOpenRun("finished", "2024-02-01")
	if err := WriteEntry(pf); err != nil {
		t.Fatal(err)
	}
	if got := statusOf(t, dir); got != "finished" {
		t.Errorf("game status = %q, want finished", got)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "_index.md"))
	if want := "\nBody stays.\n"; len(raw) < len(want) || string(raw[len(raw)-len(want):]) != want {
		t.Errorf("body must be untouched:\n%s", raw)
	}
}

func TestWriteEntry_HandSetStatusIsLeftAlone(t *testing.T) {
	dir := writeGame(t, "endless", "playthroughs:\n  - started: \"2024-01-01\"\n    finished: \"2024-02-01\"\n    status: finished\n")
	pf, err := model.LoadPlaythroughs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteEntry(pf); err != nil {
		t.Fatal(err)
	}
	if got := statusOf(t, dir); got != "endless" {
		t.Errorf("a one-shot game's status must stay hand-set, got %q", got)
	}
}
