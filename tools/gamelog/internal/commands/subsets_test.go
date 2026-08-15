package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.dalton.dog/gamelog/internal/model"
	"go.dalton.dog/gamelog/internal/providers/retroachievements"
)

// fakeRA answers the one per-game call this file needs, so the parent-link
// rules can be tested without a key — and, more to the point, so the *refusal*
// cases can be tested at all, which a live server would never produce on
// demand.
type fakeRA struct {
	byID  map[string]retroachievements.RAProgress
	calls int
}

func (f *fakeRA) GetGameProgress(ctx context.Context, gameID string) (retroachievements.RAProgress, error) {
	f.calls++
	p, ok := f.byID[gameID]
	if !ok {
		return retroachievements.RAProgress{}, fmt.Errorf("retroachievements: no game with ID %s", gameID)
	}
	return p, nil
}

func subsetProgress(parent int) retroachievements.RAProgress {
	return retroachievements.RAProgress{
		Title:        "Professor Layton and the Last Specter [Subset - Mouse Alley]",
		ConsoleName:  "Nintendo DS",
		ParentGameID: parent,
	}
}

// resetSubsetCache clears the process-wide parent memo between tests — it
// exists so the housekeeping page doesn't re-ask RA on every render, which
// would otherwise leak one test's answers into the next.
func resetSubsetCache(t *testing.T) {
	t.Helper()
	raParentCache.Lock()
	raParentCache.byID = map[string]string{}
	raParentCache.Unlock()
}

func TestResolveSubsets_LinksToTheLoggedBaseGame(t *testing.T) {
	resetSubsetCache(t)
	index := NewLoggedIndex([]model.GameSummary{
		{Slug: "professor-layton-and-the-last-specter", Title: "Professor Layton and the Last Specter", RAGameID: "7601"},
	})
	ra := &fakeRA{byID: map[string]retroachievements.RAProgress{"25709": subsetProgress(7601)}}

	got := ResolveSubsets(context.Background(), ra, []Candidate{{
		Provider: "RetroAchievements", ID: "25709",
		Title:  "Professor Layton and the Last Specter [Subset - Mouse Alley]",
		Subset: &SubsetInfo{Name: "Mouse Alley", BaseTitle: "Professor Layton and the Last Specter"},
	}}, index)

	sub := got[0].Subset
	if sub.ParentID != "7601" {
		t.Errorf("ParentID = %q, want 7601 — from ParentGameID, not from the title", sub.ParentID)
	}
	if !sub.Attachable() || sub.BaseSlug != "professor-layton-and-the-last-specter" {
		t.Errorf("subset = %+v, want it attachable to the logged base game", sub)
	}

	// The memo is what keeps the housekeeping page from re-asking RA (~1.2s a
	// call) every time it renders.
	ResolveSubsets(context.Background(), ra, got, index)
	if ra.calls != 1 {
		t.Errorf("GetGameProgress called %d times, want 1 — the parent link is memoized", ra.calls)
	}
}

func TestResolveSubsets_RecordsAFailureInsteadOfGuessing(t *testing.T) {
	resetSubsetCache(t)
	// Titled like a subset, but RA reports no parent. The title convention is
	// never enough on its own, so this row falls back to an ordinary one.
	ra := &fakeRA{byID: map[string]retroachievements.RAProgress{"25709": subsetProgress(0)}}

	got := ResolveSubsets(context.Background(), ra, []Candidate{{
		Provider: "RetroAchievements", ID: "25709", Title: "X [Subset - Y]",
		Subset: &SubsetInfo{Name: "Y", BaseTitle: "X"},
	}}, NewLoggedIndex(nil))

	sub := got[0].Subset
	if sub.ParentID != "" || sub.Attachable() {
		t.Errorf("subset = %+v, want no parent and not attachable", sub)
	}
	if sub.Err == "" {
		t.Error("want the reason recorded on the row rather than the scan failing")
	}
}

// An unrelated candidate must come back untouched — subsets are rare, and the
// resolution pass runs over every RA row.
func TestResolveSubsets_LeavesOrdinaryCandidatesAlone(t *testing.T) {
	resetSubsetCache(t)
	ra := &fakeRA{}
	got := ResolveSubsets(context.Background(), ra, []Candidate{
		{Provider: "RetroAchievements", ID: "10210", Title: "Banjo-Kazooie"},
	}, NewLoggedIndex(nil))
	if got[0].Subset != nil || ra.calls != 0 {
		t.Errorf("ordinary candidate touched: subset=%+v, calls=%d", got[0].Subset, ra.calls)
	}
}

