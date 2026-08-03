package commands

import (
	"strings"
	"testing"

	"go.dalton.dog/gamelog/internal/providers/psn"
)

func TestFormatPSNList_Empty(t *testing.T) {
	got := FormatPSNList(map[string]psn.TrophyTitle{})
	if !strings.Contains(got, "No PSN trophy titles found on this account.") {
		t.Errorf("got %q, want the no-titles message", got)
	}
}

func TestFormatPSNList_SortsByTitleAndShowsProgress(t *testing.T) {
	sekiro := psn.TrophyTitle{
		TrophyTitleName: "Sekiro: Shadows Die Twice",
		EarnedTrophies:  psn.TrophyCounts{Bronze: 26, Silver: 3, Gold: 1},
		DefinedTrophies: psn.TrophyCounts{Bronze: 30, Silver: 7, Gold: 1, Platinum: 1},
	}
	bloodborne := psn.TrophyTitle{
		TrophyTitleName: "Bloodborne",
		EarnedTrophies:  psn.TrophyCounts{Bronze: 5},
		DefinedTrophies: psn.TrophyCounts{Bronze: 30, Silver: 7, Gold: 1, Platinum: 1},
	}

	titles := map[string]psn.TrophyTitle{
		"NPWR15587_00": sekiro,
		"NPWR10391_00": bloodborne,
	}

	got := FormatPSNList(titles)

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
