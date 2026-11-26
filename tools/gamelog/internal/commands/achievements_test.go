package commands

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.dalton.dog/gamelog/internal/model"
	"go.dalton.dog/gamelog/internal/providers/retroachievements"
	"go.dalton.dog/gamelog/internal/providers/steam"
)

const testSteamID64 = "76561198066238038"

// steamStub points the Steam achievements endpoint at a local server for the
// duration of a test — the cross-package equivalent of steam package's own
// resetProviderCaches drops the shared clients and memoized lists before and
// after a test, so neither leaks between tests that reuse the same credentials.
func resetProviderCaches(t *testing.T) {
	t.Helper()
	reset := func() { ForgetScanCache(Credentials{}) }
	reset()
	t.Cleanup(reset)
}

// steamStub points the per-game achievements endpoint at handler, the library
// listing at an empty library (ownedStub replaces that with a game), and the
// schema and global-percentage endpoints at empty answers.
func steamStub(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	resetProviderCaches(t)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	owned := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"response":{"games":[]}}`))
	}))
	t.Cleanup(owned.Close)
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	}))
	t.Cleanup(empty.Close)

	// Steam answers the per-game stats call with a bare 400 for an app that has none.
	noUserStats := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{}`))
	}))
	t.Cleanup(noUserStats.Close)

	urls := []*string{&steam.PlayerAchievementsURL, &steam.OwnedGamesURL, &steam.SchemaForGameURL, &steam.GlobalPercentagesURL, &steam.UserStatsURL}
	saved := make([]string, len(urls))
	for i, u := range urls {
		saved[i] = *u
	}
	t.Cleanup(func() {
		for i, u := range urls {
			*u = saved[i]
		}
	})
	steam.PlayerAchievementsURL, steam.OwnedGamesURL = srv.URL, owned.URL
	steam.SchemaForGameURL, steam.GlobalPercentagesURL = empty.URL, empty.URL
	steam.UserStatsURL = noUserStats.URL
}

func unlocked(key, date string) model.ArchivedAchievement {
	return model.ArchivedAchievement{Key: key, Name: key, Unlocked: true, Date: date}
}

// The exact regression that motivated this: a private profile used to wipe
// the archive and report success.
func TestSaveAchievements_PrivateProfileKeepsExistingData(t *testing.T) {
	dir := t.TempDir()
	if _, err := model.SaveRecord(dir, model.ProviderSteam, "1145360", "Hades", &model.ProviderRecord{
		ID: "1145360", Total: 49, Fetched: "2026-07-01", Achievements: []model.ArchivedAchievement{
			unlocked("AchClearTartarus", "2020-10-23T22:39:49-05:00"),
			unlocked("AchWarGod", "2022-12-20T17:06:33-06:00"),
		},
	}); err != nil {
		t.Fatal(err)
	}

	steamStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"playerstats":{"error":"Profile is not public","success":false}}`))
	})
	creds := Credentials{SteamAPIKey: "k", SteamID: testSteamID64}
	if _, err := SaveAchievements(context.Background(), dir, "Hades", steamLink("1145360"), creds); err != nil {
		t.Fatalf("refresh errored: %v", err)
	}

	rec, err := model.LoadRecord(dir, model.ProviderSteam, "1145360")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Unlocked != 2 {
		t.Fatalf("DATA LOSS: 2 captured unlocks became %d", rec.Unlocked)
	}
	if rec.LastError == "" {
		t.Error("the failed attempt should be recorded")
	}
	if rec.Fetched != "2026-07-01" {
		t.Errorf("Fetched should not advance on a failed refresh, got %q", rec.Fetched)
	}
}

// steamLink is the common single-provider case in these tests.
func steamLink(id string) []model.ProviderLink {
	return []model.ProviderLink{{Provider: model.ProviderSteam, ID: id}}
}

// raStubProgress points the RetroAchievements per-game endpoint at a canned
// response for one test. saveAchievements builds its own client, so the URL is
// the only seam — same shape as steamStub above.
func raStubProgress(t *testing.T, body string) {
	t.Helper()
	resetProviderCaches(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	old := retroachievements.GameProgressURL
	retroachievements.GameProgressURL = srv.URL
	t.Cleanup(func() { retroachievements.GameProgressURL = old })

	// Pre-seed the user-level last-played memo so the fetch doesn't reach for
	// the live recently-played endpoint, which has no stub of its own.
	raRecentCache.Lock()
	raRecentCache.byUser["u"] = raRecentEntry{at: time.Now(), byID: map[string]*retroachievements.RARecentGame{}}
	raRecentCache.Unlock()
}

// ownedStub serves a one-game Steam library and counts achievement requests,
// so a test can tell whether SaveAchievements went to the per-game endpoint.
func ownedStub(t *testing.T, playtime int, lastPlayed int64) *int {
	t.Helper()
	calls := new(int)
	steamStub(t, func(w http.ResponseWriter, r *http.Request) {
		*calls++
		w.Write([]byte(`{"playerstats":{"success":true,"achievements":[{"apiname":"A","achieved":1,"unlocktime":1700000000}]}}`))
	})

	owned := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"response":{"games":[{"appid":1145360,"name":"Hades","playtime_forever":%d,"rtime_last_played":%d}]}}`, playtime, lastPlayed)
	}))
	t.Cleanup(owned.Close)
	old := steam.OwnedGamesURL
	steam.OwnedGamesURL = owned.URL
	t.Cleanup(func() { steam.OwnedGamesURL = old })
	return calls
}

