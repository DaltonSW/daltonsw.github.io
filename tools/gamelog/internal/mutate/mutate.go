// Package mutate is the confirm/write/loss-check layer for playthrough and
// front-matter edits, shared by the terminal interactive flow, the local web
// server, and the commands that fold a provider refresh into a session log.
package mutate

import (
	"fmt"
	"strings"

	"go.dalton.dog/gamelog/internal/forms"
	"go.dalton.dog/gamelog/internal/model"
)

// hasPlannedFor reports whether playthroughs already has a planned-replay
// placeholder for the given effective platform.
func HasPlannedFor(playthroughs []model.PlaythroughEntry, entryPlatform, gamePlatform string) bool {
	want := model.EffectivePlatform(entryPlatform, gamePlatform)
	for _, e := range playthroughs {
		if e.Status == "planned" && model.EffectivePlatform(e.Platform, gamePlatform) == want {
			return true
		}
	}
	return false
}

// oneShotConflict reports the existing entry that already covers a one-shot
// status (ongoing/multiplayer) on the given platform, if any — that's the
// case doNewPlaythrough refuses. None of the one-shot statuses has a save
// file or finish line, so a *second* entry of the same one-shot status on
// the same platform would be fragmentation, not a distinct mode; a
// differently-statused entry (a finished campaign alongside an ongoing
// sandbox mode, say) is a real second mode and is allowed to coexist. A
// second platform is never a conflict either way — saves don't cross
// consoles, so that's a genuinely separate record.
//
// gameStatus resolves entries written before per-entry ongoing/multiplayer
// existed — see effectiveOneShotStatus.
func OneShotConflict(playthroughs []model.PlaythroughEntry, status, entryPlatform, gamePlatform, gameStatus string) *model.PlaythroughEntry {
	if !forms.IsOneShot(status) {
		return nil
	}
	want := model.EffectivePlatform(entryPlatform, gamePlatform)
	for i, e := range playthroughs {
		if EffectiveOneShotStatus(e.Status, gameStatus) == status && model.EffectivePlatform(e.Platform, gamePlatform) == want {
			return &playthroughs[i]
		}
	}
	return nil
}

// effectiveOneShotStatus resolves what one-shot status, if any, an existing
// entry really represents. Every ongoing/multiplayer/software game written
// before per-entry ongoing/multiplayer support kept its sole entry's own
// status as "playing" and relied entirely on the game's front-matter status
// for its real meaning (games-timeline.html's override still does this for
// display). Without this, a new literal "ongoing" entry wouldn't be seen as
// conflicting with that old-style "playing" entry, silently dropping the
// fragmentation guard for every game written the old way.
func EffectiveOneShotStatus(entryStatus, gameStatus string) string {
	if entryStatus == "playing" && forms.IsOneShot(gameStatus) {
		return gameStatus
	}
	return entryStatus
}

func SameGameInfo(a, b model.FrontMatter) bool {
	return a.Title == b.Title && a.Platform == b.Platform && a.Status == b.Status &&
		a.Started == b.Started && a.Finished == b.Finished && a.Draft == b.Draft &&
		a.RatingString() == b.RatingString()
}

// confirmAndWriteFrontMatter is confirmAndWrite's front-matter analog:
// preview, confirm, prove the rewrite loses nothing, then write. When pf is
// non-nil, it also syncs and writes the playthrough via SyncStatus under the
// same confirmation, so status and the timeline never drift apart again.
func ConfirmAndWriteFrontMatter(doc *model.Doc, pf *model.PlaythroughsFile, allowedRemovals ...string) error {
	fm, err := doc.EncodeFM()
	if err != nil {
		return err
	}
	preview := strings.Split(strings.TrimRight(string(fm), "\n"), "\n")

	syncing := pf != nil && pf.SyncStatus(doc.FM.Status, doc.FM.Finished)
	if syncing {
		entryPreview, err := PreviewEntry(pf, 0)
		if err != nil {
			return err
		}
		preview = append(preview, "", "playthroughs.yaml:")
		preview = append(preview, entryPreview...)
	}

	ok, err := forms.ConfirmWrite(doc.Path, preview)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println("Discarded.")
		return nil
	}

	return WriteFrontMatter(doc, pf, syncing, allowedRemovals...)
}

// writeFrontMatter is confirmAndWriteFrontMatter's no-prompt tail — see
// writeEntry. syncing must match what the caller already decided (whether
// pf's status was brought in line with doc.FM.Status), since that's what
// decides whether pf needs its own loss-check and save alongside doc's.
func WriteFrontMatter(doc *model.Doc, pf *model.PlaythroughsFile, syncing bool, allowedRemovals ...string) error {
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
	if syncing {
		if err := VerifyNoLoss(pf, nil); err != nil {
			return err
		}
	}

	if err := doc.Save(); err != nil {
		return err
	}
	fmt.Printf("Saved %s\n", doc.Path)

	if syncing {
		if err := pf.Save(); err != nil {
			return err
		}
		fmt.Printf("Saved %s\n", pf.Path)
	}
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
	return nil
}

// confirmAndWriteSplit is confirmAndWrite's two-entry analog: previews both
// the shrunk source entry and the new split-off entry, confirms once, then
// writes.
func ConfirmAndWriteSplit(pf *model.PlaythroughsFile, srcIdx, newIdx int, allowedRemovals ...string) error {
	srcPreview, err := PreviewEntry(pf, srcIdx)
	if err != nil {
		return err
	}
	newPreview, err := PreviewEntry(pf, newIdx)
	if err != nil {
		return err
	}
	preview := append(append(append([]string{}, srcPreview...), "", "new playthrough:"), newPreview...)

	ok, err := forms.ConfirmWrite(pf.Path, preview)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println("Discarded.")
		return nil
	}

	return WriteSplit(pf, allowedRemovals...)
}

// writeSplit is confirmAndWriteSplit's no-prompt tail — see writeEntry.
func WriteSplit(pf *model.PlaythroughsFile, allowedRemovals ...string) error {
	if err := VerifyNoLoss(pf, allowedRemovals); err != nil {
		return err
	}
	if err := pf.Save(); err != nil {
		return err
	}
	fmt.Printf("Saved %s\n", pf.Path)
	return nil
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
