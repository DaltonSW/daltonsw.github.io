package steam

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// steamStub points the Steam endpoints at a local server for the duration of
// a test. handler receives the request path so one stub can serve several
// endpoints.
func steamStub(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	oldAch, oldOwned, oldVanity, oldSchema := PlayerAchievementsURL, OwnedGamesURL, ResolveVanityURL, SchemaForGameURL
	PlayerAchievementsURL = srv.URL + "/achievements"
	OwnedGamesURL = srv.URL + "/owned"
	ResolveVanityURL = srv.URL + "/vanity"
	SchemaForGameURL = srv.URL + "/schema"
	t.Cleanup(func() {
		PlayerAchievementsURL, OwnedGamesURL, ResolveVanityURL, SchemaForGameURL = oldAch, oldOwned, oldVanity, oldSchema
	})
}

const testSteamID64 = "76561198066238038"

// Steam reports "this game has no achievements" as HTTP 400 with a useful
// JSON body. That must surface as a readable skip, not a generic HTTP error.
func TestGetPlayerAchievements_NoStatsIsNotAnError(t *testing.T) {
	steamStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"playerstats":{"error":"Requested app has no stats","success":false}}`))
	})

	c := &SteamClient{APIKey: "k", SteamID: testSteamID64}
	got, err := c.GetPlayerAchievements(context.Background(), "365670")
	if err != nil {
		t.Fatalf("expected no hard error, got %v", err)
	}
	if got.Success {
		t.Error("expected Success=false")
	}
	if got.Error != "Requested app has no stats" {
		t.Errorf("Error = %q, want the API's message", got.Error)
	}
}

// A profile that isn't public comes back as 403, likewise with a real reason.
func TestGetPlayerAchievements_PrivateProfileIsNotAnError(t *testing.T) {
	steamStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"playerstats":{"error":"Profile is not public","success":false}}`))
	})

	c := &SteamClient{APIKey: "k", SteamID: testSteamID64}
	got, err := c.GetPlayerAchievements(context.Background(), "1237970")
	if err != nil {
		t.Fatalf("expected no hard error, got %v", err)
	}
	if got.Error != "Profile is not public" {
		t.Errorf("Error = %q, want the API's message", got.Error)
	}
}

// A status we have no contract for is still a genuine failure.
func TestGetPlayerAchievements_ServerErrorStillFails(t *testing.T) {
	steamStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`nope`))
	})

	c := &SteamClient{APIKey: "k", SteamID: testSteamID64}
	if _, err := c.GetPlayerAchievements(context.Background(), "1"); err == nil {
		t.Fatal("expected an error for a 500")
	}
}

// Steam rejects a vanity name with a non-JSON body, so it must be exchanged
// for a SteamID64 before any other call.
func TestResolveSteamID_ResolvesVanityNameOnce(t *testing.T) {
	var vanityCalls int
	steamStub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/vanity":
			vanityCalls++
			w.Write([]byte(`{"response":{"steamid":"` + testSteamID64 + `","success":1}}`))
		case "/achievements":
			if got := r.URL.Query().Get("steamid"); got != testSteamID64 {
				t.Errorf("achievements called with steamid %q, want the resolved ID", got)
			}
			w.Write([]byte(`{"playerstats":{"success":true,"achievements":[]}}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})

	c := &SteamClient{APIKey: "k", SteamID: "daltonsw"}
	for range 2 {
		if _, err := c.GetPlayerAchievements(context.Background(), "1"); err != nil {
			t.Fatal(err)
		}
	}
	if vanityCalls != 1 {
		t.Errorf("vanity resolved %d times, want 1 (result should be cached)", vanityCalls)
	}
}

// A 17-digit ID is already a SteamID64 and must not trigger a lookup.
func TestResolveSteamID_SkipsLookupForSteamID64(t *testing.T) {
	steamStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/vanity" {
			t.Error("should not resolve a value that is already a SteamID64")
		}
		w.Write([]byte(`{"playerstats":{"success":true,"achievements":[]}}`))
	})

	c := &SteamClient{APIKey: "k", SteamID: testSteamID64}
	if _, err := c.GetPlayerAchievements(context.Background(), "1"); err != nil {
		t.Fatal(err)
	}
}

func TestResolveSteamID_UnknownVanityName(t *testing.T) {
	steamStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"response":{"success":42,"message":"No match"}}`))
	})

	c := &SteamClient{APIKey: "k", SteamID: "nobody-here"}
	_, err := c.GetPlayerAchievements(context.Background(), "1")
	if err == nil {
		t.Fatal("expected an error for an unresolvable vanity name")
	}
	if !strings.Contains(err.Error(), "nobody-here") {
		t.Errorf("error should name the failing input, got %v", err)
	}
}

