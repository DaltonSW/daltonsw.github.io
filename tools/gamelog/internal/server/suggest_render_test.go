package server

import (
	"bytes"
	"errors"
	"html/template"
	"strings"
	"testing"
	"time"

	"go.dalton.dog/gamelog/internal/commands"
	"go.dalton.dog/gamelog/internal/model"
	"go.dalton.dog/gamelog/internal/providers/retroachievements"
	"go.dalton.dog/gamelog/internal/providers/steam"
)

// Noon UTC lands on the same calendar date in any plausible site timezone,
// so these dates survive commands.Day's conversion unchanged.
func noon(date string) time.Time {
	t, err := time.Parse("2006-01-02 15:04", date+" 12:00")
	if err != nil {
		panic(err)
	}
	return t
}

func renderSuggestPanel(t *testing.T, v suggestView) string {
	t.Helper()
	tmpl, err := template.New("suggest_fragments.html").Funcs(funcMap).
		ParseFS(templatesFS, "web/templates/suggest_fragments.html")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "suggest_panel", v); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func suggestTestGame(t *testing.T) (*model.Doc, *model.PlaythroughsFile) {
	t.Helper()
	pf := &model.PlaythroughsFile{}
	pf.AddPlaythrough(model.PlaythroughFields{Started: "2020-01-01", Finished: "2020-02-01", Status: "finished"})
	pf.AddPlaythrough(model.PlaythroughFields{Status: "planned"})
	pf.AddPlaythrough(model.PlaythroughFields{Started: "2023-05-01", Status: "playing", Platform: "PC"})
	return &model.Doc{FM: model.FrontMatter{Title: "Banjo", Platform: "N64"}}, pf
}

// The panel exists so a suggestion can be logged without retyping it: the
// forms must carry the provider's dates, post to the existing handlers, and
// never claim a finish the provider didn't signal.
func TestSuggestPanelPrefills(t *testing.T) {
	doc, pf := suggestTestGame(t)
	report := commands.SuggestionReport{
		Slug: "banjo", NumPlaythroughs: 2,
		RA: commands.ProviderResult{RA: &retroachievements.RASuggestion{
			Started: noon("2024-01-10"), Finished: noon("2024-03-02"), Confidence: "high", AwardKind: "mastered", OK: true,
		}},
		Steam: commands.ProviderResult{
			Steam:                &steam.SteamSuggestion{Started: noon("2024-01-11"), Finished: noon("2024-02-20"), UnlockedCount: 12, TotalCount: 40, OK: true},
			SteamPlaytimeMinutes: 90, SteamPlaytimeKnown: true,
		},
	}
	v := buildSuggestView(report, doc, pf, "playing")

	ra, st := v.Providers[0], v.Providers[1]
	if ra.Started != "2024-01-10" || ra.Finished != "2024-03-02" || ra.Status != "mastered" {
		t.Errorf("RA prefill = %+v", ra)
	}
	if st.Finished != "" || st.FinishedHint != "2024-02-20" || st.Status != "playing" {
		t.Errorf("partial Steam must not prefill a finish: %+v", st)
	}

	// Planned entries can't take a session; the newest played one is the default.
	if len(v.SessionTargets) != 2 || !v.SessionTargets[1].Selected ||
		v.SessionTargets[1].Action != "/games/banjo/playthroughs/2/sessions" {
		t.Errorf("session targets = %+v", v.SessionTargets)
	}

	out := renderSuggestPanel(t, v)
	for _, want := range []string{
		`action="/games/banjo/playthroughs"`,
		`value="2024-01-10"`,
		`value="2024-03-02"`,
		`<option value="mastered" selected>`,
		`value="dates suggested from RetroAchievements (mastered)"`,
		`placeholder="last activity 2024-02-20"`,
		`12/40 achievements`,
		`1.5h total playtime`,
		`action="/games/banjo/playthroughs/2/sessions"`,
		`#3 · playing · PC`,
		"several playthroughs logged",
		`hx-get="/games/banjo/suggest"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("panel missing %q", want)
		}
	}
	if strings.Contains(out, "planned") {
		t.Error("a planned entry was offered as a session target")
	}
}

func TestSuggestPanelSkippedProviders(t *testing.T) {
	doc := &model.Doc{FM: model.FrontMatter{Title: "Tunic"}}
	report := commands.SuggestionReport{
		Slug:  "tunic",
		RA:    commands.ProviderResult{Skipped: true, Reason: "no retroachievements_id set on this game"},
		Steam: commands.ProviderResult{Errored: true, Err: errors.New("503")},
	}
	out := renderSuggestPanel(t, buildSuggestView(report, doc, &model.PlaythroughsFile{}, "endless"))
	for _, want := range []string{"no retroachievements_id set on this game", "request failed: 503"} {
		if !strings.Contains(out, want) {
			t.Errorf("panel missing %q", want)
		}
	}
	if strings.Contains(out, "<form") {
		t.Error("a provider with no dates must not offer a form")
	}
}
