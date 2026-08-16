package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.dalton.dog/gamelog/internal/model"
)

// The two scan modes must partition the library exactly: every playtime lands
// in one list or the other, never both, never neither. The threshold itself
// counts as played.
func TestScanOptions_ModesPartitionTheLibrary(t *testing.T) {
	for _, mins := range []int{0, 1, 299, 300, 301, 6000} {
		played := ScanOptions{MinHours: 5, Mode: ScanModePlayed}.keeps(mins)
		backlog := ScanOptions{MinHours: 5, Mode: ScanModeBacklog}.keeps(mins)
		if played == backlog {
			t.Errorf("%d mins: played=%v backlog=%v — must be in exactly one list", mins, played, backlog)
		}
	}

	if !(ScanOptions{MinHours: 5, Mode: ScanModePlayed}).keeps(300) {
		t.Error("exactly the threshold should count as played, not backlog")
	}
	if !(ScanOptions{MinHours: 5, Mode: ScanModeBacklog}).keeps(299) {
		t.Error("just under the threshold should be backlog")
	}
	// The zero value has to keep meaning the original scan.
	if !(ScanOptions{MinHours: 5}).keeps(6000) {
		t.Error("unset Mode should behave as ScanModePlayed")
	}
}

// A backlog entry is a claim about intent, not history: `status: backlog`,
// still a draft, and no finished date to imply it was ever played.
func TestBacklogCandidate_CreatesBacklogDraft(t *testing.T) {
	dir := t.TempDir()
	c := Candidate{Provider: "Steam", Title: "Outer Wilds", Platform: "PC", ID: "753640", Status: "backlog"}

	path, err := model.CreateGameFile(dir, model.Slugify(c.Title), c.NewGameFields())
	if err != nil {
		t.Fatal(err)
	}
	doc, err := model.LoadDoc(path)
	if err != nil {
		t.Fatalf("generated file does not parse: %v", err)
	}
	if doc.FM.Status != "backlog" {
		t.Errorf("status = %q, want backlog", doc.FM.Status)
	}
	if doc.FM.Finished != "" {
		t.Errorf("a backlog entry must have no finished date, got %q", doc.FM.Finished)
	}
	if !doc.FM.Draft {
		t.Error("a backlog entry must be created as a draft")
	}
	// A backlog game is defined by having no history at all — creating one
	// must not leave an empty playthroughs.yaml behind to contradict that.
	if _, err := os.Stat(model.PlaythroughsPath(filepath.Dir(path))); !os.IsNotExist(err) {
		t.Errorf("expected no playthroughs.yaml beside a backlog entry (stat err %v)", err)
	}
}

// Only records with a real denominator and something left to earn qualify,
// and only games whose status doesn't already say they're over.
func TestFindUnfinished_FiltersToGamesWithSomethingLeft(t *testing.T) {
	archiveDir := t.TempDir()
	save := func(id, title string, total, unlocked int) {
		t.Helper()
		if _, err := model.SaveRecord(archiveDir, model.ProviderSteam, id, title, &model.ProviderRecord{
			Total: total, Achievements: nUnlocked("a"+id, unlocked), LastPlayed: daysAgo(10),
		}); err != nil {
			t.Fatal(err)
		}
	}
	save("1", "Partial", 10, 6)
	save("2", "Complete", 10, 10)
	save("3", "No Achievements", 0, 0)
	save("4", "Endless", 10, 6)
	save("5", "Finished", 10, 6)
	save("6", "Paused", 10, 6)

	games := []model.GameSummary{
		{Slug: "partial", Title: "Partial", Status: "playing", SteamAppID: "1"},
		{Slug: "complete", Title: "Complete", Status: "playing", SteamAppID: "2"},
		{Slug: "none", Title: "No Achievements", Status: "playing", SteamAppID: "3"},
		{Slug: "endless", Title: "Endless", Status: "endless", SteamAppID: "4"},
		{Slug: "finished", Title: "Finished", Status: "finished", SteamAppID: "5"},
		{Slug: "paused", Title: "Paused", Status: "paused", SteamAppID: "6"},
		{Slug: "unarchived", Title: "Unarchived", Status: "playing", SteamAppID: "999"},
	}

	got, err := FindUnfinished(archiveDir, games, UnfinishedOptions{})
	if err != nil {
		t.Fatal(err)
	}
	gotSlugs := map[string]bool{}
	for _, c := range got {
		gotSlugs[c.Game.Slug] = true
	}
	want := map[string]bool{"partial": true, "paused": true}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %+v", want, got)
	}
	for slug := range want {
		if !gotSlugs[slug] {
			t.Errorf("expected %q among candidates, got %+v", slug, got)
		}
	}
}

