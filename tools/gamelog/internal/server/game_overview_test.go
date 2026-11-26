package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormatStatValue(t *testing.T) {
	for in, want := range map[float64]string{0: "0", 7: "7", 1234567: "1,234,567", -1500: "-1,500", 2.5: "2.50", 0.00123: "0.00123"} {
		if got := formatStatValue(in); got != want {
			t.Errorf("formatStatValue(%v) = %q, want %q", in, got, want)
		}
	}
}

// the game page shows completion %, rarity per achievement, and the stats projection
func TestGameDetail_ShowsPercentagesAndStats(t *testing.T) {
	s := listsFixture(t)
	dir := filepath.Join(s.gamesDir, "hollow-knight")
	os.WriteFile(filepath.Join(dir, "achievement-summary.yaml"), []byte("unlocked: 1\ntotal: 4\nplaytime_mins: 90\nlast_played: \"2024-08-11\"\n"+
		"earned:\n    - id: \"367520\"\n      key: STEEL_SOUL\n      name: Steel Soul\n      date: \"2024-08-11T10:00:00-05:00\"\n      provider: steam\n      rarity: 3.25\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "stats-summary.yaml"), []byte("providers:\n    - provider: steam\n      platform: PC\n      fetched: \"2024-08-12\"\n      stats:\n"+
		"        - key: crops_shipped\n          value: 12345\n"), 0o644)

	tmpl, err := parsePage(pageSpecs["game_detail"].name, pageSpecs["game_detail"].partials...)
	if err != nil {
		t.Fatal(err)
	}
	doc, pf, err := s.loadGame("hollow-knight")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	req := httptest.NewRequest(http.MethodGet, "/games/hollow-knight", nil)
	if err := tmpl.ExecuteTemplate(&buf, "layout", s.buildGameDetail(req, "hollow-knight", doc, pf)); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"25.0%", "1/4 achievements", "1.5h", "3.25%", "Rarest earned", "crops shipped", "12,345", "captured 2024-08-12"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("game page missing %q", want)
		}
	}
}