// GetSchemaForGame is the only Steam endpoint that carries icon URLs —
// GetPlayerAchievements doesn't, even with a language set.
func TestGetSchemaForGame_ParsesIcons(t *testing.T) {
	steamStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"game":{"gameName":"Test Game","availableGameStats":{"achievements":[
		  {"name":"A","displayName":"Escaped","icon":"https://cdn.example/a.jpg","icongray":"https://cdn.example/a-gray.jpg"}
		]}}}`))
	})

	c := &SteamClient{APIKey: "k"}
	got, err := c.GetSchemaForGame(context.Background(), "1145360")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].APIName != "A" || got[0].Icon != "https://cdn.example/a.jpg" {
		t.Errorf("schema not parsed as expected: %+v", got)
	}
}

// GetOwnedGames omits free-to-play titles unless explicitly asked.
func TestGetOwnedGamePlaytime_RequestsFreeGames(t *testing.T) {
	steamStub(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("include_played_free_games"); got != "1" {
			t.Errorf("include_played_free_games = %q, want 1", got)
		}
		w.Write([]byte(`{"response":{"games":[{"appid":440,"playtime_forever":120}]}}`))
	})

	c := &SteamClient{APIKey: "k", SteamID: testSteamID64}
	minutes, found, err := c.GetOwnedGamePlaytime(context.Background(), "440")
	if err != nil || !found || minutes != 120 {
		t.Fatalf("got (%d, %v, %v), want (120, true, nil)", minutes, found, err)
	}
}

func TestSteamAchievements_DecodesSampleResponse(t *testing.T) {
	raw, err := os.ReadFile("testdata/steam_achievements_sample.json")
	if err != nil {
		t.Fatal(err)
	}
	var parsed steamPlayerAchievementsResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("decoding sample: %v", err)
	}
	if !parsed.PlayerStats.Success {
		t.Fatal("expected success=true in sample")
	}
	if len(parsed.PlayerStats.Achievements) != 3 {
		t.Fatalf("expected 3 achievements, got %d", len(parsed.PlayerStats.Achievements))
	}
	got := SteamSuggestRange(parsed.PlayerStats.Achievements)
	if !got.OK || got.UnlockedCount != 2 || got.TotalCount != 3 {
		t.Fatalf("unexpected suggestion from sample: %+v", got)
	}
}

func TestSteamSuggestRange_AllLocked(t *testing.T) {
	got := SteamSuggestRange([]SteamAchievement{
		{APIName: "a", Achieved: 0, UnlockTime: 0},
		{APIName: "b", Achieved: 0, UnlockTime: 0},
	})
	if got.OK {
		t.Fatal("expected OK=false when nothing is unlocked")
	}
	if got.TotalCount != 2 || got.UnlockedCount != 0 {
		t.Fatalf("unexpected counts: %+v", got)
	}
}

func TestSteamSuggestRange_MixedLockedUnlocked(t *testing.T) {
	achievements := []SteamAchievement{
		{APIName: "a", Achieved: 1, UnlockTime: 1735776000}, // 2025-01-02
		{APIName: "b", Achieved: 0, UnlockTime: 0},
		{APIName: "c", Achieved: 1, UnlockTime: 1738454400}, // 2025-02-02
	}
	got := SteamSuggestRange(achievements)
	if !got.OK {
		t.Fatal("expected OK=true")
	}
	if got.UnlockedCount != 2 || got.TotalCount != 3 {
		t.Fatalf("unexpected counts: %+v", got)
	}
	if !got.Started.Equal(time.Unix(1735776000, 0)) {
		t.Fatalf("unexpected started: %v", got.Started)
	}
	if !got.Finished.Equal(time.Unix(1738454400, 0)) {
		t.Fatalf("unexpected finished: %v", got.Finished)
	}
}

func TestSteamSuggestRange_AchievedButZeroUnlockTimeIgnored(t *testing.T) {
	// Defensive: Achieved==1 with UnlockTime==0 shouldn't happen per Steam's
	// docs, but must not be treated as a real signal if it does.
	got := SteamSuggestRange([]SteamAchievement{
		{APIName: "a", Achieved: 1, UnlockTime: 0},
	})
	if got.OK {
		t.Fatal("expected OK=false when the only 'unlocked' entry has unlocktime 0")
	}
}

func TestFetchSteamRecord_CapturesPlaytimeBreakdown(t *testing.T) {
	steamStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("l") == "" {
			t.Error("expected a language parameter so names are returned")
		}
		w.Write([]byte(`{"playerstats":{"success":true,"achievements":[
		  {"apiname":"A","achieved":1,"unlocktime":1603510789,"name":"Escaped","description":"d"},
		  {"apiname":"B","achieved":0,"unlocktime":0,"name":"Locked"}
		]}}`))
	})

	owned := &SteamOwnedGame{
		AppID: 1145360, PlaytimeMins: 4400, LastPlayed: 1671577593,
		PlaytimeWindows: 4000, PlaytimeDeck: 400, PlaytimeMac: 0,
	}
	client := &SteamClient{APIKey: "k", SteamID: testSteamID64}
	rec, err := FetchRecord(context.Background(), client, "1145360", owned)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Unlocked != 1 || rec.Total != 2 {
		t.Errorf("counts wrong: %d/%d", rec.Unlocked, rec.Total)
	}
	if rec.PlaytimeMins != 4400 {
		t.Errorf("playtime not captured: %d", rec.PlaytimeMins)
	}
	if rec.Playtime["windows"] != 4000 || rec.Playtime["deck"] != 400 {
		t.Errorf("per-device playtime not captured: %v", rec.Playtime)
	}
	if _, ok := rec.Playtime["mac"]; ok {
		t.Error("zero-playtime devices should be omitted")
	}
	if rec.LastPlayed == "" {
		t.Error("last played not captured")
	}
}

// Icons come from GetSchemaForGame, not GetPlayerAchievements, and must be
// joined onto the right achievement by apiname.
func TestFetchSteamRecord_AttachesIconsFromSchema(t *testing.T) {
	steamStub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/achievements":
			w.Write([]byte(`{"playerstats":{"success":true,"achievements":[
			  {"apiname":"A","achieved":1,"unlocktime":1603510789,"name":"Escaped","description":"d"}
			]}}`))
		case "/schema":
			w.Write([]byte(`{"game":{"availableGameStats":{"achievements":[
			  {"name":"A","displayName":"Escaped","icon":"https://cdn.example/a.jpg","icongray":"https://cdn.example/a-gray.jpg"}
			]}}}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})

	client := &SteamClient{APIKey: "k", SteamID: testSteamID64}
	rec, err := FetchRecord(context.Background(), client, "1145360", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Achievements) != 1 || rec.Achievements[0].Icon != "https://cdn.example/a.jpg" {
		t.Errorf("icon not attached: %+v", rec.Achievements)
	}
}

