package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// write a playthroughs.yaml into a temp game bundle and load it back.
func loadFixture(t *testing.T, body string) *PlaythroughsFile {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(PlaythroughsPath(dir), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	pf, err := LoadPlaythroughs(dir)
	if err != nil {
		t.Fatal(err)
	}
	return pf
}

// saveAndReload round-trips through disk, which is what actually matters:
// every defence against losing a field has to survive encode *and* decode.
func saveAndReload(t *testing.T, pf *PlaythroughsFile) *PlaythroughsFile {
	t.Helper()
	if err := pf.Save(); err != nil {
		t.Fatal(err)
	}
	out, err := LoadPlaythroughs(filepath.Dir(pf.Path))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// The shape that used to be destroyed: fields written between the dates.
// As a line splice, converting this entry to sessions deleted everything from
// `started:` through `finished:` — status and rating included — and exited 0.
const flatWithFieldsBetween = `playthroughs:
  - started: 2026-01-04
    status: playing
    rating: 7
    mood: obsessive
    finished: 2026-02-11
    notes: |
      First run.
      Two lines.
`

func TestAddSession_ConvertsWithoutDisturbingSiblings(t *testing.T) {
	pf := loadFixture(t, flatWithFieldsBetween)
	if err := pf.AddSession(0, "2026-05-19", "2026-05-24", ""); err != nil {
		t.Fatal(err)
	}
	got := saveAndReload(t, pf)

	e := got.Playthroughs[0]
	if e.Status != "playing" {
		t.Errorf("status lost: %q", e.Status)
	}
	if e.RatingString() != "7" {
		t.Errorf("rating lost: %q", e.RatingString())
	}
	if e.Extra["mood"] != "obsessive" {
		t.Errorf("unknown field lost: %v", e.Extra)
	}
	if !strings.Contains(e.Notes, "First run.") || !strings.Contains(e.Notes, "Two lines.") {
		t.Errorf("notes lost: %q", e.Notes)
	}

	// Both dates moved into the list rather than being dropped.
	if len(e.Sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(e.Sessions))
	}
	if e.Sessions[0].Started != "2026-01-04" || e.Sessions[0].Finished != "2026-02-11" {
		t.Errorf("original range not preserved: %+v", e.Sessions[0])
	}
	if e.Sessions[1].Started != "2026-05-19" || e.Sessions[1].Finished != "2026-05-24" {
		t.Errorf("new session wrong: %+v", e.Sessions[1])
	}
	if e.Started != "" || e.Finished != "" {
		t.Errorf("flat dates should have moved, not been copied: %q/%q", e.Started, e.Finished)
	}
}

// The second bug: appending a session used to adopt a trailing field from the
// previous one, because a session was assumed to be exactly two lines.
func TestAddSession_LeavesTheEarlierSessionAlone(t *testing.T) {
	pf := loadFixture(t, `playthroughs:
  - status: playing
    sessions:
      - started: 2026-03-01
        finished: 2026-03-04
      - started: 2026-04-02
        finished: 2026-04-09
        note: final stretch
`)
	if err := pf.AddSession(0, "2026-05-19", "", ""); err != nil {
		t.Fatal(err)
	}
	got := saveAndReload(t, pf).Playthroughs[0]

	if len(got.Sessions) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(got.Sessions))
	}
	if got.Sessions[1].Extra["note"] != "final stretch" {
		t.Errorf("trailing field moved off its session: %+v", got.Sessions[1])
	}
	if _, adopted := got.Sessions[2].Extra["note"]; adopted {
		t.Error("the new session adopted the previous session's field")
	}
	if got.Sessions[2].Started != "2026-05-19" || got.Sessions[2].Finished != "" {
		t.Errorf("new session wrong: %+v", got.Sessions[2])
	}
}

