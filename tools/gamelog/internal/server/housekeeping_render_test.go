package server

import (
	"bytes"
	"strings"
	"testing"

	"go.dalton.dog/gamelog/internal/commands"
	"go.dalton.dog/gamelog/internal/forms"
	"go.dalton.dog/gamelog/internal/model"
)

// The scan and backlog rows follow the stale section's paradigm — the guess
// as the primary button, every other plausible status one click beside it,
// and a way out that isn't "create it and fix it later". That's all template
// wiring, so it's only checked by rendering the page.
func TestHousekeepingRenders(t *testing.T) {
	tmpl, err := parsePage(pageSpecs["housekeeping"].name, pageSpecs["housekeeping"].partials...)
	if err != nil {
		t.Fatal(err)
	}
	data := housekeepingData{
		MinHours: 5,
		ScanCandidates: scanRows([]commands.Candidate{
			{Provider: "RetroAchievements", Title: "Banjo", ID: "10210", Status: "finished", Finished: true, AwardKind: "beaten-hardcore", FinishedOn: "2026-01-02", AchievementsA: 12, AchievementsB: 189},
		}, forms.ScanQuickStatuses, "/scan", 5),
		BacklogCandidates: scanRows([]commands.Candidate{
			{Provider: "Steam", Title: "Tunic", ID: "553420", Status: "backlog"},
		}, forms.BacklogQuickStatuses, "/backlog", 5),
		Ignored: []model.IgnoredGame{{Provider: "steam", ID: "440", Title: "X", IgnoredOn: "2026-08-15"}},
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "layout", data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"Correct — create finished",
		`<option value="mastered">`,
		"Correct — create backlog",
		`<option value="unplayed">`,
		`name="use_status"`,
		"Never log this",
		"12/189 achievements",
		"finished 2026-01-02 (beaten-hardcore)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("page missing %q", want)
		}
	}
	if strings.Contains(out, `<option value="finished">`) {
		t.Error("the guess must not also appear in its own alternatives dropdown")
	}

	// The two one-click buttons sit in a row of their own and reach their
	// forms by id, so a typo in either half leaves a button that submits
	// nothing. Check both ends actually match up.
	for _, want := range []string{
		`id="create-retroachievements-10210"`,
		`form="create-retroachievements-10210"`,
		`id="ignore-retroachievements-10210"`,
		`form="ignore-retroachievements-10210"`,
		`id="create-steam-553420"`,
		`form="ignore-steam-553420"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("page missing %q", want)
		}
	}
}
