package model

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// listsFilename holds the curated lists shown at /games/lists/: authored, never derived, so never sorted.
const listsFilename = "game-lists.yaml"

// GameLists is the whole file.
type GameLists struct {
	Lists []GameList     `yaml:"lists"`
	Extra map[string]any `yaml:",inline"`
}

// GameList is one curated list; ID is a stable slug used as the page anchor and in routes.
type GameList struct {
	ID          string         `yaml:"id"`
	Title       string         `yaml:"title"`
	Description string         `yaml:"description,omitempty"`
	Items       []ListItem     `yaml:"items"`
	Extra       map[string]any `yaml:",inline"`
}

// ListItem is a game, or with Key one of its achievements (by provider IDs, which never change); Name is a snapshot label.
type ListItem struct {
	Game     string         `yaml:"game"`
	Provider string         `yaml:"provider,omitempty"`
	ID       string         `yaml:"id,omitempty"`
	Key      string         `yaml:"key,omitempty"`
	Name     string         `yaml:"name,omitempty"`
	Note     string         `yaml:"note,omitempty"`
	Extra    map[string]any `yaml:",inline"`
}

// IsAchievement reports whether the item names an achievement rather than a game.
func (it ListItem) IsAchievement() bool { return it.Key != "" }

// sameRef reports whether two items point at the same game or achievement.
func (it ListItem) sameRef(o ListItem) bool {
	return it.Game == o.Game && it.Provider == o.Provider && it.ID == o.ID && it.Key == o.Key
}

// ListsPath is <repo>/data/game-lists.yaml, given content/games.
func ListsPath(gamesDir string) string {
	return filepath.Join(filepath.Dir(filepath.Dir(gamesDir)), "data", listsFilename)
}

// LoadLists reads the lists; a missing file is an empty value, not an error.
func LoadLists(gamesDir string) (GameLists, error) {
	var gl GameLists
	path := ListsPath(gamesDir)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return gl, nil
		}
		return gl, err
	}
	if err := yaml.Unmarshal(raw, &gl); err != nil {
		return gl, fmt.Errorf("%s: %w", path, err)
	}
	return gl, nil
}

// SaveLists writes the lists back whole, order untouched.
func SaveLists(gamesDir string, gl GameLists) error {
	for i := range gl.Lists {
		if gl.Lists[i].Items == nil {
			gl.Lists[i].Items = []ListItem{} // `items: []`, not `items: null`, for Hugo's range
		}
	}
	body, err := yaml.Marshal(gl)
	if err != nil {
		return err
	}
	path := ListsPath(gamesDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	header := "# Written by tools/gamelog (the Lists page). Safe to edit by hand, but comments\n" +
		"# are not preserved: the tool rewrites this file whole. Order is rank.\n"
	return writeFileAtomic(path, append([]byte(header), body...), 0o644)
}

// List returns the list with this id, or nil.
func (gl *GameLists) List(id string) *GameList {
	for i := range gl.Lists {
		if gl.Lists[i].ID == id {
			return &gl.Lists[i]
		}
	}
	return nil
}

func (gl *GameLists) mustList(id string) (*GameList, error) {
	l := gl.List(id)
	if l == nil {
		return nil, fmt.Errorf("no list %q", id)
	}
	return l, nil
}

// CreateList appends a new, empty list, its id slugified from the title.
func (gl *GameLists) CreateList(title, description string) (*GameList, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, fmt.Errorf("a list needs a title")
	}
	id := Slugify(title)
	if id == "" {
		return nil, fmt.Errorf("can't make an id from %q", title)
	}
	if id == "add" { // reserved by the web UI's POST /lists/add
		return nil, fmt.Errorf("%q is reserved; pick another title", title)
	}
	if gl.List(id) != nil {
		return nil, fmt.Errorf("a list with id %q already exists", id)
	}
	gl.Lists = append(gl.Lists, GameList{ID: id, Title: title, Description: strings.TrimSpace(description), Items: []ListItem{}})
	return &gl.Lists[len(gl.Lists)-1], nil
}

// UpdateList changes title and description; the id stays so anchors keep working.
func (gl *GameLists) UpdateList(id, title, description string) error {
	l, err := gl.mustList(id)
	if err != nil {
		return err
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("a list needs a title")
	}
	l.Title = title
	l.Description = strings.TrimSpace(description)
	return nil
}