// An ongoing session keeps its `finished` key, so the shape stays stable.
func TestOngoingSessionKeepsTheFinishedKey(t *testing.T) {
	pf := loadFixture(t, "playthroughs:\n  - started: 2026-01-04\n    finished: 2026-02-11\n")
	if err := pf.AddSession(0, "2026-05-19", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := pf.Save(); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(pf.Path)
	if strings.Count(string(raw), "finished:") != 2 {
		t.Errorf("expected both sessions to carry a finished key:\n%s", raw)
	}
}

// Fields this tool has never heard of must survive a whole-file rewrite. The
// encoder drops anything the struct doesn't model, so `Extra` is what stands
// between an unrecognised key and silent deletion.
func TestUnknownFieldsSurviveARewrite(t *testing.T) {
	// `platform` used to stand in for an unknown key here. It is a modeled
	// field now, so it would prove nothing — these have to be keys the struct
	// really doesn't know about.
	pf := loadFixture(t, `playthroughs:
  - started: 2026-01-04
    finished: 2026-02-11
    platform: Switch
    mood: obsessive
    co_op_with: sam
    sessions:
      - started: 2026-01-04
        finished: 2026-01-09
        device: deck
`)
	if err := pf.UpdatePlaythrough(0, "2026-02-12", "finished", "Switch", "", "8", "done"); err != nil {
		t.Fatal(err)
	}
	got := saveAndReload(t, pf).Playthroughs[0]

	if got.Extra["mood"] != "obsessive" || got.Extra["co_op_with"] != "sam" {
		t.Errorf("entry-level unknown fields lost: %v", got.Extra)
	}
	if got.Platform != "Switch" {
		t.Errorf("platform = %q, want Switch", got.Platform)
	}
	if got.Sessions[0].Extra["device"] != "deck" {
		t.Errorf("session-level unknown field lost: %v", got.Sessions[0].Extra)
	}
}

// Dates must come back out exactly as written. YAML resolves an unquoted
// 2026-01-04 to a timestamp, and letting that become a time.Time would
// rewrite every date in the file on the next save.
func TestDatesRoundTripUnchanged(t *testing.T) {
	pf := loadFixture(t, "playthroughs:\n  - started: 2026-01-04\n    finished: 2026-02-11\n")
	got := saveAndReload(t, pf).Playthroughs[0]
	if got.Started != "2026-01-04" || got.Finished != "2026-02-11" {
		t.Fatalf("dates changed: %q/%q", got.Started, got.Finished)
	}
}

// A bare `rating: 9` must not come back as the string "9" — Hugo templates
// render it directly.
func TestRatingStaysANumber(t *testing.T) {
	pf := loadFixture(t, "playthroughs:\n  - started: 2026-01-04\n    rating: 9\n")
	if got := pf.Playthroughs[0].RatingString(); got != "9" {
		t.Fatalf("RatingString = %q", got)
	}
	if err := pf.Save(); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(pf.Path)
	if !strings.Contains(string(raw), "rating: 9\n") {
		t.Errorf("rating should be written unquoted:\n%s", raw)
	}
}

func TestNotesBlockRoundTrips(t *testing.T) {
	pf := loadFixture(t, `playthroughs:
  - started: 2026-01-04
    notes: |
      Line one.

      Line three after a blank.
`)
	got := saveAndReload(t, pf).Playthroughs[0]
	want := "Line one.\n\nLine three after a blank.\n"
	if got.Notes != want {
		t.Fatalf("notes changed:\n%q\nwant\n%q", got.Notes, want)
	}
}

func TestLoadPlaythroughs_MissingFileIsEmpty(t *testing.T) {
	pf, err := LoadPlaythroughs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(pf.Playthroughs) != 0 {
		t.Errorf("expected no playthroughs, got %d", len(pf.Playthroughs))
	}
}

// A game with nothing logged should have no file at all rather than an empty
// one, so Hugo's `.Resources.Get` simply misses.
func TestSaveRemovesTheFileWhenEmpty(t *testing.T) {
	pf := loadFixture(t, "playthroughs:\n  - started: 2026-01-04\n")
	pf.Playthroughs = nil
	if err := pf.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pf.Path); !os.IsNotExist(err) {
		t.Error("an emptied file should be removed, not left blank")
	}
}

func TestAddSessionRejectsAnEntryWithNoStartDate(t *testing.T) {
	pf := loadFixture(t, "playthroughs:\n  - status: backlog\n")
	if err := pf.AddSession(0, "2026-05-19", "", ""); err == nil {
		t.Fatal("expected an error rather than a session with no origin")
	}
}

// The case this field exists for: one game, two runs, different platforms and
// different outcomes. Persona 5 Royal was played through on PS4 and again on
// Steam; Octopath Traveler was finished on PC and dropped on Switch. Neither
// is expressible with a single game-level platform.
func TestPlaythroughsCarrySeparatePlatforms(t *testing.T) {
	pf := loadFixture(t, `playthroughs:
  - started: 2020-05-19
    finished: 2020-10-17
    status: mastered
    platform: PS4
  - started: 2023-03-12
    finished: 2023-05-14
    status: mastered
    platform: PC
`)
	got := saveAndReload(t, pf).Playthroughs
	if len(got) != 2 {
		t.Fatalf("got %d playthroughs, want 2", len(got))
	}
	if got[0].Platform != "PS4" || got[1].Platform != "PC" {
		t.Errorf("platforms = %q / %q, want PS4 / PC", got[0].Platform, got[1].Platform)
	}
	if got[0].Finished != "2020-10-17" || got[1].Finished != "2023-05-14" {
		t.Errorf("dates disturbed: %+v", got)
	}
}

