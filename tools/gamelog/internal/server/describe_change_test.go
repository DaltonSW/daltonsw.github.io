package server

import (
	"strings"
	"testing"

	"go.dalton.dog/gamelog/internal/model"
)

func fl(v float64) *float64 { return &v }

func withStats(stats ...model.ArchivedStat) *model.ArchiveRecord {
	r := &model.ArchiveRecord{}
	r.Stats = stats
	return r
}

func rec(unlocked int, names ...string) *model.ArchiveRecord {
	r := &model.ArchiveRecord{}
	r.Total = 10
	for i, n := range names {
		r.Achievements = append(r.Achievements, model.ArchivedAchievement{Key: n, Name: n + " name", Unlocked: i < unlocked})
	}
	r.Unlocked = unlocked
	return r
}

func TestDescribeChange_NamesWhatUnlockedAndWhatElseMoved(t *testing.T) {
	before := rec(1, "a", "b", "c", "d", "e")
	before.PlaytimeMins = 60
	before.Stats = []model.ArchivedStat{{Key: "crops", Value: fl(5)}, {Key: "fish", Value: fl(2)}}
	after := rec(5, "a", "b", "c", "d", "e")
	after.PlaytimeMins = 150
	after.LastPlayed = "2026-11-25"
	after.Stats = []model.ArchivedStat{{Key: "crops", Value: fl(9)}, {Key: "fish", Value: fl(2)}, {Key: "new", Value: fl(1)}}

	got := describeChange("Steam", before, after)
	for _, want := range []string{
		"+4 (5/10): b name, c name, d name, and 1 more",
		"+1.5h played (2.5h total)",
		"last played 2026-11-25",
		"2 stats changed",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

func TestDescribeChange_NothingMovedIsBlank(t *testing.T) {
	r := rec(1, "a")
	r.Stats = []model.ArchivedStat{{Key: "k", Value: fl(3)}}
	if got := describeChange("Steam", r, r); got != "" {
		t.Errorf("got %q, want nothing for an unchanged record", got)
	}
}

// A stats-only game moving is worth a line even though nothing unlocked.
func TestDescribeChange_StatsAloneAreReported(t *testing.T) {
	before := withStats(model.ArchivedStat{Key: "k", Value: fl(1)})
	after := withStats(model.ArchivedStat{Key: "k", Value: fl(2)})
	if got := describeChange("Steam", before, after); got != "Steam: 1 stat changed" {
		t.Errorf("got %q", got)
	}
}

func TestDescribeChange_FirstCaptureCountsStats(t *testing.T) {
	after := withStats(model.ArchivedStat{Key: "a", Value: fl(1)}, model.ArchivedStat{Key: "b"})
	if got := describeChange("Steam", nil, after); got != "Steam: first capture, no achievements, 1 stat" {
		t.Errorf("got %q", got)
	}
}

func TestDescribeUnmoved_SaysWhyNothingChanged(t *testing.T) {
	ok := rec(1, "a")
	failing := &model.ArchiveRecord{}
	failing.LastError = "steam: appid 1: unexpected status 500 Internal Server Error"

	cases := []struct {
		name    string
		skipped bool
		rec     *model.ArchiveRecord
		want    string
	}{
		{"skipped", true, ok, "Steam: skipped, playtime and last played unchanged"},
		{"checked", false, ok, "Steam: checked, nothing new (1/10 unlocked)"},
		{"failing", false, failing, "Steam: still failing, steam: appid 1: unexpected status 500 Internal Server Error (existing data kept)"},
		{"nil", false, nil, ""},
	}
	for _, c := range cases {
		if got := describeUnmoved("Steam", c.skipped, c.rec); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
