package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func listsTestDir(t *testing.T) string {
	t.Helper()
	gamesDir := filepath.Join(t.TempDir(), "content", "games")
	if err := os.MkdirAll(gamesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return gamesDir
}

func titles(l *GameList) string {
	var s []string
	for _, it := range l.Items {
		s = append(s, it.Game)
	}
	return strings.Join(s, ",")
}

func TestLists_MissingFileIsEmpty(t *testing.T) {
	gl, err := LoadLists(listsTestDir(t))
	if err != nil || len(gl.Lists) != 0 {
		t.Fatalf("got %v, %v", gl, err)
	}
}

func TestLists_CreateRejectsDuplicateAndBlank(t *testing.T) {
	var gl GameLists
	l, err := gl.CreateList("Want to Play Soon", "")
	if err != nil || l.ID != "want-to-play-soon" {
		t.Fatalf("got %+v, %v", l, err)
	}
	if _, err := gl.CreateList("want to play soon", ""); err == nil {
		t.Fatal("duplicate id accepted")
	}
	if _, err := gl.CreateList("  ", ""); err == nil {
		t.Fatal("blank title accepted")
	}
}

func TestLists_AddSkipsDuplicateRef(t *testing.T) {
	var gl GameLists
	gl.CreateList("Favorites", "")
	ach := ListItem{Game: "hollow-knight", Provider: "steam", ID: "367520", Key: "A"}
	for _, it := range []ListItem{{Game: "hollow-knight"}, ach} {
		if added, err := gl.AddItem("favorites", it); !added || err != nil {
			t.Fatalf("add %+v: %v %v", it, added, err)
		}
	}
	// the game and its achievement are different refs; repeats of either are not
	if added, _ := gl.AddItem("favorites", ListItem{Game: "hollow-knight", Note: "again"}); added {
		t.Fatal("duplicate game added")
	}
	if added, _ := gl.AddItem("favorites", ach); added {
		t.Fatal("duplicate achievement added")
	}
	if _, err := gl.AddItem("favorites", ListItem{Game: "x", Key: "A"}); err == nil {
		t.Fatal("achievement without provider/id accepted")
	}
	if _, err := gl.AddItem("nope", ListItem{Game: "x"}); err == nil {
		t.Fatal("unknown list accepted")
	}
}

func TestLists_MoveItemClampsAndBounds(t *testing.T) {
	var gl GameLists
	gl.CreateList("Favorites", "")
	for _, g := range []string{"a", "b", "c", "d"} {
		gl.AddItem("favorites", ListItem{Game: g})
	}
	l := gl.List("favorites")
	steps := []struct {
		from, to int
		want     string
	}{
		{3, 0, "d,a,b,c"},
		{0, 99, "a,b,c,d"},
		{2, -5, "c,a,b,d"},
		{1, 2, "c,b,a,d"},
		{1, 1, "c,b,a,d"},
	}
	for _, s := range steps {
		if err := gl.MoveItem("favorites", s.from, s.to); err != nil {
			t.Fatal(err)
		}
		if got := titles(l); got != s.want {
			t.Fatalf("move %d->%d: got %s, want %s", s.from, s.to, got, s.want)
		}
	}
	if err := gl.MoveItem("favorites", 4, 0); err == nil {
		t.Fatal("out-of-range from accepted")
	}
}

func TestLists_DeleteRefusedWhileNonEmpty(t *testing.T) {
	var gl GameLists
	gl.CreateList("Favorites", "")
	gl.AddItem("favorites", ListItem{Game: "a"})
	if err := gl.DeleteList("favorites"); err == nil {
		t.Fatal("non-empty list deleted")
	}
	if err := gl.RemoveItem("favorites", 0); err != nil {
		t.Fatal(err)
	}
	if err := gl.DeleteList("favorites"); err != nil || len(gl.Lists) != 0 {
		t.Fatalf("empty list not deleted: %v", err)
	}
}

func TestLists_Ranks(t *testing.T) {
	var gl GameLists
	gl.CreateList("Favorites", "")
	gl.CreateList("Proud", "")
	gl.AddItem("favorites", ListItem{Game: "a"})
	gl.AddItem("favorites", ListItem{Game: "b"})
	gl.AddItem("proud", ListItem{Game: "b", Provider: "steam", ID: "1", Key: "K"})
	if r := gl.GameRanks("b"); len(r) != 1 || r[0].ListID != "favorites" || r[0].Rank != 2 {
		t.Fatalf("game ranks: %+v", r)
	}
	if r := gl.AchievementRanks("b", "steam", "1", "K"); len(r) != 1 || r[0].ListID != "proud" || r[0].Rank != 1 {
		t.Fatalf("achievement ranks: %+v", r)
	}
}

// a hand-added key at any level must survive the tool's whole-file rewrite
func TestLists_ExtraRoundTrips(t *testing.T) {
	gamesDir := listsTestDir(t)
	path := ListsPath(gamesDir)
	os.MkdirAll(filepath.Dir(path), 0o755)
	in := "updated: yesterday\nlists:\n  - id: favorites\n    title: Favorites\n    cover: x.png\n" +
		"    items:\n      - game: a\n        tier: S\n"
	if err := os.WriteFile(path, []byte(in), 0o644); err != nil {
		t.Fatal(err)
	}
	gl, err := LoadLists(gamesDir)
	if err != nil {
		t.Fatal(err)
	}
	gl.SetNote("favorites", 0, "best")
	if err := SaveLists(gamesDir, gl); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(path)
	for _, want := range []string{"updated: yesterday", "cover: x.png", "tier: S", "note: best"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("rewrite lost %q:\n%s", want, out)
		}
	}
}

func TestLists_EmptyListSavesItemsAsEmptySeq(t *testing.T) {
	gamesDir := listsTestDir(t)
	if err := SaveLists(gamesDir, GameLists{Lists: []GameList{{ID: "x", Title: "X"}}}); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(ListsPath(gamesDir))
	if !strings.Contains(string(out), "items: []") {
		t.Fatalf("got:\n%s", out)
	}
}
