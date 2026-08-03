package xbox

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseXboxTime(t *testing.T) {
	got, ok := parseXboxTime("2011-12-22T20:09:54.0270000Z")
	if !ok {
		t.Fatal("expected a parse")
	}
	if !strings.HasPrefix(got, "2011-12-22T") {
		t.Errorf("got %q, want a timestamp on 2011-12-22", got)
	}
	if _, ok := parseXboxTime("not a date"); ok {
		t.Error("expected garbage input to fail, not silently succeed")
	}
	if _, ok := parseXboxTime(xboxUnsetTime); ok {
		t.Error("expected the SqlDateTime.MinValue sentinel to fail, not parse as a real date")
	}
}

func TestFetchRecordBuildsArchiveRecord(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Authorization") != "test-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/account"):
			fmt.Fprint(w, `{"content":{"profileUsers":[{"id":"2533274922308637"}]},"code":200}`)
		case strings.HasSuffix(r.URL.Path, "/titleHistory"):
			fmt.Fprint(w, `{"content":{"titles":[
			  {"titleId":"1480657033","name":"Peggle","devices":["Xbox360"],
			   "achievement":{"currentAchievements":3,"totalAchievements":15},
			   "titleHistory":{"lastTimePlayed":"2011-12-29T22:56:18Z"}}
			]},"code":200}`)
		case strings.Contains(r.URL.Path, "/achievements/x360/"):
			fmt.Fprint(w, `{"content":{"achievements":[
			  {"id":2,"name":"Go ULTRA!","description":"Cleared all pegs.","gamerscore":15,
			   "unlocked":true,"timeUnlocked":"2011-12-22T20:09:54.0270000Z"},
			  {"id":1,"name":"Catch the Fever","description":"Cured fever.","gamerscore":5,
			   "unlocked":true,"timeUnlocked":"2011-12-22T20:02:43.5630000Z"},
			  {"id":3,"name":"Ancient Unlock","description":"No real date on file.","gamerscore":10,
			   "unlocked":true,"timeUnlocked":"1753-01-01T00:00:00.0000000Z"}
			]},"code":200}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	defer swapXboxURLs(srv.URL)()

	rec, err := FetchRecord(context.Background(), &XBLClient{APIKey: "test-key"}, "1480657033")
	if err != nil {
		t.Fatal(err)
	}
	if rec.LastError != "" {
		t.Fatalf("unexpected error: %s", rec.LastError)
	}
	// 3 unlocked per titleHistory, even though only 2 carry a real date —
	// the third (xboxUnsetTime) mustn't be silently undercounted.
	if rec.Unlocked != 3 || rec.Total != 15 {
		t.Errorf("got %d/%d, want 3/15", rec.Unlocked, rec.Total)
	}
	var sawDateless bool
	for _, a := range rec.Achievements {
		if a.Key == "3" {
			sawDateless = true
			if !a.Unlocked || a.Date != "" {
				t.Errorf("dateless unlock = %+v, want Unlocked:true Date:\"\"", a)
			}
		}
	}
	if !sawDateless {
		t.Fatal("dateless achievement missing from the record")
	}
	if rec.Source != "openxbl" {
		t.Errorf("source = %q, want openxbl", rec.Source)
	}
	if rec.Platform != "Xbox 360" {
		t.Errorf("platform = %q, want Xbox 360", rec.Platform)
	}
	if rec.LastPlayed != "2011-12-29" {
		t.Errorf("last_played = %q, want 2011-12-29", rec.LastPlayed)
	}
	// Unlocked achievements sort first in date order, empty dates first —
	// so the dateless one leads, then the older real unlock.
	if rec.Achievements[0].Key != "3" || rec.Achievements[1].Key != "1" {
		t.Errorf("achievements not in expected order: %+v", rec.Achievements)
	}
	if rec.Achievements[1].Points != 5 {
		t.Errorf("points = %d, want 5", rec.Achievements[1].Points)
	}
	if len(rec.Raw) == 0 {
		t.Error("raw response not kept")
	}
}

// A title not in the account's history must report the miss and keep
// whatever is already archived, not overwrite it with an empty record.
func TestFetchRecordUnknownTitleIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/account"):
			fmt.Fprint(w, `{"content":{"profileUsers":[{"id":"2533274922308637"}]},"code":200}`)
		default:
			fmt.Fprint(w, `{"content":{"titles":[]},"code":200}`)
		}
	}))
	defer srv.Close()
	defer swapXboxURLs(srv.URL)()

	rec, err := FetchRecord(context.Background(), &XBLClient{APIKey: "test-key"}, "999999")
	if err != nil {
		t.Fatal(err)
	}
	if rec.LastError == "" {
		t.Error("expected a last_error for a title not in the account's history")
	}
	if len(rec.Achievements) != 0 {
		t.Error("a miss must not invent achievements")
	}
}

func swapXboxURLs(base string) func() {
	account, titleHistory, x360 := AccountURL, TitleHistoryURL, X360Achievements
	AccountURL = base + "/api/v2/account"
	TitleHistoryURL = base + "/api/v2/player/titleHistory"
	X360Achievements = base + "/api/v2/achievements/x360/%s/title/%s"
	return func() {
		AccountURL, TitleHistoryURL, X360Achievements = account, titleHistory, x360
	}
}
