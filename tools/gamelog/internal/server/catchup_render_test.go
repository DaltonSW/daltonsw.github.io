package server

import (
	"bytes"
	"strings"
	"testing"

	"go.dalton.dog/gamelog/internal/commands"
	"go.dalton.dog/gamelog/internal/model"
)

func renderCatchup(t *testing.T, data catchupData) string {
	t.Helper()
	tmpl, err := parsePage(pageSpecs["catchup"].name, pageSpecs["catchup"].partials...)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "layout", data); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func sampleActivity(slug string, last string) commands.ActivityItem {
	return commands.ActivityItem{
		Game: model.GameSummary{Slug: slug, Title: strings.ToUpper(slug), Status: "finished"},
		Providers: []commands.ProviderActivity{
			{Provider: "steam", ID: "1", Label: "Steam", Platform: "PC", Days: 3, First: "2026-11-01", Last: last, LastPlayed: last},
		},
		Days: 3, First: "2026-11-01", Last: last, NewUnlocks: 4,
		Runs:  []model.Playthrough{{Index: 0, Status: "finished", Sessions: []model.Session{{Started: "2024-01-01", Finished: "2024-02-01"}}}},
		Guess: commands.ActivityGuess{Target: -1, Status: "playing", Started: "2026-11-01", FinishedHint: last},
	}
}

// The activity card is the point of the page: everything needed to log the
// activity is on the card, its two buttons reach their forms by id, and the
// run it could be a session on is offered by index.
func TestCatchupRenders(t *testing.T) {
	th := catchupThresholds{MinHours: 5, StaleDays: 30, CloseDays: 30}
	data := catchupData{
		Page:       Page{Title: "Catch up", Nav: "catchup"},
		Thresholds: th,
		Since:      "2026-11-03",
		LastRun:    "2026-11-22",
		New:        []activityCard{newActivityCard(sampleActivity("hades", "2026-11-20"), th)},
		Older:      []activityCard{newActivityCard(sampleActivity("celeste", "2019-01-01"), th)},
		Problems:   []commands.RecordProblem{{Provider: "psn", Error: "npsso expired", Games: []string{"A", "B", "C", "D"}}},
		Stale: staleRows([]commands.StaleCandidate{{
			Game: model.GameSummary{Slug: "tunic", Title: "Tunic", SteamAppID: "553420"}, LastPlayed: "2026-09-01",
			DaysSince: 82, SuggestedStatus: "dropped", Confidence: "medium",
		}}),
		Drafts: 2,
	}
	out := renderCatchup(t, data)
	for _, want := range []string{
		`aria-current="page">catch up</a>`,
		`action="/catchup/run"`,
		`id="load-steam" name="load" value="steam" checked>`,
		`id="load-ninjakiwi" name="load" value="ninjakiwi" checked>`,
		// Rarely played sources are buttons of their own, never run-wide boxes.
		`name="only" value="psn"`, `name="only" value="xbox"`, `name="only" value="ubisoft"`,
		`name="only" value="nadeo" title="current season; uses your play account">Trackmania</button>`,
		"Played since 2026-11-03",
		`id="log-hades"`, `form="log-hades"`, `hx-include="#log-hades"`,
		`id="dismiss-hades"`, `form="dismiss-hades"`, `hx-post="/catchup/dismiss/hades"`,
		`<option value="new" selected>new playthrough</option>`,
		`<option value="0">new session on #1 · finished</option>`,
		`value="2026-11-01"`,
		`placeholder="last activity 2026-11-20"`,
		"+4 achievements since last catch-up",
		`<input type="hidden" name="tab" value="activity">`,
		`<input type="hidden" name="from" value="catchup">`,
		"PSN: npsso expired — 4 games",
		`hx-get="/catchup/scan?min_hours=5"`,
		`action="/stale/tunic"`, "Correct — mark dropped",
		`<a href="/games?drafts=1">2 drafts awaiting review →</a>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("page missing %q", want)
		}
	}
	for _, own := range []string{`id="load-psn"`, `id="load-xbox"`, `id="load-ubisoft"`, `id="load-nadeo"`} {
		if strings.Contains(out, own) {
			t.Errorf("%s must be its own button, not a run-wide checkbox", own)
		}
	}
	// Older activity is there but out of the way.
	details := strings.Index(out, `<details class="catchup-older">`)
	if details < 0 || strings.Index(out, `id="log-celeste"`) < details {
		t.Error("older activity must render inside the collapsed section")
	}
	if strings.Contains(out, "all caught up") {
		t.Error("a page with work on it must not say it's caught up")
	}
}

func TestCatchupRenders_EmptySectionsDisappear(t *testing.T) {
	out := renderCatchup(t, catchupData{Since: "2026-11-03"})
	for _, absent := range []string{"<h2>Played since", "<h2>Gone quiet", "<h2>Left open", "<h2>Problems", "catchup-older", "drafts awaiting"} {
		if strings.Contains(out, absent) {
			t.Errorf("empty page still renders %q", absent)
		}
	}
	if !strings.Contains(out, "all caught up") {
		t.Error("an empty queue should say it's caught up")
	}
	// New games always loads lazily: whether it's empty isn't known yet.
	if !strings.Contains(out, `hx-trigger="load"`) {
		t.Error("New games must still be requested")
	}
}

// A card set to add a session starts with its status select disabled, since
// a session has no status and a disabled field isn't submitted.
func TestActivityCard_SessionGuessDisablesStatus(t *testing.T) {
	it := sampleActivity("spelunky", "2026-11-20")
	it.Guess = commands.ActivityGuess{Target: 0, Started: "2026-11-01", Finished: "2026-11-20"}
	out := renderCatchup(t, catchupData{New: []activityCard{newActivityCard(it, catchupThresholds{})}})
	if !strings.Contains(out, `<option value="0" selected>new session on #1`) {
		t.Error("the session target should be preselected")
	}
	if !strings.Contains(out, `aria-label="status" disabled`) {
		t.Error("status select should start disabled for a session")
	}
}
