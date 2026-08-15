package commands

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.dalton.dog/gamelog/internal/model"
	"go.dalton.dog/gamelog/internal/providers/retroachievements"
	"go.dalton.dog/gamelog/internal/providers/steam"
)

const testSteamID64 = "76561198066238038"

// steamStub points the Steam achievements endpoint at a local server for the
// duration of a test — the cross-package equivalent of steam package's own
// steamStub, needed here because saveAchievements constructs its own
// steam.SteamClient rather than accepting one.
func steamStub(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	old := steam.PlayerAchievementsURL
	steam.PlayerAchievementsURL = srv.URL
	t.Cleanup(func() { steam.PlayerAchievementsURL = old })
}

func unlocked(key, date string) model.ArchivedAchievement {
	return model.ArchivedAchievement{Key: key, Name: key, Unlocked: true, Date: date}
}

func locked(key string) model.ArchivedAchievement {
	return model.ArchivedAchievement{Key: key, Name: key}
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
	raRecentCache.byUser["u"] = map[string]*retroachievements.RARecentGame{}
	raRecentCache.Unlock()
	t.Cleanup(func() {
		raRecentCache.Lock()
		delete(raRecentCache.byUser, "u")
		raRecentCache.Unlock()
	})
}
