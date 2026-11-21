// Package mutate is the confirm/write/loss-check layer for playthrough and
// front-matter edits, shared by the local web server and the achievements
// command's own-session nudge (the only remaining CLI caller of the
// confirm-prompting path).
package mutate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.dalton.dog/gamelog/internal/forms"
	"go.dalton.dog/gamelog/internal/model"
)

// hasPlannedFor reports whether playthroughs already has a planned-replay
// placeholder for the given effective platform and subgame. Two different
// subgames each wanting their own planned replay on the same platform are
// two real intents, not fragmentation of one bookmark — so subgame must also
// match, not just platform.
func HasPlannedFor(playthroughs []model.PlaythroughEntry, entryPlatform, gamePlatform, subgame string) bool {
	want := model.EffectivePlatform(entryPlatform, gamePlatform)
	for _, e := range playthroughs {
		if e.Status == "planned" && model.EffectivePlatform(e.Platform, gamePlatform) == want && e.Subgame == subgame {
			return true
		}
	}
	return false
}

// OneShotConflict reports the existing entry that already covers a one-shot
// status (endless/multiplayer) on the given platform and subgame, if any —
// that's the case the web UI's handleNewPlaythrough refuses. None of the one-shot statuses has
// a save file or finish line, so a *second* entry of the same one-shot
// status on the same platform *and subgame* would be fragmentation, not a
// distinct mode; a differently-statused entry (a finished campaign alongside
// an endless sandbox mode, say) is a real second mode and is allowed to
// coexist. A second platform is never a conflict either way — saves don't
// cross consoles, so that's a genuinely separate record. Likewise a second
// subgame: two compilation entries each with their own endless mode on the
// same platform are two real modes, not fragmentation of one.
//
// Every entry carries its own status. Entries on endless/multiplayer games
// that once said "playing" and leaned on the game's status were rewritten on
// 2026-11-21, so nothing here consults the game's status any more.
func OneShotConflict(playthroughs []model.PlaythroughEntry, status, entryPlatform, gamePlatform, subgame string) *model.PlaythroughEntry {
	if !forms.IsOneShot(status) {
		return nil
	}
	want := model.EffectivePlatform(entryPlatform, gamePlatform)
	for i, e := range playthroughs {
		if e.Status == status && model.EffectivePlatform(e.Platform, gamePlatform) == want && e.Subgame == subgame {
			return &playthroughs[i]
		}
	}
	return nil
}

func SameGameInfo(a, b model.FrontMatter) bool {
	return a.Title == b.Title && a.Platform == b.Platform && a.Status == b.Status &&
		a.Draft == b.Draft &&
		a.RatingString() == b.RatingString() && sameSubgames(a.Subgames, b.Subgames)
}

func sameSubgames(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// WriteFrontMatter previews nothing and prompts nothing — the caller (a
// submitted web form) is already its own confirmation. It writes only the
// game's front matter: status flows the other way now, from runs to the game
// (SyncGameStatus), so a front-matter write never touches playthroughs.yaml.
func WriteFrontMatter(doc *model.Doc, allowedRemovals ...string) error {
	fm, err := doc.EncodeFM()
	if err != nil {
		return err
	}

	oldFields, err := model.CollectYAMLFields(doc.FMRaw())
	if err != nil {
		return err
	}
	newFields, err := model.CollectYAMLFields(fm)
	if err != nil {
		return fmt.Errorf("refusing to write %s — the result would not parse: %w", doc.Path, err)
	}
	if err := model.CheckNoFieldLoss(oldFields, newFields, allowedRemovals); err != nil {
		return err
	}
	if err := doc.Save(); err != nil {
		return err
	}
	fmt.Printf("Saved %s\n", doc.Path)
	return nil
}

// addSessionAndWrite logs a session against an existing playthrough and
// writes it. hadSessions must reflect the entry's state *before* this call —
// converting a flat started/finished pair into a sessions list is this
// call's own doing, not data loss, so the check needs to know whether that
// conversion is about to happen.
func AddSessionAndWrite(pf *model.PlaythroughsFile, idx int, hadSessions bool, started, finished, title string) error {
	if err := pf.AddSession(idx, started, finished, title); err != nil {
		return err
	}

	// Converting to sessions moves the flat date pair into the list rather
	// than dropping it, so those two paths are expected to disappear.
	var allowed []string
	if !hadSessions {
		allowed = []string{
			fmt.Sprintf("playthroughs[%d].started", idx),
			fmt.Sprintf("playthroughs[%d].finished", idx),
		}
	}
	return ConfirmAndWrite(pf, idx, allowed...)
}

// confirmAndWrite previews the affected entry, and on confirmation proves the
// rewrite loses nothing before letting it reach disk. allowedRemovals names
// the paths this particular operation means to drop; every other field
// present beforehand must still be there afterwards.
//
// The check compares against the bytes actually on disk, not against the
// in-memory entries, which the operation has already mutated in place.
func ConfirmAndWrite(pf *model.PlaythroughsFile, focus int, allowedRemovals ...string) error {
	preview, err := PreviewEntry(pf, focus)
	if err != nil {
		return err
	}
	ok, err := forms.ConfirmWrite(pf.Path, preview)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println("Discarded.")
		return nil
	}

	return WriteEntry(pf, allowedRemovals...)
}

