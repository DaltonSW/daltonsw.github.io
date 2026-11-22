package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.dalton.dog/gamelog/internal/model"
)

// activityFixture is a throwaway repo: content/games/<slug>/ for each game and
// archive/<provider>/<id>.json for each record, laid out where ListGames and
// FindArchiveDir expect them.
type activityFixture struct {
	t        *testing.T
	gamesDir string
}

func newActivityFixture(t *testing.T) *activityFixture {
	t.Helper()
	return &activityFixture{t: t, gamesDir: filepath.Join(t.TempDir(), "content", "games")}
}

func (f *activityFixture) archiveDir() string { return model.FindArchiveDir(f.gamesDir) }

// game writes _index.md with the given extra front-matter lines and, when
// playthroughs is non-empty, a playthroughs.yaml body.
func (f *activityFixture) game(slug, status string, frontMatter []string, playthroughs string) {
	f.t.Helper()
	dir := filepath.Join(f.gamesDir, slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		f.t.Fatal(err)
	}
	fm := "---\ntitle: " + slug + "\nstatus: " + status + "\n" + strings.Join(frontMatter, "\n") + "\n---\n"
	if err := os.WriteFile(filepath.Join(dir, "_index.md"), []byte(fm), 0o644); err != nil {
		f.t.Fatal(err)
	}
	if playthroughs != "" {
		if err := os.WriteFile(filepath.Join(dir, model.PlaythroughsFilename), []byte(playthroughs), 0o644); err != nil {
			f.t.Fatal(err)
		}
	}
}

