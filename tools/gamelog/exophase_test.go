package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The profile payload is a JS single-quoted string in which every structural
// character is \uXXXX-escaped — including the quotes of nested JSON strings,
// which is why decodeJSString walks left to right rather than doing two passes.
//
// The contract is that the output is valid JSON, not that it is slash-free:
// `\\\/` decodes to `\/`, which is a legal JSON escape the inner decoder then
// resolves. Asserting on the decoded struct is what actually matters, since a
// naive two-pass unescape produces text that fails to parse at all.
func TestDecodeJSStringHandlesNestedEscapes(t *testing.T) {
	in := `{"canonical_id":"NPWR15587_00","title":"a\\\/b"}`
	var got struct {
		CanonicalID string `json:"canonical_id"`
		Title       string `json:"title"`
	}
	if err := json.Unmarshal([]byte(decodeJSString(in)), &got); err != nil {
		t.Fatalf("decodeJSString produced unparseable JSON: %v\n%s", err, decodeJSString(in))
	}
	if got.CanonicalID != "NPWR15587_00" || got.Title != "a/b" {
		t.Errorf("got %+v, want {NPWR15587_00 a/b}", got)
	}
}

func TestParseExophasePlaytime(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
	}{
		{"15h 6m", 906},
		{"687h 55m", 41275},
		{"16m", 16},
		{"7h", 420},
		{"", 0},
	} {
		if got := parseExophasePlaytime(tc.in); got != tc.want {
			t.Errorf("parseExophasePlaytime(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// profileHTML is the shape the client actually parses: service widgets for
// the player id, and the embedded payload for canonical ids.
func profileHTML(masterID int, canonical string) string {
	payload := fmt.Sprintf(`{"games":[{"master_id":%d,"meta":{"canonical_id":"%s","title":"Sekiro"}}]}`, masterID, canonical)
	var esc strings.Builder
	for _, r := range payload {
		if r == '"' || r == '{' || r == '}' || r == ':' || r == ',' || r == '[' || r == ']' {
			fmt.Fprintf(&esc, `\u%04x`, r)
			continue
		}
		esc.WriteRune(r)
	}
	return `<li data-endpoint="/psn/user/DaltonSW/" data-environment="psn" data-playerid="4103091" class="psn service-widget"></li>` +
		`<script>window.playerGames = '` + esc.String() + `';</script>`
}

func TestFetchPSNRecordBuildsArchiveRecord(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" || strings.HasPrefix(r.Header.Get("User-Agent"), "Go-http-client") {
			// Cloudflare serves an interstitial to Go's default UA; a client
			// that forgets the browser UA must fail loudly in tests too.
			http.Error(w, "blocked", http.StatusForbidden)
			return
		}
		switch {
		case strings.Contains(r.URL.Path, "/user/"):
			fmt.Fprint(w, profileHTML(182258, "NPWR15587_00"))
		case strings.HasSuffix(r.URL.Path, "/earned"):
			fmt.Fprint(w, `{"list":[
			  {"slug":"shura","timestamp":1744845788},
			  {"slug":"isshin","timestamp":1640400000}]}`)
		case strings.HasSuffix(r.URL.Path, "/games"):
			fmt.Fprint(w, `{"success":true,"games":[{"master_id":182258,"earned_awards":2,
			  "total_awards":34,"playtime":"15h 6m","lastplayed":1744845847,"percent":73,
			  "meta":{"title":"Sekiro","platforms":[{"name":"PS4"}]}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	defer swapExophaseURLs(srv.URL)()

	rec, err := FetchPSNRecord(context.Background(), &ExophaseClient{User: "DaltonSW"}, "NPWR15587_00")
	if err != nil {
		t.Fatal(err)
	}
	if rec.LastError != "" {
		t.Fatalf("unexpected error: %s", rec.LastError)
	}
	if rec.Unlocked != 2 || rec.Total != 34 {
		t.Errorf("got %d/%d, want 2/34", rec.Unlocked, rec.Total)
	}
	if rec.Source != "exophase" {
		t.Errorf("source = %q, want exophase — a mirror must be distinguishable from a first-party fetch", rec.Source)
	}
	if rec.Platform != "PS4" {
		t.Errorf("platform = %q, want PS4", rec.Platform)
	}
	if rec.PlaytimeMins != 906 {
		t.Errorf("playtime = %d, want 906", rec.PlaytimeMins)
	}
	// Unlocked achievements sort first in date order, so the older unlock leads.
	if rec.Achievements[0].Key != "isshin" {
		t.Errorf("achievements not in date order: %+v", rec.Achievements)
	}
	if len(rec.Raw) == 0 {
		t.Error("raw response not kept")
	}
}

// A game that isn't on the profile must report the miss and keep whatever is
// already archived, not overwrite it with an empty record.
func TestFetchPSNRecordUnknownGameIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/user/"):
			fmt.Fprint(w, profileHTML(182258, "NPWR15587_00"))
		default:
			fmt.Fprint(w, `{"success":true,"games":[]}`)
		}
	}))
	defer srv.Close()
	defer swapExophaseURLs(srv.URL)()

	rec, err := FetchPSNRecord(context.Background(), &ExophaseClient{User: "DaltonSW"}, "NPWR99999_00")
	if err != nil {
		t.Fatal(err)
	}
	if rec.LastError == "" {
		t.Error("expected a last_error for a game not on the profile")
	}
	if len(rec.Achievements) != 0 {
		t.Error("a miss must not invent achievements")
	}

	// mergeProvider is what turns that into "keep the existing data".
	old := &ProviderRecord{Unlocked: 5, Total: 10, Achievements: []ArchivedAchievement{{Key: "a", Unlocked: true}}}
	merged := mergeProvider(old, rec)
	if merged.Unlocked != 5 || len(merged.Achievements) != 1 {
		t.Errorf("a failed PSN fetch lost data: %+v", merged)
	}
}

func swapExophaseURLs(base string) func() {
	site, games, earned := exophaseBaseURL, exophaseGamesURL, exophaseEarnedURL
	exophaseBaseURL = base
	exophaseGamesURL = base + "/public/player/%s/games"
	exophaseEarnedURL = base + "/public/player/%s/game/%s/earned"
	return func() {
		exophaseBaseURL, exophaseGamesURL, exophaseEarnedURL = site, games, earned
	}
}