func seedSteam(t *testing.T, dir string, rec model.ProviderRecord) {
	t.Helper()
	rec.ID = "1145360"
	if _, err := model.SaveRecord(dir, model.ProviderSteam, "1145360", "Hades", &rec); err != nil {
		t.Fatal(err)
	}
}

func TestSaveAchievements_SkipUnchangedSteam(t *testing.T) {
	dir := t.TempDir()
	const lp = int64(1700000000)
	day := time.Unix(lp, 0).In(model.SiteLocation).Format("2006-01-02")
	seedSteam(t, dir, model.ProviderRecord{Fetched: "2026-07-01", Total: 1, PlaytimeMins: 600, LastPlayed: day,
		Achievements: []model.ArchivedAchievement{unlocked("A", "2023-11-14")}})
	creds := Credentials{SteamAPIKey: "k", SteamID: testSteamID64}

	calls := ownedStub(t, 600, lp)
	saved, err := SaveAchievements(context.Background(), dir, "Hades", steamLink("1145360"), creds, SkipUnchanged)
	if err != nil {
		t.Fatal(err)
	}
	if *calls != 0 || len(saved) != 1 || !saved[0].Skipped {
		t.Errorf("unchanged game should be skipped: calls=%d saved=%+v", *calls, saved)
	}

	// Without the option the same state is fetched.
	SaveAchievements(context.Background(), dir, "Hades", steamLink("1145360"), creds)
	if *calls != 1 {
		t.Errorf("a plain refresh must always fetch, got %d calls", *calls)
	}
}

// A bulk run lists the Steam library once, not once per game.
func TestSaveAchievements_ListsSteamLibraryOnce(t *testing.T) {
	steamStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"playerstats":{"success":true,"achievements":[{"apiname":"A","achieved":0}]}}`))
	})
	listings := 0
	owned := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		listings++
		w.Write([]byte(`{"response":{"games":[{"appid":1,"name":"One"},{"appid":2,"name":"Two"}]}}`))
	}))
	t.Cleanup(owned.Close)
	old := steam.OwnedGamesURL
	steam.OwnedGamesURL = owned.URL
	t.Cleanup(func() { steam.OwnedGamesURL = old })

	dir := t.TempDir()
	creds := Credentials{SteamAPIKey: "k", SteamID: testSteamID64}
	for _, id := range []string{"1", "2"} {
		if _, err := SaveAchievements(context.Background(), dir, "Game "+id, steamLink(id), creds); err != nil {
			t.Fatal(err)
		}
	}
	if listings != 1 {
		t.Errorf("library listed %d times, want 1", listings)
	}
}

// A game with no achievements gets a Fetched date, so the next bulk refresh
// skips it like any other unchanged game instead of re-requesting it forever.
func TestSaveAchievements_NoStatsGameIsSkippedNextTime(t *testing.T) {
	const lp = int64(1700000000)
	calls := ownedStub(t, 600, lp)
	noStats := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"playerstats":{"error":"Requested app has no stats","success":false}}`))
	}))
	t.Cleanup(noStats.Close)
	steam.PlayerAchievementsURL = noStats.URL // steamStub's cleanup restores it

	dir := t.TempDir()
	creds := Credentials{SteamAPIKey: "k", SteamID: testSteamID64}
	for range 2 {
		if _, err := SaveAchievements(context.Background(), dir, "Hades", steamLink("1145360"), creds, SkipUnchanged); err != nil {
			t.Fatal(err)
		}
	}
	if *calls != 1 {
		t.Errorf("no-stats game requested %d times, want 1 (then skipped)", *calls)
	}
}