// A blank platform must not write the key. The entry inherits the game's
// front-matter platform, so pinning a copy of it would make correcting the
// game later silently fail to correct its runs.
func TestBlankPlatformIsOmitted(t *testing.T) {
	pf := loadFixture(t, "playthroughs:\n  - started: 2026-01-04\n    status: playing\n")
	if err := pf.UpdatePlaythrough(0, "", "playing", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	out, err := pf.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "platform") {
		t.Errorf("blank platform emitted a key:\n%s", out)
	}
}

func TestUpdateWritesFinishedToTheLastSession(t *testing.T) {
	pf := loadFixture(t, `playthroughs:
  - status: playing
    sessions:
      - started: 2026-03-01
        finished: 2026-03-04
      - started: 2026-04-02
        finished:
`)
	if err := pf.UpdatePlaythrough(0, "2026-04-09", "finished", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	got := saveAndReload(t, pf).Playthroughs[0]
	if got.Sessions[0].Finished != "2026-03-04" {
		t.Errorf("earlier session disturbed: %+v", got.Sessions[0])
	}
	if got.Sessions[1].Finished != "2026-04-09" {
		t.Errorf("finish date went to the wrong place: %+v", got.Sessions[1])
	}
	if got.Finished != "" {
		t.Error("an entry with sessions must not grow a flat finished date")
	}
}

func TestViewsRenderEveryFieldAsText(t *testing.T) {
	pf := loadFixture(t, `playthroughs:
  - started: 2026-01-04
    finished: 2026-02-11
    status: finished
    rating: 9
    notes: |
      done
  - status: playing
    sessions:
      - started: 2026-03-01
        finished:
`)
	views := pf.Views()
	if len(views) != 2 {
		t.Fatalf("got %d views", len(views))
	}
	if views[0].Rating != "9" || views[0].Started != "2026-01-04" {
		t.Errorf("flat view wrong: %+v", views[0])
	}
	if views[0].HasSessions() || views[0].IsOpen() {
		t.Error("a finished flat entry is neither sessioned nor open")
	}
	if !views[1].HasSessions() || !views[1].IsOpen() {
		t.Error("an entry whose last session has no finish date is open")
	}
}

// The pre-write check is the backstop for exactly the failure this redesign
// was meant to remove: a rewrite that produces valid YAML with a field
// missing. Prove it fires rather than trusting that it would.
func TestCheckNoFieldLossCatchesASilentDrop(t *testing.T) {
	before, err := CollectYAMLFields([]byte(flatWithFieldsBetween))
	if err != nil {
		t.Fatal(err)
	}
	damaged, err := CollectYAMLFields([]byte("playthroughs:\n  - started: 2026-01-04\n    finished: 2026-02-11\n"))
	if err != nil {
		t.Fatal(err)
	}

	err = CheckNoFieldLoss(before, damaged, nil)
	if err == nil {
		t.Fatal("dropping status, rating, mood and notes was not reported")
	}
	for _, want := range []string{"status", "rating", "mood", "notes"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should name %q, got: %v", want, err)
		}
	}

	// The same removal is fine when the operation declares it.
	allowed := []string{
		"playthroughs[0].status", "playthroughs[0].rating",
		"playthroughs[0].mood", "playthroughs[0].notes",
	}
	if err := CheckNoFieldLoss(before, damaged, allowed); err != nil {
		t.Errorf("declared removals should pass: %v", err)
	}
}

// A field that was already empty carries no data, so `omitempty` dropping it
// is not a loss — otherwise every blank `finished:` would trip the check.
func TestCheckNoFieldLossIgnoresEmptyValues(t *testing.T) {
	before, _ := CollectYAMLFields([]byte("playthroughs:\n  - started: 2026-01-04\n    finished:\n"))
	after, _ := CollectYAMLFields([]byte("playthroughs:\n  - started: 2026-01-04\n"))
	if err := CheckNoFieldLoss(before, after, nil); err != nil {
		t.Errorf("dropping an already-empty field is not data loss: %v", err)
	}
}

// The conversion is the one operation that legitimately removes paths, and
// the allowlist it declares has to match what actually disappears.
func TestConversionDeclaresExactlyWhatItRemoves(t *testing.T) {
	pf := loadFixture(t, flatWithFieldsBetween)
	before, err := CollectYAMLFields(pf.Raw())
	if err != nil {
		t.Fatal(err)
	}
	if err := pf.AddSession(0, "2026-05-19", "", ""); err != nil {
		t.Fatal(err)
	}
	encoded, err := pf.Encode()
	if err != nil {
		t.Fatal(err)
	}
	after, err := CollectYAMLFields(encoded)
	if err != nil {
		t.Fatal(err)
	}

	allowed := []string{"playthroughs[0].started", "playthroughs[0].finished"}
	if err := CheckNoFieldLoss(before, after, allowed); err != nil {
		t.Fatalf("conversion lost something it did not declare: %v", err)
	}
	// And the allowlist isn't hiding a wider problem: without it, only those
	// two paths should be reported.
	err = CheckNoFieldLoss(before, after, nil)
	if err == nil {
		t.Fatal("expected the moved date pair to be reported when undeclared")
	}
	for _, unwanted := range []string{"status", "rating", "mood", "notes"} {
		if strings.Contains(err.Error(), unwanted) {
			t.Errorf("conversion should not touch %q, got: %v", unwanted, err)
		}
	}
}

