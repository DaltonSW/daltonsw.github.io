package main

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
	if err := pf.AddSession(0, "2026-05-19", "2026-05-24"); err != nil {
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
	if err := pf.AddSession(0, "2026-05-19", ""); err != nil {
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
	if err := pf.AddSession(0, "2026-05-19", ""); err != nil {
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
	pf := loadFixture(t, `playthroughs:
  - started: 2026-01-04
    finished: 2026-02-11
    platform: Switch
    co_op_with: sam
    sessions:
      - started: 2026-01-04
        finished: 2026-01-09
        device: deck
`)
	if err := pf.UpdatePlaythrough(0, "2026-02-12", "finished", "8", "done"); err != nil {
		t.Fatal(err)
	}
	got := saveAndReload(t, pf).Playthroughs[0]

	if got.Extra["platform"] != "Switch" || got.Extra["co_op_with"] != "sam" {
		t.Errorf("entry-level unknown fields lost: %v", got.Extra)
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
	if err := pf.AddSession(0, "2026-05-19", ""); err == nil {
		t.Fatal("expected an error rather than a session with no origin")
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
	if err := pf.UpdatePlaythrough(0, "2026-04-09", "finished", "", ""); err != nil {
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
	before, err := collectYAMLFields([]byte(flatWithFieldsBetween))
	if err != nil {
		t.Fatal(err)
	}
	damaged, err := collectYAMLFields([]byte("playthroughs:\n  - started: 2026-01-04\n    finished: 2026-02-11\n"))
	if err != nil {
		t.Fatal(err)
	}

	err = checkNoFieldLoss(before, damaged, nil)
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
	if err := checkNoFieldLoss(before, damaged, allowed); err != nil {
		t.Errorf("declared removals should pass: %v", err)
	}
}

// A field that was already empty carries no data, so `omitempty` dropping it
// is not a loss — otherwise every blank `finished:` would trip the check.
func TestCheckNoFieldLossIgnoresEmptyValues(t *testing.T) {
	before, _ := collectYAMLFields([]byte("playthroughs:\n  - started: 2026-01-04\n    finished:\n"))
	after, _ := collectYAMLFields([]byte("playthroughs:\n  - started: 2026-01-04\n"))
	if err := checkNoFieldLoss(before, after, nil); err != nil {
		t.Errorf("dropping an already-empty field is not data loss: %v", err)
	}
}

// The conversion is the one operation that legitimately removes paths, and
// the allowlist it declares has to match what actually disappears.
func TestConversionDeclaresExactlyWhatItRemoves(t *testing.T) {
	pf := loadFixture(t, flatWithFieldsBetween)
	before, err := collectYAMLFields(pf.Raw())
	if err != nil {
		t.Fatal(err)
	}
	if err := pf.AddSession(0, "2026-05-19", ""); err != nil {
		t.Fatal(err)
	}
	encoded, err := pf.Encode()
	if err != nil {
		t.Fatal(err)
	}
	after, err := collectYAMLFields(encoded)
	if err != nil {
		t.Fatal(err)
	}

	allowed := []string{"playthroughs[0].started", "playthroughs[0].finished"}
	if err := checkNoFieldLoss(before, after, allowed); err != nil {
		t.Fatalf("conversion lost something it did not declare: %v", err)
	}
	// And the allowlist isn't hiding a wider problem: without it, only those
	// two paths should be reported.
	err = checkNoFieldLoss(before, after, nil)
	if err == nil {
		t.Fatal("expected the moved date pair to be reported when undeclared")
	}
	for _, unwanted := range []string{"status", "rating", "mood", "notes"} {
		if strings.Contains(err.Error(), unwanted) {
			t.Errorf("conversion should not touch %q, got: %v", unwanted, err)
		}
	}
}
