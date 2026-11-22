package steam

import (
	"context"
	"net/http"
	"os"
	"testing"
)

// Keep tests off the developer's real Steam cache.
func TestMain(m *testing.M) {
	LocalStatsDir = "/nonexistent-steam-stats"
	os.Exit(m.Run())
}

// Fixture is a real UserGameStatsSchema file (appid 1392820), not hand-built.
func TestLoadLocalSchema_RealFile(t *testing.T) {
	LocalStatsDir = "testdata/stats"
	t.Cleanup(func() { LocalStatsDir = "/nonexistent-steam-stats" })

	got := LoadLocalSchema("1392820")
	a, ok := got["Achievement02_Milk1"]
	if !ok || a.Name != "Milk bought" || a.Description != "..." {
		t.Errorf("Achievement02_Milk1 = %+v (found %v)", a, ok)
	}
	if len(got) != 3 {
		t.Errorf("got %d achievements, want 3", len(got))
	}
	if LoadLocalSchema("999") != nil {
		t.Error("missing file should give nil")
	}
}

func TestParseLocalSchema_TruncatedIsNil(t *testing.T) {
	b, err := os.ReadFile("testdata/stats/UserGameStatsSchema_1392820.bin")
	if err != nil {
		t.Fatal(err)
	}
	// Cut mid-string: must not panic, and yields nothing usable.
	if got := parseLocalSchema(b[:len(b)/2]); len(got) > 3 {
		t.Errorf("unexpected result from truncated file: %v", got)
	}
}

const hiddenAchievements = `{"playerstats":{"success":true,"achievements":[
  {"apiname":"Achievement02_Milk1","achieved":0,"unlocktime":0,"name":"Achievement02_Milk1","description":""},
  {"apiname":"Other","achieved":0,"unlocktime":0,"name":"Other","description":"api text"}
]}}`

func hiddenStub(t *testing.T) {
	steamStub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/achievements":
			w.Write([]byte(hiddenAchievements))
		case "/schema":
			w.Write([]byte(`{"game":{"availableGameStats":{"achievements":[
			  {"name":"Achievement02_Milk1","hidden":1},{"name":"Other","hidden":0}]}}}`))
		}
	})
}

func TestFetchRecord_FillsHiddenFromLocalSchema(t *testing.T) {
	hiddenStub(t)
	LocalStatsDir = "testdata/stats"
	t.Cleanup(func() { LocalStatsDir = "/nonexistent-steam-stats" })

	rec, err := FetchRecord(context.Background(), &SteamClient{APIKey: "k", SteamID: testSteamID64}, "1392820", nil)
	if err != nil {
		t.Fatal(err)
	}
	if a := rec.Achievements[0]; a.Name != "Milk bought" || a.Description != "..." {
		t.Errorf("hidden achievement not filled: %+v", a)
	}
	if a := rec.Achievements[1]; a.Description != "api text" {
		t.Errorf("non-hidden achievement must be untouched: %+v", a)
	}
}

// No cache for the game: the record is exactly what the API gave.
func TestFetchRecord_NoLocalSchemaFallsBack(t *testing.T) {
	hiddenStub(t)

	rec, err := FetchRecord(context.Background(), &SteamClient{APIKey: "k", SteamID: testSteamID64}, "1392820", nil)
	if err != nil {
		t.Fatal(err)
	}
	if a := rec.Achievements[0]; a.Description != "" || a.Name != "Achievement02_Milk1" {
		t.Errorf("expected unchanged API values: %+v", a)
	}
}
