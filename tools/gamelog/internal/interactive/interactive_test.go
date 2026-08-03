package interactive

import (
	"testing"

	"go.dalton.dog/gamelog/internal/model"
)

func TestHasManageableSessions(t *testing.T) {
	if hasManageableSessions(nil) {
		t.Error("no playthroughs means nothing to manage")
	}
	if hasManageableSessions([]model.Playthrough{{Started: "2024-01-01"}}) {
		t.Error("a flat entry with no sessions has nothing 'Manage sessions' can act on")
	}
	if !hasManageableSessions([]model.Playthrough{{Sessions: []model.Session{{Started: "2024-01-01"}}}}) {
		t.Error("an entry with sessions should be manageable")
	}
}