// "finished" is a decision, not an oversight, so those rows are opt-in.
func TestFindUnfinished_IncludeDone(t *testing.T) {
	archiveDir := t.TempDir()
	if _, err := model.SaveRecord(archiveDir, model.ProviderSteam, "1", "Finished", &model.ProviderRecord{
		Total: 10, Achievements: nUnlocked("a", 6),
	}); err != nil {
		t.Fatal(err)
	}
	games := []model.GameSummary{{Slug: "finished", Title: "Finished", Status: "finished", SteamAppID: "1"}}

	if got, err := FindUnfinished(archiveDir, games, UnfinishedOptions{}); err != nil || len(got) != 0 {
		t.Fatalf("finished game should be excluded by default, got %+v (err %v)", got, err)
	}
	got, err := FindUnfinished(archiveDir, games, UnfinishedOptions{IncludeDone: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("IncludeDone should surface the finished game, got %+v", got)
	}
}

func TestFindUnfinished_MinPct(t *testing.T) {
	archiveDir := t.TempDir()
	if _, err := model.SaveRecord(archiveDir, model.ProviderSteam, "1", "Barely Started", &model.ProviderRecord{
		Total: 100, Achievements: nUnlocked("a", 4),
	}); err != nil {
		t.Fatal(err)
	}
	games := []model.GameSummary{{Slug: "barely", Title: "Barely Started", Status: "playing", SteamAppID: "1"}}

	if got, err := FindUnfinished(archiveDir, games, UnfinishedOptions{MinPct: DefaultUnfinishedPct}); err != nil || len(got) != 0 {
		t.Fatalf("4%% should fall below the default floor, got %+v (err %v)", got, err)
	}
	if got, err := FindUnfinished(archiveDir, games, UnfinishedOptions{}); err != nil || len(got) != 1 {
		t.Fatalf("no floor should keep it, got %+v (err %v)", got, err)
	}
}

// One row per provider: a game linked to two services has two independent
// denominators, and each is worth reporting on its own terms.
func TestFindUnfinished_RowPerProvider(t *testing.T) {
	archiveDir := t.TempDir()
	if _, err := model.SaveRecord(archiveDir, model.ProviderSteam, "1", "Both", &model.ProviderRecord{
		Total: 10, Achievements: nUnlocked("s", 6),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := model.SaveRecord(archiveDir, model.ProviderRA, "2", "Both", &model.ProviderRecord{
		Total: 20, Achievements: nUnlocked("r", 15),
	}); err != nil {
		t.Fatal(err)
	}
	games := []model.GameSummary{{Slug: "both", Title: "Both", Status: "playing", SteamAppID: "1", RAGameID: "2"}}

	got, err := FindUnfinished(archiveDir, games, UnfinishedOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected one row per provider, got %+v", got)
	}
	// 75% (RA) is closer to done than 60% (Steam), so it leads.
	if got[0].Provider != model.ProviderRA || got[1].Provider != model.ProviderSteam {
		t.Errorf("expected the closer-to-done provider first, got %s then %s", got[0].Provider, got[1].Provider)
	}
	if got[0].Remaining != 5 || got[1].Remaining != 4 {
		t.Errorf("remaining counts wrong: %d and %d", got[0].Remaining, got[1].Remaining)
	}
}

func TestFindUnfinished_SortsClosestToDoneFirst(t *testing.T) {
	archiveDir := t.TempDir()
	for _, g := range []struct {
		id              string
		total, unlocked int
	}{{"1", 10, 3}, {"2", 10, 9}, {"3", 10, 6}} {
		if _, err := model.SaveRecord(archiveDir, model.ProviderSteam, g.id, "Game "+g.id, &model.ProviderRecord{
			Total: g.total, Achievements: nUnlocked("a"+g.id, g.unlocked),
		}); err != nil {
			t.Fatal(err)
		}
	}
	games := []model.GameSummary{
		{Slug: "a", Title: "A", Status: "playing", SteamAppID: "1"},
		{Slug: "b", Title: "B", Status: "playing", SteamAppID: "2"},
		{Slug: "c", Title: "C", Status: "playing", SteamAppID: "3"},
	}

	got, err := FindUnfinished(archiveDir, games, UnfinishedOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, c := range got {
		order = append(order, c.Game.Slug)
	}
	if strings.Join(order, ",") != "b,c,a" {
		t.Errorf("expected b,c,a (90%%, 60%%, 30%%), got %v", order)
	}
}
