package model

import (
	"slices"
	"testing"
)

func TestParseTags(t *testing.T) {
	cases := map[string][]string{
		"":                          nil,
		" , \n ":                    nil,
		"multiplayer":               {"multiplayer"},
		"Multiplayer, co-op\ncouch": {"multiplayer", "co-op", "couch"},
		"a, A , b, a":               {"a", "b"},
	}
	for in, want := range cases {
		if got := ParseTags(in); !slices.Equal(got, want) || (want == nil && got != nil) {
			t.Errorf("ParseTags(%q) = %#v, want %#v", in, got, want)
		}
	}
}

func TestShrunkListPaths(t *testing.T) {
	if got := ShrunkListPaths("tags", 3, 1); !slices.Equal(got, []string{"tags[1]", "tags[2]"}) {
		t.Errorf("got %v", got)
	}
	if got := ShrunkListPaths("tags", 1, 2); got != nil {
		t.Errorf("a growing list removes nothing, got %v", got)
	}
}

// A run's tags carry into a split-off entry, and the game summary reports
// the tags only some runs carry.
func TestTags_SplitAndRunOnlyTags(t *testing.T) {
	f := &PlaythroughsFile{Playthroughs: []PlaythroughEntry{
		{Status: "finished", Sessions: []SessionEntry{{Started: "2020-01-01", Finished: "2020-01-02"}}},
		{Status: "endless", Tags: []string{"multiplayer"}, Sessions: []SessionEntry{
			{Started: "2021-01-01", Finished: "2021-01-02"}, {Started: "2022-01-01", Finished: "2022-01-02"},
		}},
	}}
	idx, err := f.SplitPlaythrough(1, 1, "endless")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.Playthroughs[idx].Tags, []string{"multiplayer"}) {
		t.Errorf("split-off entry tags = %v", f.Playthroughs[idx].Tags)
	}
	if got := runOnlyTags([]string{"co-op"}, f.Views()); !slices.Equal(got, []string{"multiplayer"}) {
		t.Errorf("runOnlyTags = %v", got)
	}
	if got := runOnlyTags([]string{"multiplayer"}, f.Views()); got != nil {
		t.Errorf("a tag the game already has isn't run-only, got %v", got)
	}
}