// DeleteList removes a list only once it's empty, so one click can't discard authored notes.
func (gl *GameLists) DeleteList(id string) error {
	l, err := gl.mustList(id)
	if err != nil {
		return err
	}
	if len(l.Items) > 0 {
		return fmt.Errorf("%q still has %d item(s) — remove them first", l.Title, len(l.Items))
	}
	for i := range gl.Lists {
		if gl.Lists[i].ID == id {
			gl.Lists = append(gl.Lists[:i], gl.Lists[i+1:]...)
			break
		}
	}
	return nil
}

// MoveList moves a list to position to (0-based, clamped): its order on the page.
func (gl *GameLists) MoveList(id string, to int) error {
	from := -1
	for i := range gl.Lists {
		if gl.Lists[i].ID == id {
			from = i
		}
	}
	if from < 0 {
		return fmt.Errorf("no list %q", id)
	}
	gl.Lists = moveElem(gl.Lists, from, to)
	return nil
}

// AddItem appends an item; false with no error means it was already on the list.
func (gl *GameLists) AddItem(listID string, it ListItem) (bool, error) {
	l, err := gl.mustList(listID)
	if err != nil {
		return false, err
	}
	it.Game = strings.TrimSpace(it.Game)
	it.Note = strings.TrimSpace(it.Note)
	if it.Game == "" {
		return false, fmt.Errorf("an item needs a game")
	}
	if it.IsAchievement() && (it.Provider == "" || it.ID == "") {
		return false, fmt.Errorf("an achievement item needs its provider and record id")
	}
	for _, e := range l.Items {
		if e.sameRef(it) {
			return false, nil
		}
	}
	l.Items = append(l.Items, it)
	return true, nil
}

func (l *GameList) item(idx int) (*ListItem, error) {
	if idx < 0 || idx >= len(l.Items) {
		return nil, fmt.Errorf("%q has no item #%d", l.Title, idx+1)
	}
	return &l.Items[idx], nil
}

// RemoveItem takes item idx (0-based) off a list.
func (gl *GameLists) RemoveItem(listID string, idx int) error {
	l, err := gl.mustList(listID)
	if err != nil {
		return err
	}
	if _, err := l.item(idx); err != nil {
		return err
	}
	l.Items = append(l.Items[:idx], l.Items[idx+1:]...)
	return nil
}

// MoveItem moves item from to position to (0-based, clamped): up, down and "move to #n".
func (gl *GameLists) MoveItem(listID string, from, to int) error {
	l, err := gl.mustList(listID)
	if err != nil {
		return err
	}
	if _, err := l.item(from); err != nil {
		return err
	}
	l.Items = moveElem(l.Items, from, to)
	return nil
}

// SetNote replaces an item's note; blank clears it.
func (gl *GameLists) SetNote(listID string, idx int, note string) error {
	l, err := gl.mustList(listID)
	if err != nil {
		return err
	}
	it, err := l.item(idx)
	if err != nil {
		return err
	}
	it.Note = strings.TrimSpace(note)
	return nil
}

// ListRank is one list a game or achievement is on, with its 1-based rank there.
type ListRank struct {
	ListID string
	Title  string
	Rank   int
}

// GameRanks is every list this game itself (not one of its achievements) is on.
func (gl GameLists) GameRanks(slug string) []ListRank {
	return gl.ranks(ListItem{Game: slug})
}

// AchievementRanks is every list this achievement is on.
func (gl GameLists) AchievementRanks(slug, provider, id, key string) []ListRank {
	return gl.ranks(ListItem{Game: slug, Provider: provider, ID: id, Key: key})
}

func (gl GameLists) ranks(ref ListItem) []ListRank {
	var out []ListRank
	for _, l := range gl.Lists {
		for i, it := range l.Items {
			if it.sameRef(ref) {
				out = append(out, ListRank{ListID: l.ID, Title: l.Title, Rank: i + 1})
			}
		}
	}
	return out
}

func moveElem[T any](s []T, from, to int) []T {
	if to < 0 {
		to = 0
	}
	if to > len(s)-1 {
		to = len(s) - 1
	}
	if from == to {
		return s
	}
	v := s[from]
	s = append(s[:from], s[from+1:]...)
	s = append(s[:to], append([]T{v}, s[to:]...)...)
	return s
}
