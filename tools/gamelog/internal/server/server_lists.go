package server

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"go.dalton.dog/gamelog/internal/model"
)

// ── Curated lists (data/game-lists.yaml, rendered at /games/lists/) ───────

const listsPath = "/lists"

type listsData struct {
	Page
	Lists []listView
	Games []model.GameSummary // the add-game <datalist>
}

type listView struct {
	ID, Title, Description string
	Pos                    int // 1-based position among lists
	Items                  []listItemView
}

type listItemView struct {
	Idx       int // 0-based; what the item routes address
	Rank      int
	Item      model.ListItem
	Ref       string // guards item routes against a stale page; see itemRef
	GameTitle string
	Desc      string // achievement description, when it resolves
	Missing   string // why the reference no longer resolves; never pruned automatically
}

// itemRef identifies an item's target, posted back with every item action
// so an index from an out-of-date tab can't edit the wrong entry.
func itemRef(it model.ListItem) string {
	return strings.Join([]string{it.Game, it.Provider, it.ID, it.Key}, "|")
}

// editLists is the one write path: load, apply, save, under a lock.
func (s *server) editLists(fn func(*model.GameLists) error) error {
	s.listsMu.Lock()
	defer s.listsMu.Unlock()
	gl, err := model.LoadLists(s.gamesDir)
	if err != nil {
		return err
	}
	if err := fn(&gl); err != nil {
		return err
	}
	return model.SaveLists(s.gamesDir, gl)
}

// summaryCache loads each game's achievement summary at most once per request.
type summaryCache map[string]*model.AchievementSummary

func (c summaryCache) get(gamesDir, slug string) *model.AchievementSummary {
	if sum, ok := c[slug]; ok {
		return sum
	}
	sum, _ := model.LoadAchievementSummary(filepath.Join(gamesDir, slug))
	c[slug] = sum
	return sum
}

func findEarned(sum *model.AchievementSummary, provider, id, key string) *model.EarnedAchievement {
	if sum == nil {
		return nil
	}
	for i, a := range sum.Earned {
		if a.Provider == provider && a.ID == id && a.Key == key {
			return &sum.Earned[i]
		}
	}
	return nil
}

