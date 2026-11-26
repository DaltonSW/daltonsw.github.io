package mutate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.dalton.dog/gamelog/internal/model"
)

// Tags on a game and on a run survive whole-file rewrites, and shortening
// either list is only allowed when the dropped tail is declared.
func TestTags_RoundTripAndDeclaredRemoval(t *testing.T) {
	dir := writeGame(t, "endless", "playthroughs:\n  - status: endless\n    tags: [multiplayer, couch]\n    sessions:\n      - started: \"2024-01-01\"\n        finished: \"2024-02-01\"\n")
	indexPath := filepath.Join(dir, "_index.md")
	raw, _ := os.ReadFile(indexPath)
	if err := os.WriteFile(indexPath, []byte(strings.Replace(string(raw), "status: endless\n", "status: endless\ntags: [multiplayer]\n", 1)), 0o644); err != nil {
		t.Fatal(err)
	}

	// Run tags: an untouched rewrite keeps them.
	pf, err := model.LoadPlaythroughs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := pf.Views()[0].Tags; len(got) != 2 || got[0] != "multiplayer" || got[1] != "couch" {
		t.Fatalf("run tags = %v", got)
	}
	if err := WriteEntry(pf); err != nil {
		t.Fatalf("an untouched rewrite must keep the run's tags: %v", err)
	}

	// Dropping one undeclared is refused; declared, it goes through.
	pf, _ = model.LoadPlaythroughs(dir)
	pf.Playthroughs[0].Tags = []string{"multiplayer"}
	if err := VerifyNoLoss(pf, nil); err == nil {
		t.Fatal("an undeclared dropped run tag must be refused")
	}
	if err := WriteEntry(pf, model.ShrunkListPaths("playthroughs[0].tags", 2, 1)...); err != nil {
		t.Fatalf("a declared dropped run tag must be allowed: %v", err)
	}

	// Game tags: same rules through the front-matter write.
	doc, err := model.LoadDoc(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.FM.Tags) != 1 || doc.FM.Tags[0] != "multiplayer" {
		t.Fatalf("game tags = %v", doc.FM.Tags)
	}
	doc.FM.Tags = nil
	if err := WriteFrontMatter(doc); err == nil {
		t.Fatal("an undeclared cleared game tag must be refused")
	}
	if err := WriteFrontMatter(doc, model.ShrunkListPaths("tags", 1, 0)...); err != nil {
		t.Fatalf("a declared cleared game tag must be allowed: %v", err)
	}
	after, _ := os.ReadFile(indexPath)
	if strings.Contains(string(after), "tags") || !strings.HasSuffix(string(after), "\nBody stays.\n") {
		t.Errorf("tags key should be gone and the body untouched:\n%s", after)
	}
}