// "ongoing" on the timeline comes from a blank finished date, not from
// status text — the bug SyncStatus exists to close is a front-matter status
// of "finished" with a playthrough whose last session is still open.
func TestSyncStatus_ClosesTheLastOpenSession(t *testing.T) {
	pf := loadFixture(t, `playthroughs:
  - status: playing
    sessions:
      - started: "2024-01-01"
        finished: "2024-01-05"
      - started: "2024-06-01"
        finished: ""
`)
	if !pf.SyncStatus("finished", "2024-06-03") {
		t.Fatal("expected a change")
	}
	e := pf.Playthroughs[0]
	if e.Status != "finished" {
		t.Errorf("status = %q, want finished", e.Status)
	}
	last := e.Sessions[len(e.Sessions)-1]
	if last.Finished != "2024-06-03" {
		t.Errorf("last session finished = %q, want 2024-06-03", last.Finished)
	}
	if e.Sessions[0].Finished != "2024-01-05" {
		t.Error("an earlier, already-closed session must not be touched")
	}
}

func TestSyncStatus_NeverOverwritesAnExistingFinishedDate(t *testing.T) {
	pf := loadFixture(t, `playthroughs:
  - status: playing
    started: "2024-01-01"
    finished: "2024-01-05"
`)
	if pf.SyncStatus("finished", "2024-06-03") == false {
		t.Fatal("status itself should still change even though the date doesn't")
	}
	if pf.Playthroughs[0].Finished != "2024-01-05" {
		t.Error("an existing finished date must never be overwritten")
	}
}

// Pausing isn't finishing: the entry's status should follow, but callers are
// expected to pass closedOn="" for it (see reviewStale), and even so, no
// date on the entry should ever be touched.
func TestSyncStatus_PausedNeverSetsAFinishedDate(t *testing.T) {
	pf := loadFixture(t, `playthroughs:
  - status: playing
    sessions:
      - started: "2024-01-01"
        finished: ""
`)
	if !pf.SyncStatus("paused", "") {
		t.Fatal("expected a change")
	}
	e := pf.Playthroughs[0]
	if e.Status != "paused" {
		t.Errorf("status = %q, want paused", e.Status)
	}
	if e.Sessions[0].Finished != "" {
		t.Errorf("pausing must not close the open session, got finished=%q", e.Sessions[0].Finished)
	}
}

func TestSyncStatus_SkipsWhenAmbiguousOrIrrelevant(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		status  string
	}{
		{"no playthroughs", `playthroughs: []`, "finished"},
		{"multiple playthroughs", "playthroughs:\n  - status: playing\n    started: \"2024-01-01\"\n  - status: playing\n    started: \"2024-02-01\"\n", "finished"},
		{"status is playing", "playthroughs:\n  - status: playing\n    started: \"2024-01-01\"\n", "playing"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pf := loadFixture(t, tc.fixture)
			if pf.SyncStatus(tc.status, "2024-06-03") {
				t.Error("expected no change")
			}
		})
	}
}

// A one-shot game (endless/multiplayer/software) is capped at one entry
// per platform, not one entry outright. Saves don't cross consoles, so a
// second platform is a genuinely separate record — but a second entry on the
// same platform is the fragmentation the cap exists to prevent.
const threeSessionsFixture = `playthroughs:
  - status: playing
    sessions:
      - started: "2024-01-01"
        finished: "2024-01-05"
        title: prologue
      - started: "2024-02-01"
        finished: "2024-02-10"
      - started: "2024-03-01"
        finished: "2024-03-15"
        mood: grindy
`

func TestRemoveSession_LeavesEarlierSessionsUntouched(t *testing.T) {
	pf := loadFixture(t, threeSessionsFixture)
	if err := pf.RemoveSession(0, 1); err != nil {
		t.Fatal(err)
	}
	got := saveAndReload(t, pf).Playthroughs[0]
	if got.Sessions[0].Title != "prologue" || got.Sessions[0].Started != "2024-01-01" {
		t.Errorf("earlier session disturbed: %+v", got.Sessions[0])
	}
}

func TestRemoveSession_ShiftsLaterSessionsCorrectly(t *testing.T) {
	pf := loadFixture(t, threeSessionsFixture)
	if err := pf.RemoveSession(0, 1); err != nil {
		t.Fatal(err)
	}
	got := saveAndReload(t, pf).Playthroughs[0]
	if len(got.Sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(got.Sessions))
	}
	if got.Sessions[1].Started != "2024-03-01" || got.Sessions[1].Extra["mood"] != "grindy" {
		t.Errorf("shifted session lost its own data: %+v", got.Sessions[1])
	}
}