// writeEntry is confirmAndWrite's no-prompt tail: verify-no-loss, save,
// report. Split out so a caller that has already gotten its confirmation some
// other way — a submitted web form, say — can reach the same safety net
// without going through a terminal prompt.
func WriteEntry(pf *model.PlaythroughsFile, allowedRemovals ...string) error {
	if err := VerifyNoLoss(pf, allowedRemovals); err != nil {
		return err
	}
	if err := pf.Save(); err != nil {
		return err
	}
	fmt.Printf("Saved %s\n", pf.Path)
	_, err := SyncGameStatus(pf)
	return err
}

// WriteSplit previews and prompts nothing — the caller (a submitted web
// form) is already its own confirmation. See WriteEntry.
func WriteSplit(pf *model.PlaythroughsFile, allowedRemovals ...string) error {
	if err := VerifyNoLoss(pf, allowedRemovals); err != nil {
		return err
	}
	if err := pf.Save(); err != nil {
		return err
	}
	fmt.Printf("Saved %s\n", pf.Path)
	_, err := SyncGameStatus(pf)
	return err
}

// SyncGameStatus writes the status a game's runs imply into its front matter
// (model.DeriveGameStatus), after a playthrough write. It's a no-op for a
// hand-set status, when the status already matches, and when the game has no
// _index.md (a playthroughs file being written in isolation, as tests do).
// Only the status field is touched, through the same field-loss check as
// every other front-matter write.
func SyncGameStatus(pf *model.PlaythroughsFile) (bool, error) {
	path := filepath.Join(filepath.Dir(pf.Path), "_index.md")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false, nil
	}
	doc, err := model.LoadDoc(path)
	if err != nil {
		return false, err
	}
	status, ok := model.DeriveGameStatus(doc.FM.Status, pf.Views())
	if !ok || status == doc.FM.Status {
		return false, nil
	}
	doc.FM.Status = status
	return true, WriteFrontMatter(doc)
}

// previewEntry renders just the entry being changed. Showing the whole file
// would bury a one-line edit in a game with a dozen playthroughs.
func PreviewEntry(pf *model.PlaythroughsFile, focus int) ([]string, error) {
	if focus < 0 || focus >= len(pf.Playthroughs) {
		return nil, fmt.Errorf("no playthrough %d to preview", focus+1)
	}
	one := &model.PlaythroughsFile{Playthroughs: pf.Playthroughs[focus : focus+1]}
	out, err := one.Encode()
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimRight(string(out), "\n"), "\n"), nil
}

// verifyNoLoss compares the pending rewrite against the entries as they were
// read. The file is now encoded whole rather than patched, so the failure
// mode is a field the struct doesn't model being dropped on the way through —
// `Extra` is what catches those, and this is what proves it did.
func VerifyNoLoss(pf *model.PlaythroughsFile, allowedRemovals []string) error {
	newBytes, err := pf.Encode()
	if err != nil {
		return err
	}
	oldFields, err := model.CollectYAMLFields(pf.Raw())
	if err != nil {
		return err
	}
	newFields, err := model.CollectYAMLFields(newBytes)
	if err != nil {
		return fmt.Errorf("refusing to write %s — the result would not parse: %w", pf.Path, err)
	}
	return model.CheckNoFieldLoss(oldFields, newFields, allowedRemovals)
}
