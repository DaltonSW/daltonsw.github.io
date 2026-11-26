package mutate

import (
	"os"
	"testing"

	"go.dalton.dog/gamelog/internal/model"
)

// A hand-typed flat entry is converted to a session on load; saving it must
// not trip the loss check on the flat paths that moved, while a field that
// really vanishes still must.
func TestVerifyNoLoss_FlatEntryMovesButRealLossStillFails(t *testing.T) {
	dir := t.TempDir()
	body := "playthroughs:\n  - started: \"2020-01-01\"\n    finished: \"2020-02-01\"\n    status: finished\n    notes: keep me\n"
	if err := os.WriteFile(model.PlaythroughsPath(dir), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	pf, err := model.LoadPlaythroughs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyNoLoss(pf, nil); err != nil {
		t.Fatalf("the flat pair moved into sessions[0], that is not loss: %v", err)
	}
	pf.Playthroughs[0].Notes = ""
	if err := VerifyNoLoss(pf, nil); err == nil {
		t.Fatal("dropping notes must still be refused")
	}
	pf.Playthroughs[0].Notes = "keep me"
	pf.Playthroughs[0].Sessions = nil
	if err := VerifyNoLoss(pf, nil); err == nil {
		t.Fatal("losing the dates altogether must still be refused")
	}
}
