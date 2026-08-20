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
		}, forms.ScanQuickStatuses, "/scan", 5, "scan"),
		BacklogCandidates: scanRows([]commands.Candidate{
			{Provider: "Steam", Title: "Tunic", ID: "553420", Status: "backlog"},
		}, forms.BacklogQuickStatuses, "/backlog", 5, "backlog"),
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

// A RetroAchievements subset row swaps its primary button for "attach to the
// game this belongs to" — creating it would make a second entry for a game
// that already has one. The other two parts of the row (create-as, never log
// this) stay, since the guess can still be wrong.
func TestHousekeepingRenders_SubsetRowOffersAttach(t *testing.T) {
	tmpl, err := parsePage(pageSpecs["housekeeping"].name, pageSpecs["housekeeping"].partials...)
	if err != nil {
		t.Fatal(err)
	}
	data := housekeepingData{
		MinHours: 5,
		ScanCandidates: scanRows([]commands.Candidate{
			{
				Provider: "RetroAchievements", Title: "Professor Layton and the Last Specter [Subset - Mouse Alley]",
				ID: "25709", Status: "playing", AchievementsA: 6, AchievementsB: 52,
				Subset: &commands.SubsetInfo{
					Name: "Mouse Alley", ParentID: "7601",
					BaseTitle: "Professor Layton and the Last Specter",
					BaseSlug:  "professor-layton-and-the-last-specter",
				},
			},
			// Same thing, base game not logged: the primary offers to create it.
			{
				Provider: "RetroAchievements", Title: "Some Game [Subset - Bonus]", ID: "99",
				Status: "playing",
				Subset: &commands.SubsetInfo{Name: "Bonus", ParentID: "12", BaseTitle: "Some Game"},
			},
			// Unresolvable parent: falls back to an ordinary row, because the
			// title convention alone is never enough to link one.
			{
				Provider: "RetroAchievements", Title: "Mystery [Subset - Huh]", ID: "77", Status: "playing",
				Subset: &commands.SubsetInfo{Name: "Huh", BaseTitle: "Mystery", Err: "no parent game"},
			},
		}, forms.ScanQuickStatuses, "/scan", 5, "scan"),
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "layout", data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	for _, want := range []string{
		`action="/subset/attach"`,
		`id="subset-retroachievements-25709"`,
		`form="subset-retroachievements-25709"`,
		`value="professor-layton-and-the-last-specter"`,
		"Attach to Professor Layton and the Last Specter",
		"subset of professor-layton-and-the-last-specter",
		`action="/subset/create-base"`,
		"Create Some Game + attach",
		"Never log this",
		// The unresolved one keeps the ordinary primary button.
		"Correct — create playing",
		"its parent could not be resolved",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("page missing %q", want)
		}
	}
	// Only the unresolved row keeps the create-it-as-a-game primary; the two
	// resolved subsets must not offer to duplicate a game as well as attach.
	if n := strings.Count(out, "Correct — create"); n != 1 {
		t.Errorf("create-as-a-game primary appears %d times, want 1 (the unresolvable row only)", n)
	}
}