func TestRemoveSession_RefusesToLeaveZeroSessions(t *testing.T) {
	pf := loadFixture(t, "playthroughs:\n  - status: playing\n    sessions:\n      - started: \"2024-01-01\"\n")
	if err := pf.RemoveSession(0, 0); err == nil {
		t.Fatal("expected an error rather than an entry with zero sessions")
	}
	if len(pf.Playthroughs[0].Sessions) != 1 {
		t.Error("the rejected removal must not have mutated anything")
	}
}

// The direct shift-corruption repro: the deleted session has a title the
// session sliding into its slot lacks, so that title has to be declared or
// CheckNoFieldLoss must catch it.
func TestRemoveSession_TitleThatDoesNotCarryOverIsCaughtCorrectly(t *testing.T) {
	fixture := `playthroughs:
  - status: playing
    sessions:
      - started: "2024-01-01"
        finished: "2024-01-05"
      - started: "2024-02-01"
        finished: "2024-02-10"
        title: raid night
      - started: "2024-03-01"
        finished: "2024-03-15"
`
	pf := loadFixture(t, fixture)
	before, err := CollectYAMLFields(pf.Raw())
	if err != nil {
		t.Fatal(err)
	}
	beforeSessions := append([]SessionEntry(nil), pf.Playthroughs[0].Sessions...)

	if err := pf.RemoveSession(0, 1); err != nil {
		t.Fatal(err)
	}
	encoded, err := pf.Encode()
	if err != nil {
		t.Fatal(err)
	}
	after, err := CollectYAMLFields(encoded)
	if err != nil {
		t.Fatal(err)
	}

	allowed := RemoveSessionAllowedPaths(0, beforeSessions, 1)
	if err := CheckNoFieldLoss(before, after, allowed); err != nil {
		t.Fatalf("removal lost something it did not declare: %v", err)
	}

	err = CheckNoFieldLoss(before, after, nil)
	if err == nil || !strings.Contains(err.Error(), "sessions[1].title") {
		t.Fatalf("expected sessions[1].title reported when undeclared, got: %v", err)
	}
}

// The allowlist must be exact: declaring it passes, and without it the
// error names precisely those paths and nothing else.
func TestRemoveSessionAllowedPaths_MatchesWhatActuallyVanishes(t *testing.T) {
	pf := loadFixture(t, threeSessionsFixture)
	before, err := CollectYAMLFields(pf.Raw())
	if err != nil {
		t.Fatal(err)
	}
	beforeSessions := append([]SessionEntry(nil), pf.Playthroughs[0].Sessions...)

	if err := pf.RemoveSession(0, 0); err != nil {
		t.Fatal(err)
	}
	encoded, err := pf.Encode()
	if err != nil {
		t.Fatal(err)
	}
	after, err := CollectYAMLFields(encoded)
	if err != nil {
		t.Fatal(err)
	}

	allowed := RemoveSessionAllowedPaths(0, beforeSessions, 0)
	if err := CheckNoFieldLoss(before, after, allowed); err != nil {
		t.Fatalf("declared allowlist should pass: %v", err)
	}

	err = CheckNoFieldLoss(before, after, nil)
	if err == nil {
		t.Fatal("expected the vanished paths to be reported when undeclared")
	}
	for _, want := range []string{"sessions[0].title", "sessions[2].started", "sessions[2].finished", "sessions[2].mood"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should name %q, got: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "sessions[1]") {
		t.Errorf("session 1 should be untouched, got: %v", err)
	}
}

func TestMoveSession_SwapsAdjacentSessions(t *testing.T) {
	pf := loadFixture(t, threeSessionsFixture)
	if err := pf.MoveSession(0, 1); err != nil {
		t.Fatal(err)
	}
	got := saveAndReload(t, pf).Playthroughs[0]
	if len(got.Sessions) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(got.Sessions))
	}
	if got.Sessions[1].Started != "2024-03-01" || got.Sessions[1].Extra["mood"] != "grindy" {
		t.Errorf("session that moved up lost its own data: %+v", got.Sessions[1])
	}
	if got.Sessions[2].Started != "2024-02-01" || got.Sessions[2].Title != "" {
		t.Errorf("session that moved down lost its own data: %+v", got.Sessions[2])
	}
	if got.Sessions[0].Title != "prologue" {
		t.Errorf("session not part of the swap was disturbed: %+v", got.Sessions[0])
	}
}

