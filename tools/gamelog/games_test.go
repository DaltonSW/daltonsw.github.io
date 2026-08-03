package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSlugify(t *testing.T) {
	cases := []struct {
		title, want, why string
	}{
		// Accents fold to their base letter instead of being dropped.
		{"Ōkami", "okami", "leading accented letter used to vanish entirely"},
		{"Pokémon Red", "pokemon-red", "accented letter used to split the slug"},
		{"NieR:Automata", "nier-automata", ""},
		{"Ys VIII: Lacrimosa of DANA", "ys-viii-lacrimosa-of-dana", ""},
		// Letters with no decomposed form need spelling out.
		{"Papers, Please: Straße", "papers-please-strasse", ""},
		{"Æon Flux", "aeon-flux", ""},
		// Symbols are not letters and still fall away. Every non-ASCII
		// character in the real backfill corpus is one of these two, so
		// these cases pin the slugs that library already produces.
		{"ELDEN RING®", "elden-ring", ""},
		{"Crash Bandicoot™ N. Sane Trilogy", "crash-bandicoot-n-sane-trilogy", ""},
		{"DARK SOULS™: REMASTERED", "dark-souls-remastered", ""},
		{"LEGO® Harry Potter: Years 1-4", "lego-harry-potter-years-1-4", ""},
		{"STAR WARS Jedi: Fallen Order™ ", "star-wars-jedi-fallen-order", "trailing space"},
		// Ordinary punctuation.
		{"Doc Louis's Punch-Out!!", "doc-louis-s-punch-out", ""},
		{"~Hack~ Super Mario Eclipse", "hack-super-mario-eclipse", ""},
		{"Sid Meier's Civilization® VI", "sid-meier-s-civilization-vi", ""},
		// Nothing sluggable at all.
		{"!@#$%", "", "must be caught by validateSlug, not written to disk"},
	}
	for _, tc := range cases {
		if got := Slugify(tc.title); got != tc.want {
			t.Errorf("Slugify(%q) = %q, want %q %s", tc.title, got, tc.want, tc.why)
		}
	}
}

// "Hades: II" and "Hades II" both slugify to hades-ii. No slug scheme
// separates them, so the collision has to be caught at write time.
func TestSlugifyCollisionIsReported(t *testing.T) {
	if Slugify("Hades: II") != Slugify("Hades II") {
		t.Skip("titles no longer collide; this test is about the collision path")
	}
	dir := t.TempDir()
	if _, err := CreateGameFile(dir, "hades-ii", NewGameFields{Title: "Hades II", Status: "playing"}); err != nil {
		t.Fatalf("first create should succeed: %v", err)
	}
	_, err := CreateGameFile(dir, "hades-ii", NewGameFields{Title: "Hades: II", Status: "playing"})
	if err == nil {
		t.Fatal("expected the second create to be refused")
	}
	if !strings.Contains(err.Error(), "Hades II") {
		t.Fatalf("error should name the game already holding the slug, got: %v", err)
	}
}

// An empty slug collapses filepath.Join(gamesDir, "", "_index.md") onto
// content/games/_index.md — the games section index page. In the real repo
// that was blocked only because the file happened to already exist.
func TestCreateGameFileRejectsUnsafeSlugs(t *testing.T) {
	for _, slug := range []string{"", ".", "..", "../escape", "nested/slug", `back\slash`} {
		dir := t.TempDir()
		sectionIndex := filepath.Join(dir, "_index.md")
		if _, err := CreateGameFile(dir, slug, NewGameFields{Title: "X", Status: "playing"}); err == nil {
			t.Errorf("CreateGameFile(%q) was allowed", slug)
		}
		if _, err := os.Stat(sectionIndex); err == nil {
			t.Errorf("CreateGameFile(%q) wrote the section index at %s", slug, sectionIndex)
		}
	}
}

func TestCreateGameFileWritesTheGameDirectory(t *testing.T) {
	dir := t.TempDir()
	path, err := CreateGameFile(dir, "okami", NewGameFields{Title: "Ōkami", Status: "playing"})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "okami", "_index.md"); path != want {
		t.Fatalf("wrote %s, want %s", path, want)
	}
	doc, err := LoadDoc(path)
	if err != nil {
		t.Fatalf("generated file is not valid: %v", err)
	}
	if got := doc.FM.Title; got != "Ōkami" {
		t.Fatalf("title should keep its accents, got %q", got)
	}

	// Playthroughs are their own file now, and a brand-new game has none —
	// front matter must not carry a `playthroughs:` key at all.
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "playthroughs") {
		t.Error("front matter should no longer mention playthroughs")
	}
	if _, err := os.Stat(PlaythroughsPath(filepath.Dir(path))); !os.IsNotExist(err) {
		t.Error("a game with no playthroughs should have no playthroughs.yaml")
	}
}

// Front matter is written bare (`steam_appid: 1145360`), so it decodes as an
// int; every reader wants the text back.
func TestLoadDocReadsBareExternalIDs(t *testing.T) {
	dir := t.TempDir()
	path, err := CreateGameFile(dir, "hades", NewGameFields{
		Title: "Hades", Status: "playing", SteamAppID: "1145360", RetroAchievementsID: "4650",
	})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := LoadDoc(path)
	if err != nil {
		t.Fatal(err)
	}
	raID, steamAppID := doc.ExternalIDs()
	if raID != "4650" || steamAppID != "1145360" {
		t.Fatalf("ExternalIDs = %q/%q, want 4650/1145360", raID, steamAppID)
	}
}

// Draft drives the `gamelog review` backlog filter, so ListGames has to
// surface it, not just leave it readable via LoadDoc.
func TestListGamesPopulatesDraft(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateGameFile(dir, "draft-game", NewGameFields{Title: "Draft Game", Status: "playing", Draft: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateGameFile(dir, "published-game", NewGameFields{Title: "Published Game", Status: "playing", Draft: false}); err != nil {
		t.Fatal(err)
	}

	games, err := ListGames(dir)
	if err != nil {
		t.Fatal(err)
	}
	byTitle := map[string]bool{}
	for _, g := range games {
		byTitle[g.Title] = g.Draft
	}
	if !byTitle["Draft Game"] {
		t.Error("Draft Game should have Draft == true")
	}
	if byTitle["Published Game"] {
		t.Error("Published Game should have Draft == false")
	}
}