// record writes an archive record whose unlocks fall on the given days.
func (f *activityFixture) record(provider, id string, edit func(*model.ArchiveRecord), unlockDays ...string) {
	f.t.Helper()
	rec := model.ArchiveRecord{Provider: provider, Title: id}
	rec.ID = id
	for i, d := range unlockDays {
		rec.Achievements = append(rec.Achievements, model.ArchivedAchievement{
			Key: string(rune('a' + i)), Name: "ach", Unlocked: true, Date: d + "T20:00:00-06:00",
		})
	}
	rec.Achievements = append(rec.Achievements, model.ArchivedAchievement{Key: "locked", Name: "locked"})
	if edit != nil {
		edit(&rec)
	}
	path := model.RecordPath(f.archiveDir(), provider, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *activityFixture) find(opts ActivityOptions) ActivityReport {
	f.t.Helper()
	games, err := model.ListGames(f.gamesDir)
	if err != nil {
		f.t.Fatal(err)
	}
	if opts.Now.IsZero() {
		opts.Now = date("2026-11-22")
	}
	if opts.StaleDays == 0 {
		opts.StaleDays = DefaultStaleDays
	}
	rep, err := FindUnloggedActivity(f.archiveDir(), games, opts)
	if err != nil {
		f.t.Fatal(err)
	}
	return rep
}

func (f *activityFixture) item(rep ActivityReport, slug string) *ActivityItem {
	for i := range rep.Items {
		if rep.Items[i].Game.Slug == slug {
			return &rep.Items[i]
		}
	}
	return nil
}

func TestFindUnloggedActivity_Coverage(t *testing.T) {
	f := newActivityFixture(t)

	// Every unlock inside a closed run: nothing to do.
	f.game("covered", "finished", []string{"steam_appid: 1"},
		"playthroughs:\n  - started: \"2024-03-01\"\n    finished: \"2024-04-01\"\n    status: finished\n")
	f.record("steam", "1", nil, "2024-03-02", "2024-03-20")

	// An open run covers everything after it (Celeste's shape).
	f.game("open", "playing", []string{"steam_appid: 2"},
		"playthroughs:\n  - started: \"2024-03-01\"\n    finished: \"\"\n    status: playing\n")
	f.record("steam", "2", nil, "2024-03-02", "2026-11-20")

	// Activity in the gap between two logged sessions.
	f.game("gap", "finished", []string{"steam_appid: 3"},
		"playthroughs:\n  - status: finished\n    sessions:\n      - started: \"2024-02-01\"\n        finished: \"2024-02-15\"\n      - started: \"2024-05-20\"\n        finished: \"2024-06-01\"\n")
	f.record("steam", "3", nil, "2024-02-03", "2024-04-10", "2024-04-12", "2024-05-25")

	// Nothing logged at all.
	f.game("unlogged", "backlog", []string{"steam_appid: 4"}, "")
	f.record("steam", "4", nil, "2026-11-10")

	rep := f.find(ActivityOptions{})
	if f.item(rep, "covered") != nil {
		t.Error("activity inside a closed run must be covered")
	}
	if f.item(rep, "open") != nil {
		t.Error("an open run covers later activity")
	}
	gap := f.item(rep, "gap")
	if gap == nil {
		t.Fatal("activity between logged sessions must be flagged")
	}
	if gap.First != "2024-04-10" || gap.Last != "2024-04-12" || gap.Days != 2 {
		t.Errorf("gap range = %s → %s (%d days), want 2024-04-10 → 2024-04-12 (2)", gap.First, gap.Last, gap.Days)
	}
	if f.item(rep, "unlogged") == nil {
		t.Error("a game with no playthroughs and archived activity must be flagged")
	}
	if rep.Items[0].Game.Slug != "unlogged" {
		t.Errorf("items sort newest activity first, got %s first", rep.Items[0].Game.Slug)
	}
}

// last_played is a real played day even with no unlock behind it, so a game
// picked back up without earning anything still surfaces.
func TestFindUnloggedActivity_LastPlayedAlone(t *testing.T) {
	f := newActivityFixture(t)
	f.game("replayed", "finished", []string{"steam_appid: 9"},
		"playthroughs:\n  - started: \"2020-01-01\"\n    finished: \"2020-02-01\"\n    status: finished\n")
	f.record("steam", "9", func(r *model.ArchiveRecord) { r.LastPlayed = "2026-11-18" }, "2020-01-10")

	it := f.item(f.find(ActivityOptions{}), "replayed")
	if it == nil {
		t.Fatal("last_played past every logged run must be flagged")
	}
	if it.First != "2026-11-18" || it.Days != 1 {
		t.Errorf("got %s (%d days), want just 2026-11-18", it.First, it.Days)
	}
}

func TestFindUnloggedActivity_Award(t *testing.T) {
	f := newActivityFixture(t)
	// An uncovered award pre-fills a finished run ending on it.
	f.game("beaten", "backlog", []string{"retroachievements_id: 10"}, "")
	f.record("retroachievements", "10", func(r *model.ArchiveRecord) {
		r.AwardKind, r.AwardDate = "beaten-hardcore", "2026-11-12"
	}, "2026-11-01", "2026-11-12")

	// An award inside a logged run belongs to that run; later activity
	// doesn't inherit it.
	f.game("replay", "finished", []string{"retroachievements_id: 11"},
		"playthroughs:\n  - started: \"2024-01-01\"\n    finished: \"2024-02-01\"\n    status: finished\n")
	f.record("retroachievements", "11", func(r *model.ArchiveRecord) {
		r.AwardKind, r.AwardDate = "mastered", "2024-01-30"
	}, "2024-01-05", "2026-11-15")

	rep := f.find(ActivityOptions{})
	beaten := f.item(rep, "beaten")
	if beaten == nil || beaten.Award != "2026-11-12" {
		t.Fatalf("want the uncovered award, got %+v", beaten)
	}
	if g := beaten.Guess; g.Target != -1 || g.Status != "finished" || g.Finished != "2026-11-12" || g.Started != "2026-11-01" {
		t.Errorf("guess = %+v, want a finished run 2026-11-01 → 2026-11-12", g)
	}
	replay := f.item(rep, "replay")
	if replay == nil {
		t.Fatal("activity after the logged run must be flagged")
	}
	if replay.Award != "" || replay.Guess.Status == "finished" {
		t.Errorf("a covered award must not pre-fill a finish: %+v", replay.Guess)
	}
}

// Reviewed days are hidden; newer activity on the same record comes back.
func TestFindUnloggedActivity_Reviewed(t *testing.T) {
	f := newActivityFixture(t)
	f.game("hades", "finished", []string{"steam_appid: 1145360"},
		"playthroughs:\n  - started: \"2020-01-01\"\n    finished: \"2020-02-01\"\n    status: finished\n")
	f.record("steam", "1145360", nil, "2018-06-01", "2018-07-01")

	var st model.CatchupState
	st.AddReviewed(model.ReviewedActivity{Provider: "steam", ID: "1145360", Through: "2018-07-01"})
	if f.item(f.find(ActivityOptions{Reviewed: st}), "hades") != nil {
		t.Fatal("activity reviewed through its newest day must not be offered")
	}

	f.record("steam", "1145360", nil, "2018-06-01", "2018-07-01", "2026-11-19")
	it := f.item(f.find(ActivityOptions{Reviewed: st}), "hades")
	if it == nil || it.First != "2026-11-19" || it.Days != 1 {
		t.Fatalf("newer activity must come back on its own, got %+v", it)
	}
	if it.Providers[0].Latest != "2026-11-19" {
		t.Errorf("Latest = %s, want 2026-11-19", it.Providers[0].Latest)
	}
}

func TestFindUnloggedActivity_Guesses(t *testing.T) {
	f := newActivityFixture(t)
	// A one-shot game: a session on its run, not a second entry.
	f.game("spelunky", "endless", []string{"steam_appid: 20"},
		"playthroughs:\n  - status: endless\n    sessions:\n      - started: \"2024-01-01\"\n        finished: \"2024-01-03\"\n")
	f.record("steam", "20", nil, "2026-11-01", "2026-11-04")

	// Old activity that stopped long ago: a closed, unfinished run.
	f.game("old", "backlog", []string{"steam_appid: 21"}, "")
	f.record("steam", "21", nil, "2016-03-01", "2016-04-02")

	// Recent activity: an open run with the default status.
	f.game("recent", "backlog", []string{"steam_appid: 22"}, "")
	f.record("steam", "22", nil, "2026-11-15", "2026-11-20")

	rep := f.find(ActivityOptions{})
	if g := f.item(rep, "spelunky").Guess; g.Target != 0 || g.Started != "2026-11-01" || g.Finished != "2026-11-04" {
		t.Errorf("one-shot guess = %+v, want a session on #0 2026-11-01 → 2026-11-04", g)
	}
	if g := f.item(rep, "old").Guess; g.Target != -1 || g.Status != "unfinished" || g.Finished != "2016-04-02" {
		t.Errorf("old guess = %+v, want unfinished closed 2016-04-02", g)
	}
	if g := f.item(rep, "recent").Guess; g.Status != "playing" || g.Finished != "" || g.FinishedHint != "2026-11-20" {
		t.Errorf("recent guess = %+v, want open playing with a 2026-11-20 hint", g)
	}
}

// Every achievement provider counts, not just the two the live suggest panel
// asks — and a RetroAchievements subset is its own labelled record.
func TestFindUnloggedActivity_AllProvidersAndSubsets(t *testing.T) {
	f := newActivityFixture(t)
	f.game("p5r", "backlog", []string{`psn_id: "NPWR1_00"`, "xbox_id: 77"}, "")
	f.record("psn", "NPWR1_00", nil, "2026-10-01")
	f.record("xbox", "77", nil, "2026-10-05")

	f.game("layton", "finished", []string{"retroachievements_id: 7601", "retroachievements_subsets:\n  - 25709"},
		"playthroughs:\n  - started: \"2024-01-01\"\n    finished: \"2024-02-01\"\n    status: finished\n")
	f.record("retroachievements", "7601", nil, "2024-01-10")
	f.record("retroachievements", "25709", func(r *model.ArchiveRecord) {
		r.Title = "Professor Layton and the Last Specter [Subset - Mouse Alley]"
	}, "2026-11-02")

	rep := f.find(ActivityOptions{})
	p5r := f.item(rep, "p5r")
	if p5r == nil || len(p5r.Providers) != 2 || p5r.Days != 2 {
		t.Fatalf("want PSN and Xbox activity, got %+v", p5r)
	}
	layton := f.item(rep, "layton")
	if layton == nil || len(layton.Providers) != 1 || layton.Providers[0].Label != "RetroAchievements · Mouse Alley" {
		t.Fatalf("want the subset's activity, labelled, got %+v", layton)
	}
}

func TestFindUnloggedActivity_SinceAndProblems(t *testing.T) {
	f := newActivityFixture(t)
	f.game("a", "backlog", []string{"steam_appid: 1"}, "")
	f.record("steam", "1", func(r *model.ArchiveRecord) {
		r.LastError, r.LastAttempt = "profile is not public", "2026-11-22"
	}, "2026-11-10", "2026-11-21")
	f.game("b", "backlog", []string{"steam_appid: 2"}, "")
	f.record("steam", "2", func(r *model.ArchiveRecord) {
		r.LastError, r.LastAttempt = "profile is not public", "2026-01-01" // before this run: not reported
	}, "2020-01-01")

	since := date("2026-11-15")
	rep := f.find(ActivityOptions{Since: since, ProblemsSince: date("2026-11-22")})
	if rep.GamesPlayed != 1 || rep.Unlocks != 1 {
		t.Errorf("since summary = %d games, %d unlocks; want 1, 1", rep.GamesPlayed, rep.Unlocks)
	}
	if !f.item(rep, "a").IsNew(since) || f.item(rep, "b").IsNew(since) {
		t.Error("a played after the boundary is new; b's 2020 activity is not")
	}
	if len(rep.Problems) != 1 || rep.Problems[0].Error != "profile is not public" || len(rep.Problems[0].Games) != 1 {
		t.Errorf("problems = %+v, want a's failure only", rep.Problems)
	}
}

func TestActivityDay(t *testing.T) {
	cases := map[string]string{
		"":                          "",
		"2026-11-12":                "2026-11-12",
		"2026-11-12T23:30:00-06:00": "2026-11-12",
		// A UTC instant past midnight is still the evening before in Chicago.
		"2026-11-13T03:00:00Z": "2026-11-12",
		"garbage":              "",
	}
	for in, want := range cases {
		if got := activityDay(in); got != want {
			t.Errorf("activityDay(%q) = %q, want %q", in, got, want)
		}
	}
}