func TestMoveSession_RefusesOutOfRange(t *testing.T) {
	pf := loadFixture(t, threeSessionsFixture)
	if err := pf.MoveSession(0, -1); err == nil {
		t.Fatal("expected an error moving session 0 up past the start")
	}
	if err := pf.MoveSession(0, 2); err == nil {
		t.Fatal("expected an error moving the last session down past the end")
	}
	got := pf.Playthroughs[0]
	if got.Sessions[0].Title != "prologue" || got.Sessions[2].Extra["mood"] != "grindy" {
		t.Error("a rejected move must not have mutated anything")
	}
}

// Same shape of proof as TestRemoveSessionAllowedPaths_MatchesWhatActuallyVanishes:
// the swap moves title/Extra fields to a different index, so the path that
// used to hold them has to be declared or CheckNoFieldLoss must catch it.
func TestMoveSessionAllowedPaths_MatchesWhatActuallyVanishes(t *testing.T) {
	pf := loadFixture(t, threeSessionsFixture)
	before, err := CollectYAMLFields(pf.Raw())
	if err != nil {
		t.Fatal(err)
	}
	beforeSessions := append([]SessionEntry(nil), pf.Playthroughs[0].Sessions...)

	if err := pf.MoveSession(0, 1); err != nil {
		t.Fatal(err)
	}
	encoded, err := pf.Encode()
	if err != nil {
		t.Fatal(err)
	}
	after, err := CollectYAMLFields(encoded)
	if err != nil {
		t.Fatal(err)
	}

	allowed := MoveSessionAllowedPaths(0, beforeSessions, 1)
	if err := CheckNoFieldLoss(before, after, allowed); err != nil {
		t.Fatalf("declared allowlist should pass: %v", err)
	}

	err = CheckNoFieldLoss(before, after, nil)
	if err == nil {
		t.Fatal("expected the vanished paths to be reported when undeclared")
	}
	for _, want := range []string{"sessions[2].mood"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should name %q, got: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "sessions[0]") {
		t.Errorf("session 0 should be untouched, got: %v", err)
	}
}

func TestTruncateSessionAllowedPaths_MatchesWhatActuallyVanishes(t *testing.T) {
	pf := loadFixture(t, threeSessionsFixture)
	before, err := CollectYAMLFields(pf.Raw())
	if err != nil {
		t.Fatal(err)
	}
	beforeSessions := append([]SessionEntry(nil), pf.Playthroughs[0].Sessions...)

	pf.Playthroughs[0].Sessions = pf.Playthroughs[0].Sessions[:1]
	encoded, err := pf.Encode()
	if err != nil {
		t.Fatal(err)
	}
	after, err := CollectYAMLFields(encoded)
	if err != nil {
		t.Fatal(err)
	}

	allowed := TruncateSessionAllowedPaths(0, beforeSessions, 1)
	if err := CheckNoFieldLoss(before, after, allowed); err != nil {
		t.Fatalf("declared allowlist should pass: %v", err)
	}

	err = CheckNoFieldLoss(before, after, nil)
	if err == nil {
		t.Fatal("expected the truncated paths to be reported when undeclared")
	}
	for _, want := range []string{"sessions[1].started", "sessions[1].finished", "sessions[2].started", "sessions[2].finished", "sessions[2].mood"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should name %q, got: %v", want, err)
		}
	}
}

func TestSplitPlaythrough_MovesTrailingSessionsToANewEntry(t *testing.T) {
	pf := loadFixture(t, threeSessionsFixture)
	newIdx, err := pf.SplitPlaythrough(0, 1, "finished")
	if err != nil {
		t.Fatal(err)
	}
	got := saveAndReload(t, pf)

	if len(got.Playthroughs[0].Sessions) != 1 || got.Playthroughs[0].Sessions[0].Title != "prologue" {
		t.Errorf("source entry should keep only session 0: %+v", got.Playthroughs[0].Sessions)
	}
	newEntry := got.Playthroughs[newIdx]
	if len(newEntry.Sessions) != 2 {
		t.Fatalf("expected 2 sessions moved, got %d", len(newEntry.Sessions))
	}
	if newEntry.Sessions[0].Started != "2024-02-01" || newEntry.Sessions[1].Extra["mood"] != "grindy" {
		t.Errorf("moved sessions lost data: %+v", newEntry.Sessions)
	}
}

func TestSplitPlaythrough_InheritsPlatformNotStatusOrNotesOrRating(t *testing.T) {
	pf := loadFixture(t, `playthroughs:
  - status: dropped
    platform: Switch
    rating: 7
    notes: speedrun practice
    sessions:
      - started: "2024-01-01"
        finished: "2024-01-05"
      - started: "2024-02-01"
        finished: "2024-02-10"
`)
	newIdx, err := pf.SplitPlaythrough(0, 1, "finished")
	if err != nil {
		t.Fatal(err)
	}
	newEntry := pf.Playthroughs[newIdx]
	if newEntry.Platform != "Switch" {
		t.Errorf("platform = %q, want inherited Switch", newEntry.Platform)
	}
	if newEntry.Status != "finished" {
		t.Errorf("status = %q, want finished, not copied from source", newEntry.Status)
	}
	if newEntry.Notes != "" || newEntry.RatingString() != "" {
		t.Errorf("notes/rating should start blank, got notes=%q rating=%q", newEntry.Notes, newEntry.RatingString())
	}
}