// Once a subset is attached, the scan must stop offering it as a game of its
// own — otherwise attaching it changes nothing about the list it came from.
func TestLoggedIndex_SkipsAttachedSubsets(t *testing.T) {
	idx := NewLoggedIndex([]model.GameSummary{{
		Slug: "professor-layton-and-the-last-specter", Title: "Professor Layton and the Last Specter",
		RAGameID: "7601", RASubsets: []string{"25709"},
	}})
	if !idx.hasRA("25709", "Professor Layton and the Last Specter [Subset - Mouse Alley]") {
		t.Error("an attached subset should be filtered out of the scan")
	}
	if _, ok := idx.raGames["25709"]; ok {
		t.Error("a subset must not be a parent candidate itself")
	}
	if g := idx.raGames["7601"]; g.Slug != "professor-layton-and-the-last-specter" {
		t.Errorf("base game not indexed by its RA id, got %+v", g)
	}
}

// gameFixture writes a minimal content/games tree and returns the games dir.
func gameFixture(t *testing.T, slug, frontMatter string) string {
	t.Helper()
	gamesDir := filepath.Join(t.TempDir(), "content", "games")
	dir := filepath.Join(gamesDir, slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "_index.md"), []byte(frontMatter), 0o644); err != nil {
		t.Fatal(err)
	}
	return gamesDir
}

const laytonIndexMD = `---
title: "Professor Layton and the Last Specter"
platform: "Nintendo DS"
retroachievements_id: 7601
steam_appid:
status: "playing"
started: ""
finished: ""
rating:
draft: false
---

Prose that must survive the write.
`

