package commands

import (
	"strings"
	"testing"

	"go.dalton.dog/gamelog/internal/providers/exophase"
)

func TestFormatExophaseList_Empty(t *testing.T) {
	got := FormatExophaseList("PSN", "psn_id", "DaltonSW", map[string]exophase.Game{})
	if !strings.Contains(got, `No PSN games found on Exophase profile "DaltonSW".`) {
		t.Errorf("got %q, want the no-games message", got)
	}
}

func TestFormatExophaseList_SortsByTitleAndShowsProgress(t *testing.T) {
	sekiro := exophase.Game{EarnedAwards: 30, TotalAwards: 39}
	sekiro.Meta.Title = "Sekiro: Shadows Die Twice"
	bloodborne := exophase.Game{EarnedAwards: 5, TotalAwards: 39}
	bloodborne.Meta.Title = "Bloodborne"

	games := map[string]exophase.Game{
		"NPWR15587_00": sekiro,
		"NPWR10391_00": bloodborne,
	}

	got := FormatExophaseList("PSN", "psn_id", "DaltonSW", games)

	bIdx := strings.Index(got, "Bloodborne")
	sIdx := strings.Index(got, "Sekiro")
	if bIdx == -1 || sIdx == -1 || bIdx > sIdx {
		t.Errorf("expected Bloodborne to sort before Sekiro, got:\n%s", got)
	}
	if !strings.Contains(got, "NPWR10391_00") || !strings.Contains(got, "5/39") {
		t.Errorf("expected Bloodborne's ID and progress in output, got:\n%s", got)
	}
	if !strings.Contains(got, "Paste the ID next to the game you want into psn_id") {
		t.Errorf("expected footer instructions, got:\n%s", got)
	}
}
