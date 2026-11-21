package model

import "testing"

func run(status, started, finished string) Playthrough {
	return Playthrough{Status: status, Started: started, Finished: finished}
}

func TestDeriveGameStatus(t *testing.T) {
	cases := []struct {
		name    string
		current string
		views   []Playthrough
		want    string
		derived bool
	}{
		{"no runs stays hand-set", "dropped", nil, "", false},
		{"one-shot games stay hand-set", "endless",
			[]Playthrough{run("finished", "2020-10-23", "2022-12-20")}, "", false},
		{"only side modes and misc launches stay hand-set", "finished",
			[]Playthrough{run("multiplayer", "2014-06-05", "2014-06-10"), run("misc_launch", "2015-01-01", "2015-01-01")}, "", false},
		{"an open replay of a finished game reads playing", "finished",
			[]Playthrough{run("finished", "2023-10-15", "2024-02-11"), run("playing", "2026-08-29", "")}, "playing", true},
		{"an open paused run reads paused", "finished",
			[]Playthrough{run("finished", "2018-09-03", "2021-11-22"), run("paused", "2024-11-24", "")}, "paused", true},
		{"a later dropped run doesn't un-finish a game", "dropped",
			[]Playthrough{run("finished", "2019-10-16", "2019-10-18"), run("dropped", "2019-10-18", "2019-10-19")}, "finished", true},
		{"a later co-op run doesn't either", "finished",
			[]Playthrough{run("finished", "2012-11-17", "2012-12-31"), run("multiplayer", "2016-07-09", "2016-07-13")}, "finished", true},
		{"never finished: the run that ended last", "playing",
			[]Playthrough{run("dropped", "2020-01-01", "2020-02-01"), run("unfinished", "2021-01-01", "2021-03-01")}, "unfinished", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := DeriveGameStatus(tc.current, tc.views)
			if got != tc.want || ok != tc.derived {
				t.Errorf("DeriveGameStatus = (%q, %v), want (%q, %v)", got, ok, tc.want, tc.derived)
			}
		})
	}
}

// Stale triage's verdict lands on the most recently active open run; a
// closed run, and an earlier closed session, are never touched.
func TestCloseLatestOpenRun(t *testing.T) {
	pf := loadFixture(t, `playthroughs:
  - status: finished
    started: "2023-01-01"
    finished: "2023-02-01"
  - status: playing
    sessions:
      - started: "2024-01-01"
        finished: "2024-01-05"
      - started: "2024-06-01"
        finished: ""
`)
	if !pf.CloseLatestOpenRun("dropped", "2024-06-03") {
		t.Fatal("expected the open run to be closed")
	}
	if e := pf.Playthroughs[0]; e.Status != "finished" || e.Finished != "2023-02-01" {
		t.Errorf("a closed run must not be touched: %+v", e)
	}
	e := pf.Playthroughs[1]
	if e.Status != "dropped" || e.Sessions[1].Finished != "2024-06-03" || e.Sessions[0].Finished != "2024-01-05" {
		t.Errorf("open run = %+v", e)
	}
}

func TestCloseLatestOpenRun_PausedLeavesItOpen(t *testing.T) {
	pf := loadFixture(t, "playthroughs:\n  - status: playing\n    started: \"2024-01-01\"\n")
	if !pf.CloseLatestOpenRun("paused", "") {
		t.Fatal("expected a change")
	}
	if e := pf.Playthroughs[0]; e.Status != "paused" || e.Finished != "" {
		t.Errorf("pausing must relabel without closing: %+v", e)
	}
}

func TestCloseLatestOpenRun_NothingOpen(t *testing.T) {
	pf := loadFixture(t, "playthroughs:\n  - status: finished\n    started: \"2024-01-01\"\n    finished: \"2024-01-02\"\n")
	if pf.CloseLatestOpenRun("dropped", "2024-06-03") {
		t.Error("no open run means nothing to close")
	}
}
