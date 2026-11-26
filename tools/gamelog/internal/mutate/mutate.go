// Package mutate is the write/loss-check layer for playthrough and
// front-matter edits.
package mutate

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

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

func SameGameInfo(a, b model.FrontMatter) bool {
	return a.Title == b.Title && a.Platform == b.Platform && a.Status == b.Status &&
		a.Draft == b.Draft &&
		a.RatingString() == b.RatingString() && slices.Equal(a.Subgames, b.Subgames) &&
		slices.Equal(a.Tags, b.Tags)
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
	return doc.Save()
}

// WriteEntry writes a playthroughs file after the pre-write loss check, then
// syncs the game's derived status. The caller (a submitted web form) is
// already its own confirmation.
func WriteEntry(pf *model.PlaythroughsFile, allowedRemovals ...string) error {
	if err := VerifyNoLoss(pf, allowedRemovals); err != nil {
		return err
	}
	if err := pf.Save(); err != nil {
		return err
	}
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

// verifyNoLoss compares the pending rewrite against the entries as they were
// read. The file is now encoded whole rather than patched, so the failure
// mode is a field the struct doesn't model being dropped on the way through —
// `Extra` is what catches those, and this is what proves it did.
//
// A flat started/finished pair typed by hand is moved into a session when the
// file is loaded (pf.ConvertedMoves). Such a flat path may vanish, but only if
// the session path now holding its value is in the rewrite.
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
	allowed := append([]string(nil), allowedRemovals...)
	for from, to := range pf.ConvertedMoves() {
		if _, kept := newFields[to]; kept {
			allowed = append(allowed, from)
		}
	}
	return model.CheckNoFieldLoss(oldFields, newFields, allowed)
}
