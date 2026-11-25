package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.dalton.dog/gamelog/internal/model"
)

// listsFixture is a games dir with one game whose summary has one earned Steam achievement.
func listsFixture(t *testing.T) *server {
	t.Helper()
	gamesDir := filepath.Join(t.TempDir(), "content", "games")
	dir := filepath.Join(gamesDir, "hollow-knight")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "_index.md"), []byte("---\ntitle: Hollow Knight\nstatus: finished\nsteam_appid: 367520\n---\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "achievement-summary.yaml"), []byte("unlocked: 1\ntotal: 2\nearned:\n"+
		"    - id: \"367520\"\n      key: STEEL_SOUL\n      name: Steel Soul\n      description: Beat Steel Soul\n"+
		"      date: \"2024-08-11T10:00:00-05:00\"\n      provider: steam\n"), 0o644)
	return &server{gamesDir: gamesDir}
}

func post(t *testing.T, h http.HandlerFunc, path string, form url.Values, pathValues map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range pathValues {
		req.SetPathValue(k, v)
	}
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func loadLists(t *testing.T, s *server) *model.GameLists {
	t.Helper()
	gl, err := model.LoadLists(s.gamesDir)
	if err != nil {
		t.Fatal(err)
	}
	return &gl
}

func TestLists_AddGameAndAchievement(t *testing.T) {
	s := listsFixture(t)
	post(t, s.handleCreateList, "/lists", url.Values{"title": {"Favorites"}}, nil)

	// by exact title, from the Lists page
	rec := post(t, s.handleAddToList, "/lists/add", url.Values{"list": {"favorites"}, "game": {"hollow knight"}, "note": {" the best "}}, nil)
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/lists?ok=") || !strings.HasSuffix(loc, "#list-favorites") {
		t.Fatalf("redirect = %q", loc)
	}
	// from the game page: the name is snapshotted from the summary, and back keeps the list selected
	rec = post(t, s.handleAddToList, "/lists/add", url.Values{"list": {"favorites"}, "game": {"hollow-knight"},
		"ach": {"steam|367520|STEEL_SOUL"}, "back": {"/games/hollow-knight"}}, nil)
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "list=favorites") || !strings.HasPrefix(loc, "/games/hollow-knight?") {
		t.Fatalf("redirect = %q", loc)
	}

	items := loadLists(t, s).List("favorites").Items
	if len(items) != 2 {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Game != "hollow-knight" || items[0].Note != "the best" || items[0].IsAchievement() {
		t.Errorf("game item = %+v", items[0])
	}
	if a := items[1]; a.Provider != "steam" || a.ID != "367520" || a.Key != "STEEL_SOUL" || a.Name != "Steel Soul" {
		t.Errorf("achievement item = %+v", a)
	}
}

func TestLists_AddRejectsUnknownGameAndAchievement(t *testing.T) {
	s := listsFixture(t)
	post(t, s.handleCreateList, "/lists", url.Values{"title": {"Favorites"}}, nil)
	for _, form := range []url.Values{
		{"list": {"favorites"}, "game": {"nope"}},
		{"list": {"favorites"}, "game": {"hollow-knight"}, "ach": {"steam|367520|NOPE"}},
		{"list": {"favorites"}, "game": {"hollow-knight"}, "back": {"//evil.example"}, "ach": {"bad"}},
	} {
		rec := post(t, s.handleAddToList, "/lists/add", form, nil)
		if loc := rec.Header().Get("Location"); !strings.Contains(loc, "error=") || strings.HasPrefix(loc, "//") {
			t.Errorf("%v: redirect = %q", form, loc)
		}
	}
	if n := len(loadLists(t, s).List("favorites").Items); n != 0 {
		t.Fatalf("%d items added", n)
	}
}

// an index from a stale page must not act on whatever has moved into that slot
func TestLists_ItemActionsCheckRef(t *testing.T) {
	s := listsFixture(t)
	gl := model.GameLists{}
	gl.CreateList("Favorites", "")
	gl.AddItem("favorites", model.ListItem{Game: "a"})
	gl.AddItem("favorites", model.ListItem{Game: "b"})
	model.SaveLists(s.gamesDir, gl)
	pv := map[string]string{"id": "favorites", "idx": "0"}

	rec := post(t, s.handleRemoveListItem, "/lists/favorites/items/0/delete", url.Values{"ref": {"b|||"}}, pv)
	if !strings.Contains(rec.Header().Get("Location"), "error=") || len(loadLists(t, s).List("favorites").Items) != 2 {
		t.Fatal("stale ref acted on")
	}
	post(t, s.handleMoveListItem, "/lists/favorites/items/0/move", url.Values{"ref": {"a|||"}, "dir": {"down"}}, pv)
	if items := loadLists(t, s).List("favorites").Items; items[0].Game != "b" || items[1].Game != "a" {
		t.Fatalf("move: %+v", items)
	}
	post(t, s.handleMoveListItem, "/lists/favorites/items/1/move", url.Values{"ref": {"a|||"}, "to": {"1"}},
		map[string]string{"id": "favorites", "idx": "1"})
	post(t, s.handleSetListNote, "/lists/favorites/items/0", url.Values{"ref": {"a|||"}, "note": {"hi"}}, pv)
	if items := loadLists(t, s).List("favorites").Items; items[0].Game != "a" || items[0].Note != "hi" {
		t.Fatalf("move-to/note: %+v", items)
	}
}

func TestLists_PagesRender(t *testing.T) {
	s := listsFixture(t)
	gl := model.GameLists{}
	gl.CreateList("Proud", "")
	gl.AddItem("proud", model.ListItem{Game: "hollow-knight", Provider: "steam", ID: "367520", Key: "STEEL_SOUL", Name: "Steel Soul", Note: "pain"})
	gl.AddItem("proud", model.ListItem{Game: "deleted-game"})
	model.SaveLists(s.gamesDir, gl)

	rec := httptest.NewRecorder()
	s.handleLists(rec, httptest.NewRequest(http.MethodGet, "/lists", nil))
	body := rec.Body.String()
	for _, want := range []string{`id="list-proud"`, "Steel Soul", "Beat Steel Soul", `value="pain"`, "no such game", `value="hollow-knight|steam|367520|STEEL_SOUL"`} {
		if !strings.Contains(body, want) {
			t.Errorf("lists page missing %q", want)
		}
	}

	tmpl, err := parsePage(pageSpecs["game_detail"].name, pageSpecs["game_detail"].partials...)
	if err != nil {
		t.Fatal(err)
	}
	doc, pf, err := s.loadGame("hollow-knight")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	req := httptest.NewRequest(http.MethodGet, "/games/hollow-knight?list=proud", nil)
	if err := tmpl.ExecuteTemplate(&buf, "layout", s.buildGameDetail(req, "hollow-knight", doc, pf)); err != nil {
		t.Fatal(err)
	}
	detail := buf.String()
	for _, want := range []string{`value="steam|367520|STEEL_SOUL"`, "★ Proud #1", `<option value="proud" selected>`, "Not on any list."} {
		if !strings.Contains(detail, want) {
			t.Errorf("game page missing %q", want)
		}
	}
}