func TestSplitPlaythrough_RejectsSplittingAtIndexZero(t *testing.T) {
	pf := loadFixture(t, threeSessionsFixture)
	if _, err := pf.SplitPlaythrough(0, 0, "finished"); err == nil {
		t.Fatal("expected an error — the source entry must keep at least session 0")
	}
	if len(pf.Playthroughs) != 1 || len(pf.Playthroughs[0].Sessions) != 3 {
		t.Error("the rejected split must not have mutated anything")
	}
}

func TestSplitPlaythrough_AppendsAfterExistingLaterEntries(t *testing.T) {
	pf := loadFixture(t, `playthroughs:
  - status: playing
    platform: PC
    sessions:
      - started: "2024-01-01"
        finished: "2024-01-05"
      - started: "2024-02-01"
        finished: "2024-02-10"
  - started: "2020-01-01"
    finished: "2020-02-01"
    status: finished
    platform: Switch
`)
	newIdx, err := pf.SplitPlaythrough(0, 1, "finished")
	if err != nil {
		t.Fatal(err)
	}
	if newIdx != 2 {
		t.Fatalf("expected the new entry at index 2, got %d", newIdx)
	}
	middle := pf.Playthroughs[1]
	if middle.Started != "2020-01-01" || middle.Platform != "Switch" {
		t.Errorf("the entry after idx must be undisturbed: %+v", middle)
	}
}

func TestEditSession_UpdatesInPlaceWithoutDisturbingSiblings(t *testing.T) {
	pf := loadFixture(t, threeSessionsFixture)
	if err := pf.EditSession(0, 1, "2024-02-02", "2024-02-12", "patched"); err != nil {
		t.Fatal(err)
	}
	got := saveAndReload(t, pf).Playthroughs[0]
	if got.Sessions[0].Title != "prologue" {
		t.Error("editing session 1 must not disturb session 0")
	}
	if got.Sessions[1].Started != "2024-02-02" || got.Sessions[1].Finished != "2024-02-12" || got.Sessions[1].Title != "patched" {
		t.Errorf("edit not applied: %+v", got.Sessions[1])
	}
	if got.Sessions[2].Extra["mood"] != "grindy" {
		t.Error("editing session 1 must not disturb session 2")
	}
}

// EditSession on session 0 of an entry with no sessions: list yet must
// convert the flat started/finished pair in place — this is what lets the
// web UI's implicit "session 1" row (server_games.go's buildSessionRows) be
// edited before an explicit session has ever been logged.
func TestEditSession_ConvertsFlatPairWithoutDisturbingSiblingFields(t *testing.T) {
	pf := loadFixture(t, flatWithFieldsBetween)
	if err := pf.EditSession(0, 0, "2026-01-10", "2026-02-20", "retitled"); err != nil {
		t.Fatal(err)
	}
	got := saveAndReload(t, pf).Playthroughs[0]

	if got.Status != "playing" {
		t.Errorf("status lost: %q", got.Status)
	}
	if got.RatingString() != "7" {
		t.Errorf("rating lost: %q", got.RatingString())
	}
	if got.Extra["mood"] != "obsessive" {
		t.Errorf("unknown field lost: %v", got.Extra)
	}
	if len(got.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(got.Sessions))
	}
	if got.Sessions[0].Started != "2026-01-10" || got.Sessions[0].Finished != "2026-02-20" || got.Sessions[0].Title != "retitled" {
		t.Errorf("edit not applied: %+v", got.Sessions[0])
	}
	if got.Started != "" || got.Finished != "" {
		t.Errorf("flat dates should have moved, not been copied: %q/%q", got.Started, got.Finished)
	}
}

func TestEditSession_OnFlatEntryWithNoStartDateFails(t *testing.T) {
	pf := loadFixture(t, "playthroughs:\n  - status: backlog\n")
	if err := pf.EditSession(0, 0, "2026-05-19", "", ""); err == nil {
		t.Fatal("expected an error rather than a session with no origin")
	}
}

func TestEditSession_ClearingTitleDropsTheKey(t *testing.T) {
	pf := loadFixture(t, threeSessionsFixture)
	if err := pf.EditSession(0, 0, "2024-01-01", "2024-01-05", ""); err != nil {
		t.Fatal(err)
	}
	out, err := pf.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "title: prologue") {
		t.Errorf("cleared title should not be written:\n%s", out)
	}
}