func TestSaveAchievements_SkipUnchangedStillFetchesWhenSomethingMoved(t *testing.T) {
	const lp = int64(1700000000)
	day := time.Unix(lp, 0).In(model.SiteLocation).Format("2006-01-02")
	creds := Credentials{SteamAPIKey: "k", SteamID: testSteamID64}

	cases := map[string]struct {
		rec      model.ProviderRecord
		playtime int
	}{
		"playtime grew":    {model.ProviderRecord{Fetched: "2026-07-01", PlaytimeMins: 600, LastPlayed: day}, 650},
		"never fetched":    {model.ProviderRecord{PlaytimeMins: 600, LastPlayed: day}, 600},
		"last fetch erred": {model.ProviderRecord{Fetched: "2026-07-01", PlaytimeMins: 600, LastPlayed: day, LastError: "boom"}, 600},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			seedSteam(t, dir, c.rec)
			calls := ownedStub(t, c.playtime, lp)
			if _, err := SaveAchievements(context.Background(), dir, "Hades", steamLink("1145360"), creds, SkipUnchanged); err != nil {
				t.Fatal(err)
			}
			if *calls != 1 {
				t.Errorf("expected a fetch, got %d calls", *calls)
			}
		})
	}

	t.Run("not archived yet", func(t *testing.T) {
		calls := ownedStub(t, 600, lp)
		if _, err := SaveAchievements(context.Background(), t.TempDir(), "Hades", steamLink("1145360"), creds, SkipUnchanged); err != nil {
			t.Fatal(err)
		}
		if *calls != 1 {
			t.Errorf("expected a fetch, got %d calls", *calls)
		}
	})
}

// countingTransport answers every request with an empty JSON list.
type countingTransport struct{ calls int }

func (c *countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.calls++
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`[]`)), Request: r}, nil
}

// The recently-played list is reused within a run, but refetched once it's
// older than the TTL or after ForgetScanCache, so a long serve doesn't freeze it.
func TestRARecentlyPlayed_ExpiresAndForgets(t *testing.T) {
	resetProviderCaches(t)
	tr := &countingTransport{}
	client := &retroachievements.RAClient{Username: "u", APIKey: "k", HTTP: &http.Client{Transport: tr}, Throttle: time.Nanosecond}
	ctx := context.Background()

	raRecentlyPlayed(ctx, client, "u")
	raRecentlyPlayed(ctx, client, "u")
	if tr.calls != 1 {
		t.Fatalf("fresh entry refetched: %d calls, want 1", tr.calls)
	}

	raRecentCache.Lock()
	e := raRecentCache.byUser["u"]
	e.at = time.Now().Add(-scanCacheTTL - time.Second)
	raRecentCache.byUser["u"] = e
	raRecentCache.Unlock()
	raRecentlyPlayed(ctx, client, "u")
	if tr.calls != 2 {
		t.Fatalf("expired entry not refetched: %d calls, want 2", tr.calls)
	}

	ForgetScanCache(Credentials{})
	raRecentlyPlayed(ctx, client, "u")
	if tr.calls != 3 {
		t.Fatalf("forgotten entry not refetched: %d calls, want 3", tr.calls)
	}
}

// A link whose provider has no credentials on this machine must say so, and
// which keys to set, rather than read as if the game had no link at all.
func TestSaveAchievements_NamesMissingCredentials(t *testing.T) {
	links := []model.ProviderLink{
		{Provider: model.ProviderRA, ID: "1"},
		{Provider: model.ProviderRA, ID: "2", Subset: true},
	}
	_, err := SaveAchievements(context.Background(), t.TempDir(), "Game", links, Credentials{})
	if err == nil || !strings.Contains(err.Error(), "RA_USERNAME and RA_API_KEY") {
		t.Fatalf("err = %v, want it to name the missing RA keys", err)
	}
	if strings.Count(err.Error(), "RetroAchievements") != 1 {
		t.Errorf("RetroAchievements listed more than once: %v", err)
	}

	_, err = SaveAchievements(context.Background(), t.TempDir(), "Game", nil, Credentials{})
	if err == nil || !strings.Contains(err.Error(), "no provider link") {
		t.Errorf("err = %v, want the no-link message", err)
	}
}
