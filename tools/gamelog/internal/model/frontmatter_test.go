package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sampleIndexMD mirrors a real content/games/<slug>/_index.md: fields the
// tool models, fields it doesn't (cover, cascade), and hand-written prose
// below the closing delimiter.
const sampleIndexMD = `---
title: "Papers, Please"
platform: "PC"
retroachievements_id:
steam_appid: 239030
status: "playing"
started:
finished:
rating:
cover:
draft: true
cascade:
  params:
    games: ["papers-please"]
---

Some hand-written prose about this game.
It spans more than one line.
`

func writeIndexFixture(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "_index.md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// A front-matter edit is a splice, not a reconstruction — the markdown body
// must survive byte-for-byte, and cascade/cover (fields the struct doesn't
// model) must round-trip via Extra rather than vanish.
func TestDocSave_PreservesUnmodeledFieldsAndBody(t *testing.T) {
	path := writeIndexFixture(t, sampleIndexMD)
	doc, err := LoadDoc(path)
	if err != nil {
		t.Fatal(err)
	}

	doc.FM.Status = "finished"
	doc.FM.Draft = false
	if err := doc.Save(); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	wantBody := "Some hand-written prose about this game.\nIt spans more than one line.\n"
	if !strings.HasSuffix(string(raw), wantBody) {
		t.Errorf("markdown body was not preserved byte-for-byte, file ends with:\n%s", raw)
	}

	reloaded, err := LoadDoc(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.FM.Status != "finished" || reloaded.FM.Draft {
		t.Fatalf("edit did not persist: %+v", reloaded.FM)
	}
	cascade, ok := reloaded.FM.Extra["cascade"].(map[string]any)
	if !ok {
		t.Fatalf("cascade was not preserved through Extra: %+v", reloaded.FM.Extra)
	}
	if _, ok := cascade["params"]; !ok {
		t.Errorf("cascade.params was not preserved: %+v", cascade)
	}
}

// The loss-check backstops the front-matter write path exactly the way it
// already does for playthroughs.yaml: a rewrite that silently drops an
// unmodeled field must be caught before it reaches disk.
func TestFrontMatterLossCheckCatchesDroppedExtraField(t *testing.T) {
	path := writeIndexFixture(t, sampleIndexMD)
	doc, err := LoadDoc(path)
	if err != nil {
		t.Fatal(err)
	}

	oldFields, err := CollectYAMLFields(doc.fmRaw)
	if err != nil {
		t.Fatal(err)
	}

	delete(doc.FM.Extra, "cascade")
	newBytes, err := doc.EncodeFM()
	if err != nil {
		t.Fatal(err)
	}
	newFields, err := CollectYAMLFields(newBytes)
	if err != nil {
		t.Fatal(err)
	}

	err = CheckNoFieldLoss(oldFields, newFields, nil)
	if err == nil {
		t.Fatal("expected the dropped cascade field to be reported")
	}
	if !strings.Contains(err.Error(), "cascade") {
		t.Errorf("error should name the lost field, got: %v", err)
	}
}

// Front-matter scalar fields aren't omitempty, so a cleared key stays
// present (blank/null) rather than disappearing — never reported as a loss.
func TestFrontMatterClearingAnOptionalFieldIsNeverALoss(t *testing.T) {
	path := writeIndexFixture(t, strings.Replace(sampleIndexMD, "rating:", "rating: 7", 1))
	doc, err := LoadDoc(path)
	if err != nil {
		t.Fatal(err)
	}
	oldFields, err := CollectYAMLFields(doc.fmRaw)
	if err != nil {
		t.Fatal(err)
	}

	doc.FM.SetRating("")
	newBytes, err := doc.EncodeFM()
	if err != nil {
		t.Fatal(err)
	}
	newFields, err := CollectYAMLFields(newBytes)
	if err != nil {
		t.Fatal(err)
	}

	if err := CheckNoFieldLoss(oldFields, newFields, nil); err != nil {
		t.Errorf("clearing an optional field should never be reported as loss: %v", err)
	}
}

// The subset list is written by the tool but read like everything else in
// front matter, so it has to survive both forms a hand-edited file can use:
// a bare number (which YAML decodes as an int) and a quoted string.
func TestRASubsetIDs_AcceptsBareAndQuotedIDs(t *testing.T) {
	path := writeIndexFixture(t, `---
title: "Professor Layton and the Last Specter"
platform: "Nintendo DS"
retroachievements_id: 7601
retroachievements_subsets:
  - 25709
  - "25710"
status: "playing"
---

Prose.
`)
	doc, err := LoadDoc(path)
	if err != nil {
		t.Fatal(err)
	}
	got := doc.RASubsetIDs()
	if len(got) != 2 || got[0] != "25709" || got[1] != "25710" {
		t.Fatalf("RASubsetIDs() = %q, want [25709 25710]", got)
	}

	links := doc.ProviderLinks()
	if len(links) != 3 || !links[1].Subset || !links[2].Subset || links[0].Subset {
		t.Errorf("ProviderLinks() = %+v, want the base set then two subsets", links)
	}
}

// Attaching is offered from a page that re-derives its rows, so the same
// button is easy to click twice — the second time must be a no-op, not a
// duplicate link that would archive and count the set twice.
func TestAddRASubset_IsIdempotentAndRoundTrips(t *testing.T) {
	path := writeIndexFixture(t, sampleIndexMD)
	doc, err := LoadDoc(path)
	if err != nil {
		t.Fatal(err)
	}
	if !doc.AddRASubset("25709") {
		t.Fatal("first attach should report it added the id")
	}
	if doc.AddRASubset("25709") {
		t.Error("attaching the same subset twice must be a no-op")
	}
	if doc.AddRASubset("  ") {
		t.Error("a blank id must not be added")
	}
	if err := doc.Save(); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "retroachievements_subsets:") {
		t.Errorf("saved file lost the subset list:\n%s", raw)
	}
	// The hand-written prose below the front matter is spliced, never
	// reconstructed — same guarantee every other front-matter edit has.
	if !strings.Contains(string(raw), "Some hand-written prose about this game.") {
		t.Errorf("saved file lost the markdown body:\n%s", raw)
	}

	reloaded, err := LoadDoc(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.RASubsetIDs(); len(got) != 1 || got[0] != "25709" {
		t.Errorf("after a save/load round trip, RASubsetIDs() = %q, want [25709]", got)
	}
}