// A schema fetch failing (a delisted game, a transient error) must not fail
// the whole record — the unlock data is what matters, icons are best-effort.
func TestFetchSteamRecord_SchemaFailureIsNotFatal(t *testing.T) {
	steamStub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/achievements":
			w.Write([]byte(`{"playerstats":{"success":true,"achievements":[
			  {"apiname":"A","achieved":1,"unlocktime":1603510789,"name":"Escaped","description":"d"}
			]}}`))
		case "/schema":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})

	client := &SteamClient{APIKey: "k", SteamID: testSteamID64}
	rec, err := FetchRecord(context.Background(), client, "1145360", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Achievements) != 1 || rec.Achievements[0].Icon != "" {
		t.Errorf("expected unlock data intact with no icon: %+v", rec.Achievements)
	}
}

// A failed fetch must produce a record that records the failure rather than
// one that looks like an empty success.
func TestFetchSteamRecord_FailureIsMarked(t *testing.T) {
	steamStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"playerstats":{"error":"Requested app has no stats","success":false}}`))
	})

	client := &SteamClient{APIKey: "k", SteamID: testSteamID64}
	rec, err := FetchRecord(context.Background(), client, "365670", nil)
	if err != nil {
		t.Fatalf("expected no hard error, got %v", err)
	}
	if rec.LastError == "" {
		t.Error("failure should be recorded so a merge knows not to trust it")
	}
	if rec.Fetched != "" {
		t.Error("Fetched must only be set on a successful fetch")
	}
}