func (s *server) handleLists(w http.ResponseWriter, r *http.Request) {
	gl, err := model.LoadLists(s.gamesDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	games, err := model.ListGames(s.gamesDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, "lists", buildListsData(r, s.gamesDir, gl, games))
}

func buildListsData(r *http.Request, gamesDir string, gl model.GameLists, games []model.GameSummary) listsData {
	titles := make(map[string]string, len(games))
	for _, g := range games {
		titles[g.Slug] = g.Title
	}
	sums := summaryCache{}
	data := listsData{Page: newPage(r, "Lists", "lists"), Games: games}
	for li, l := range gl.Lists {
		lv := listView{ID: l.ID, Title: l.Title, Description: l.Description, Pos: li + 1}
		for i, it := range l.Items {
			v := listItemView{Idx: i, Rank: i + 1, Item: it, Ref: itemRef(it), GameTitle: titles[it.Game]}
			switch {
			case v.GameTitle == "":
				v.GameTitle, v.Missing = it.Game, "no such game"
			case it.IsAchievement():
				if a := findEarned(sums.get(gamesDir, it.Game), it.Provider, it.ID, it.Key); a != nil {
					v.Desc = a.Description
				} else {
					v.Missing = "achievement not in the game's summary"
				}
			}
			lv.Items = append(lv.Items, v)
		}
		data.Lists = append(data.Lists, lv)
	}
	return data
}

// backPath is where a list form returns to: its own `back` field when that's
// a local path, otherwise the Lists page.
func backPath(r *http.Request, fallback string) string {
	if b := r.FormValue("back"); strings.HasPrefix(b, "/") && !strings.HasPrefix(b, "//") {
		return b
	}
	return fallback
}

func listAnchor(id string) string { return listsPath + "#list-" + id }

func (s *server) handleCreateList(w http.ResponseWriter, r *http.Request) {
	var id string
	err := s.editLists(func(gl *model.GameLists) error {
		l, err := gl.CreateList(r.FormValue("title"), r.FormValue("description"))
		if err == nil {
			id = l.ID
		}
		return err
	})
	if err != nil {
		redirectErr(w, r, listsPath, err)
		return
	}
	redirectOK(w, r, listAnchor(id), "List created.")
}

func (s *server) handleEditList(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := s.editLists(func(gl *model.GameLists) error {
		return gl.UpdateList(id, r.FormValue("title"), r.FormValue("description"))
	})
	if err != nil {
		redirectErr(w, r, listAnchor(id), err)
		return
	}
	redirectOK(w, r, listAnchor(id), "List saved.")
}

func (s *server) handleDeleteList(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.editLists(func(gl *model.GameLists) error { return gl.DeleteList(id) }); err != nil {
		redirectErr(w, r, listAnchor(id), err)
		return
	}
	redirectOK(w, r, listsPath, "List deleted.")
}

// moveTarget turns a posted dir=up|down or to=<1-based rank> into a 0-based position.
func moveTarget(r *http.Request, from int) (int, error) {
	switch r.FormValue("dir") {
	case "up":
		return from - 1, nil
	case "down":
		return from + 1, nil
	}
	to, err := strconv.Atoi(strings.TrimSpace(r.FormValue("to")))
	if err != nil {
		return 0, fmt.Errorf("move to: enter a position number")
	}
	return to - 1, nil
}

func (s *server) handleMoveList(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := s.editLists(func(gl *model.GameLists) error {
		from := -1
		for i := range gl.Lists {
			if gl.Lists[i].ID == id {
				from = i
			}
		}
		if from < 0 {
			return fmt.Errorf("no list %q", id)
		}
		to, err := moveTarget(r, from)
		if err != nil {
			return err
		}
		return gl.MoveList(id, to)
	})
	if err != nil {
		redirectErr(w, r, listAnchor(id), err)
		return
	}
	http.Redirect(w, r, listAnchor(id), http.StatusSeeOther)
}

// handleAddToList adds a game, or with ach=provider|id|key one of its
// achievements, to the list named by the `list` field — one route so a
// single list selector can serve every "+" button on a game's page.
func (s *server) handleAddToList(w http.ResponseWriter, r *http.Request) {
	listID := r.FormValue("list")
	back := listAnchor(listID)
	if b := backPath(r, ""); b != "" {
		back = withFlash(b, "list", listID) // keeps the page's list selector on the same list
	}
	item, err := s.resolveNewItem(r.FormValue("game"), r.FormValue("ach"))
	if err != nil {
		redirectErr(w, r, back, err)
		return
	}
	item.Note = r.FormValue("note")
	var added bool
	var title string
	err = s.editLists(func(gl *model.GameLists) error {
		var err error
		added, err = gl.AddItem(listID, item)
		if l := gl.List(listID); l != nil {
			title = l.Title
		}
		return err
	})
	if err != nil {
		redirectErr(w, r, back, err)
		return
	}
	what := item.Game
	if item.IsAchievement() {
		what = item.Name
	}
	if !added {
		redirectOK(w, r, back, fmt.Sprintf("%s is already on %s.", what, title))
		return
	}
	redirectOK(w, r, back, fmt.Sprintf("Added %s to %s.", what, title))
}

// resolveNewItem checks the game exists (accepting a slug or an exact title)
// and, for an achievement, that it's in the game's summary, snapshotting its name.
func (s *server) resolveNewItem(game, ach string) (model.ListItem, error) {
	game = strings.TrimSpace(game)
	if game == "" {
		return model.ListItem{}, fmt.Errorf("pick a game")
	}
	games, err := model.ListGames(s.gamesDir)
	if err != nil {
		return model.ListItem{}, err
	}
	slug := ""
	for _, g := range games {
		if g.Slug == game || strings.EqualFold(g.Title, game) {
			slug = g.Slug
			break
		}
	}
	if slug == "" {
		return model.ListItem{}, fmt.Errorf("no game %q", game)
	}
	item := model.ListItem{Game: slug}
	if ach == "" {
		return item, nil
	}
	parts := strings.SplitN(ach, "|", 3) // key last, so a "|" inside it survives
	if len(parts) != 3 {
		return model.ListItem{}, fmt.Errorf("malformed achievement reference %q", ach)
	}
	sum, err := model.LoadAchievementSummary(filepath.Join(s.gamesDir, slug))
	if err != nil {
		return model.ListItem{}, err
	}
	a := findEarned(sum, parts[0], parts[1], parts[2])
	if a == nil {
		return model.ListItem{}, fmt.Errorf("no earned achievement %q on %s", ach, slug)
	}
	item.Provider, item.ID, item.Key, item.Name = a.Provider, a.ID, a.Key, a.Name
	return item, nil
}

// itemAction runs fn on the item the route names, after checking the posted
// ref still matches what's at that index.
func (s *server) itemAction(w http.ResponseWriter, r *http.Request, okMsg string, fn func(gl *model.GameLists, id string, idx int) error) {
	id := r.PathValue("id")
	idx, err := strconv.Atoi(r.PathValue("idx"))
	if err != nil {
		http.Error(w, "bad item index", http.StatusBadRequest)
		return
	}
	err = s.editLists(func(gl *model.GameLists) error {
		l := gl.List(id)
		if l == nil {
			return fmt.Errorf("no list %q", id)
		}
		if idx < 0 || idx >= len(l.Items) || itemRef(l.Items[idx]) != r.FormValue("ref") {
			return fmt.Errorf("that list changed since the page loaded — nothing was changed; try again")
		}
		return fn(gl, id, idx)
	})
	if err != nil {
		redirectErr(w, r, listAnchor(id), err)
		return
	}
	if okMsg == "" {
		http.Redirect(w, r, listAnchor(id), http.StatusSeeOther)
		return
	}
	redirectOK(w, r, listAnchor(id), okMsg)
}

func (s *server) handleSetListNote(w http.ResponseWriter, r *http.Request) {
	s.itemAction(w, r, "Note saved.", func(gl *model.GameLists, id string, idx int) error {
		return gl.SetNote(id, idx, r.FormValue("note"))
	})
}

func (s *server) handleMoveListItem(w http.ResponseWriter, r *http.Request) {
	s.itemAction(w, r, "", func(gl *model.GameLists, id string, idx int) error {
		to, err := moveTarget(r, idx)
		if err != nil {
			return err
		}
		return gl.MoveItem(id, idx, to)
	})
}

func (s *server) handleRemoveListItem(w http.ResponseWriter, r *http.Request) {
	s.itemAction(w, r, "Removed.", func(gl *model.GameLists, id string, idx int) error {
		return gl.RemoveItem(id, idx)
	})
}