func TestViewsIncludeSessionTitle(t *testing.T) {
	pf := loadFixture(t, threeSessionsFixture)
	views := pf.Views()
	if views[0].Sessions[0].Title != "prologue" {
		t.Errorf("session title = %q, want prologue", views[0].Sessions[0].Title)
	}
}

func TestGraduatePlannedPlaythrough_FillsFieldsInPlace(t *testing.T) {
	pf := loadFixture(t, `playthroughs:
  - status: finished
    started: "2021-05-01"
    finished: "2021-05-15"
  - status: planned
    platform: PC
    notes: try NG+
`)
	err := pf.GraduatePlannedPlaythrough(1, PlaythroughFields{
		Started:  "2026-07-28",
		Finished: "",
		Status:   "playing",
		Platform: "PC",
		Rating:   "",
		Notes:    "try NG+",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(pf.Playthroughs) != 2 {
		t.Fatalf("graduate must not add or remove entries, got %d", len(pf.Playthroughs))
	}
	e := pf.Playthroughs[1]
	if e.Status != "playing" || e.Started != "2026-07-28" || e.Platform != "PC" || e.Notes != "try NG+" {
		t.Errorf("unexpected entry after graduate: %+v", e)
	}

	out := saveAndReload(t, pf)
	if out.Playthroughs[1].Status != "playing" || out.Playthroughs[1].Started != "2026-07-28" {
		t.Errorf("graduated fields did not survive a round trip: %+v", out.Playthroughs[1])
	}
	// The original "finished" entry at index 0 must be untouched — graduate
	// only ever mutates the one index it's given.
	if out.Playthroughs[0].Status != "finished" || out.Playthroughs[0].Started != "2021-05-01" {
		t.Errorf("graduate must not disturb other entries: %+v", out.Playthroughs[0])
	}
}

func TestGraduatePlannedPlaythrough_RejectsNonPlannedStatus(t *testing.T) {
	pf := loadFixture(t, `playthroughs:
  - status: finished
    started: "2021-05-01"
    finished: "2021-05-15"
`)
	if err := pf.GraduatePlannedPlaythrough(0, PlaythroughFields{Started: "2026-07-28", Status: "playing"}); err == nil {
		t.Fatal("expected an error — index 0 is not a planned placeholder")
	}
}

func TestGraduatePlannedPlaythrough_RejectsAlreadyStartedPlanned(t *testing.T) {
	// An entry can't be status "planned" with a Started date under normal
	// use, but the guard should hold regardless of how it got there — this
	// is the invariant that keeps graduate a pure addition for the loss-check.
	pf := loadFixture(t, `playthroughs:
  - status: planned
    started: "2026-01-01"
`)
	if err := pf.GraduatePlannedPlaythrough(0, PlaythroughFields{Started: "2026-07-28", Status: "playing"}); err == nil {
		t.Fatal("expected an error — a planned entry with a Started date is not a bare placeholder")
	}
}

func TestGraduatePlannedPlaythrough_RejectsOutOfRangeIndex(t *testing.T) {
	pf := loadFixture(t, `playthroughs:
  - status: planned
`)
	if err := pf.GraduatePlannedPlaythrough(5, PlaythroughFields{Started: "2026-07-28"}); err == nil {
		t.Fatal("expected an error for an out-of-range index")
	}
}

func TestEditPlanned_UpdatesPlatformAndNotes(t *testing.T) {
	pf := loadFixture(t, `playthroughs:
  - status: planned
    platform: PC
    notes: try NG+
`)
	if err := pf.EditPlanned(0, "Switch", "", "try the DLC instead"); err != nil {
		t.Fatal(err)
	}
	e := pf.Playthroughs[0]
	if e.Platform != "Switch" || e.Notes != "try the DLC instead" || e.Status != "planned" {
		t.Errorf("unexpected entry after edit: %+v", e)
	}
}

func TestEditPlanned_RejectsNonPlannedStatus(t *testing.T) {
	pf := loadFixture(t, `playthroughs:
  - status: finished
    started: "2021-05-01"
`)
	if err := pf.EditPlanned(0, "PC", "", "notes"); err == nil {
		t.Fatal("expected an error — index 0 is not a planned placeholder")
	}
}

func TestEffectivePlatformTreatsBlankAsTheGames(t *testing.T) {
	for _, tc := range []struct {
		entry, game, want string
	}{
		{"", "PC", "PC"},     // pre-dates the field: same platform as its game
		{"  ", "PC", "PC"},   // whitespace is not a platform
		{"PS4", "PC", "PS4"}, // an explicit platform wins
		{"PC", "PC", "PC"},   // spelled out, but still the game's
	} {
		if got := EffectivePlatform(tc.entry, tc.game); got != tc.want {
			t.Errorf("effectivePlatform(%q, %q) = %q, want %q", tc.entry, tc.game, got, tc.want)
		}
	}
}