// The subset id arrives from a submitted form, and attaching one to the wrong
// game would put another game's achievements on this game's page — a wrong
// answer that looks entirely plausible on screen. So the parent is re-checked
// against RA before anything is written.
func TestAttachSubset_RefusesAMismatchedParent(t *testing.T) {
	resetSubsetCache(t)
	gamesDir := gameFixture(t, "professor-layton-and-the-last-specter", laytonIndexMD)
	ra := &fakeRA{byID: map[string]retroachievements.RAProgress{"999": subsetProgress(1234)}}

	_, err := AttachSubset(context.Background(), gamesDir, "professor-layton-and-the-last-specter", "999", ra, Credentials{})
	if err == nil {
		t.Fatal("expected a refusal for a subset belonging to another game")
	}
	if !strings.Contains(err.Error(), "refusing to attach") {
		t.Errorf("unhelpful error: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(gamesDir, "professor-layton-and-the-last-specter", "_index.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "retroachievements_subsets") {
		t.Errorf("nothing should have been written:\n%s", raw)
	}
}

func TestAttachSubset_WritesTheLinkBeforeCapturingHistory(t *testing.T) {
	resetSubsetCache(t)
	gamesDir := gameFixture(t, "professor-layton-and-the-last-specter", laytonIndexMD)
	ra := &fakeRA{byID: map[string]retroachievements.RAProgress{"25709": subsetProgress(7601)}}

	// No credentials, so the capture step can't run. The link must still land:
	// it's what puts the subset in ProviderLinks, so a later `gamelog
	// achievements <slug>` picks the history up on its own.
	_, err := AttachSubset(context.Background(), gamesDir, "professor-layton-and-the-last-specter", "25709", ra, Credentials{})
	if err == nil || !strings.Contains(err.Error(), "no achievements saved") {
		t.Fatalf("want the capture failure reported, got %v", err)
	}

	path := filepath.Join(gamesDir, "professor-layton-and-the-last-specter", "_index.md")
	doc, err := model.LoadDoc(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := doc.RASubsetIDs(); len(got) != 1 || got[0] != "25709" {
		t.Fatalf("RASubsetIDs() = %q, want [25709]", got)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "Prose that must survive the write.") {
		t.Errorf("the markdown body was not preserved:\n%s", raw)
	}

	// Clicking the same button again is a no-op, not a second link.
	_, err = AttachSubset(context.Background(), gamesDir, "professor-layton-and-the-last-specter", "25709", ra, Credentials{})
	if !IsAlreadyAttached(err) {
		t.Errorf("re-attaching = %v, want the already-attached sentinel", err)
	}
}

func TestAttachSubset_RefusesAGameWithNoRAID(t *testing.T) {
	resetSubsetCache(t)
	gamesDir := gameFixture(t, "tunic", `---
title: "Tunic"
platform: "PC"
retroachievements_id:
steam_appid: 553420
status: "playing"
---
`)
	ra := &fakeRA{byID: map[string]retroachievements.RAProgress{"25709": subsetProgress(7601)}}
	_, err := AttachSubset(context.Background(), gamesDir, "tunic", "25709", ra, Credentials{})
	if err == nil || !strings.Contains(err.Error(), "no retroachievements_id") {
		t.Fatalf("want a refusal naming the missing id, got %v", err)
	}
}

// A subset whose base game isn't logged at all: the base game is created from
// what RA reports about *it*, not from the subset's title, and the subset is
// attached to the result.
func TestCreateBaseAndAttach_CreatesTheBaseGameFromTheProvider(t *testing.T) {
	resetSubsetCache(t)
	gamesDir := filepath.Join(t.TempDir(), "content", "games")
	if err := os.MkdirAll(gamesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ra := &fakeRA{byID: map[string]retroachievements.RAProgress{
		"25709": subsetProgress(7601),
		"7601": {
			Title: "Professor Layton and the Last Specter", ConsoleName: "Nintendo DS",
			NumAchievements: 40, NumAwardedToUser: 16,
			HighestAwardKind: "beaten-hardcore", HighestAwardDate: "2026-07-28T02:40:24+00:00",
			Achievements: map[string]retroachievements.RAAchievement{
				"1": {ID: 1, DateEarnedHardcore: "2026-06-15 22:27:10"},
			},
		},
	}}

	slug, title, err := CreateBaseAndAttach(context.Background(), gamesDir, "25709", ra, Credentials{})
	// No credentials, so the archive capture can't run; the entry and the link
	// are what this call is really responsible for.
	if err != nil && !strings.Contains(err.Error(), "no achievements saved") {
		t.Fatalf("unexpected error: %v", err)
	}
	if slug != "professor-layton-and-the-last-specter" || title != "Professor Layton and the Last Specter" {
		t.Fatalf("created %q/%q, want the parent's own title from RA", slug, title)
	}

	doc, err := model.LoadDoc(filepath.Join(gamesDir, slug, "_index.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !doc.FM.Draft {
		t.Error("a game created from inferred data must land as a draft")
	}
	if doc.FM.Status != "finished" {
		t.Errorf("status = %q, want finished (beaten-hardcore)", doc.FM.Status)
	}
	if raID, _ := doc.ExternalIDs(); raID != "7601" {
		t.Errorf("retroachievements_id = %q, want the parent's 7601", raID)
	}
	if got := doc.RASubsetIDs(); len(got) != 1 || got[0] != "25709" {
		t.Errorf("RASubsetIDs() = %q, want [25709]", got)
	}
}

// The base game has its own scan row whenever any of its achievements are
// earned, so it can be created from a page rendered before this one. That's a
// stale button, not an error: the attach half is simply the part left to do.
func TestCreateBaseAndAttach_AttachesWhenTheBaseGameAlreadyExists(t *testing.T) {
	resetSubsetCache(t)
	gamesDir := gameFixture(t, "professor-layton-and-the-last-specter", laytonIndexMD)
	ra := &fakeRA{byID: map[string]retroachievements.RAProgress{
		"25709": subsetProgress(7601),
		"7601":  {Title: "Professor Layton and the Last Specter", ConsoleName: "Nintendo DS"},
	}}

	slug, _, err := CreateBaseAndAttach(context.Background(), gamesDir, "25709", ra, Credentials{})
	if err != nil && !strings.Contains(err.Error(), "no achievements saved") {
		t.Fatalf("unexpected error: %v", err)
	}
	doc, err := model.LoadDoc(filepath.Join(gamesDir, slug, "_index.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got := doc.RASubsetIDs(); len(got) != 1 || got[0] != "25709" {
		t.Errorf("RASubsetIDs() = %q, want [25709]", got)
	}
	// The existing entry must be kept, not replaced by a fresh draft.
	if doc.FM.Draft {
		t.Error("the already-logged game was overwritten with a scan-created draft")
	}
}

// A subset record's title is the only place its name ("Mouse Alley") is
// recorded anywhere in the repo, and a refresh of the base game runs through
// the same loop for every set it has — so the game's own title must never be
// written over it.
func TestSaveAchievements_NeverTitlesASubsetWithTheGamesOwnName(t *testing.T) {
	archiveDir := t.TempDir()
	if _, err := model.SaveRecord(archiveDir, model.ProviderRA, "25709",
		"Professor Layton and the Last Specter [Subset - Mouse Alley]",
		&model.ProviderRecord{Total: 52, Achievements: []model.ArchivedAchievement{unlocked("a", "2026-07-12T00:00:00-05:00")}}); err != nil {
		t.Fatal(err)
	}

	raStubProgress(t, `{"Title":"Professor Layton and the Last Specter [Subset - Mouse Alley]",
	  "ConsoleName":"Nintendo DS","ParentGameID":7601,"NumAchievements":52,
	  "Achievements":{"1":{"ID":1,"Title":"A","DateEarnedHardcore":"2026-07-12 10:00:00"}}}`)

	links := model.BuildProviderLinks("", "", "", "", "", "25709")
	saved, err := SaveAchievements(context.Background(), archiveDir,
		"Professor Layton and the Last Specter", links, Credentials{RAUsername: "u", RAAPIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != 1 || !saved[0].Subset {
		t.Fatalf("saved = %+v, want one subset record", saved)
	}
	if got := saved[0].Record.Title; got != "Professor Layton and the Last Specter [Subset - Mouse Alley]" {
		t.Errorf("subset record title = %q, want RetroAchievements' own name for the set", got)
	}
	if got := saved[0].Label(); got != "retroachievements subset Mouse Alley" {
		t.Errorf("Label() = %q, want the set named — two rows reading %q say nothing", got, model.ProviderRA)
	}
}
