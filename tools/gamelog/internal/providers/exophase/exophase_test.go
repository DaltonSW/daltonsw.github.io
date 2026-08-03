package exophase

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
		if got := ParsePlaytime(tc.in); got != tc.want {
			t.Errorf("ParsePlaytime(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// uplayProfileHTML is the shape the client actually parses for the "uplay"
// environment: service widgets for the player id, and the embedded payload
// for canonical ids.
// service widget naming that environment instead of "psn".
func uplayProfileHTML(masterID int, canonical string) string {
	payload := fmt.Sprintf(`{"games":[{"master_id":%d,"meta":{"canonical_id":"%s","title":"Far Cry 6"}}]}`, masterID, canonical)
	var esc strings.Builder
	for _, r := range payload {
		if r == '"' || r == '{' || r == '}' || r == ':' || r == ',' || r == '[' || r == ']' {
			fmt.Fprintf(&esc, `\u%04x`, r)
			continue
		}
		esc.WriteRune(r)
	}
	return `<li data-endpoint="/uplay/user/DaltonSW/" data-environment="uplay" data-playerid="9001" class="uplay service-widget"></li>` +
		`<script>window.playerGames = '` + esc.String() + `';</script>`
}

func TestFetchUbisoftRecordBuildsArchiveRecord(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/user/"):
			fmt.Fprint(w, uplayProfileHTML(555, "12345"))
		case strings.HasSuffix(r.URL.Path, "/earned"):
			fmt.Fprint(w, `{"list":[{"slug":"guerrilla","timestamp":1700000000}]}`)
		case strings.HasSuffix(r.URL.Path, "/games"):
			fmt.Fprint(w, `{"success":true,"games":[{"master_id":555,"earned_awards":1,
			  "total_awards":51,"playtime":"40h","lastplayed":1700000100,"percent":2,
			  "meta":{"title":"Far Cry 6","platforms":[{"name":"PC"}]}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	defer swapExophaseURLs(srv.URL)()

	rec, err := FetchUbisoftRecord(context.Background(), &ExophaseClient{User: "DaltonSW"}, "12345")
	if err != nil {
		t.Fatal(err)
	}
	if rec.LastError != "" {
		t.Fatalf("unexpected error: %s", rec.LastError)
	}
	if rec.Unlocked != 1 || rec.Total != 51 {
		t.Errorf("got %d/%d, want 1/51", rec.Unlocked, rec.Total)
	}
	if rec.Source != "exophase" {
		t.Errorf("source = %q, want exophase", rec.Source)
	}
	if rec.Platform != "PC" {
		t.Errorf("platform = %q, want PC", rec.Platform)
	}
}

// TestFetchUbisoftRecordDetails covers the parts of fetchRecord that
// TestFetchUbisoftRecordBuildsArchiveRecord's fixture doesn't exercise: the
// browser User-Agent requirement (Cloudflare 403s Go's default one), icon
// URL resolution, and unlocked-achievements-sort-first-by-date ordering.
func TestFetchUbisoftRecordDetails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" || strings.HasPrefix(r.Header.Get("User-Agent"), "Go-http-client") {
			http.Error(w, "blocked", http.StatusForbidden)
			return
		}
		switch {
		case strings.Contains(r.URL.Path, "/user/"):
			fmt.Fprint(w, uplayProfileHTML(182258, "67890"))
		case strings.HasSuffix(r.URL.Path, "/earned"):
			fmt.Fprint(w, `{"list":[
			  {"slug":"early-bird","timestamp":1744845788},
			  {"slug":"launch-day","timestamp":1640400000,"icons":{"m":"/uplay/awards/m/abc123.png"}}]}`)
		case strings.HasSuffix(r.URL.Path, "/games"):
			fmt.Fprint(w, `{"success":true,"games":[{"master_id":182258,"earned_awards":2,
			  "total_awards":34,"playtime":"15h 6m","lastplayed":1744845847,"percent":73,
			  "meta":{"title":"Far Cry 6","platforms":[{"name":"PC"}]}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	defer swapExophaseURLs(srv.URL)()

	rec, err := FetchUbisoftRecord(context.Background(), &ExophaseClient{User: "DaltonSW"}, "67890")
	if err != nil {
		t.Fatal(err)
	}
	if rec.LastError != "" {
		t.Fatalf("unexpected error: %s", rec.LastError)
	}
	// Unlocked achievements sort first in date order, so the older unlock leads.
	if rec.Achievements[0].Key != "launch-day" {
		t.Errorf("achievements not in date order: %+v", rec.Achievements)
	}
	if want := srv.URL + "/uplay/awards/m/abc123.png"; rec.Achievements[0].Icon != want {
		t.Errorf("icon = %q, want %q", rec.Achievements[0].Icon, want)
	}
	if rec.Achievements[1].Icon != "" {
		t.Errorf("early-bird has no icon in the fixture, got %q", rec.Achievements[1].Icon)
	}
	if len(rec.Raw) == 0 {
		t.Error("raw response not kept")
	}
}

// A game that isn't on the profile must report the miss and keep whatever is
// already archived, not overwrite it with an empty record.
func TestFetchUbisoftRecordUnknownGameIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/user/"):
			fmt.Fprint(w, uplayProfileHTML(182258, "67890"))
		default:
			fmt.Fprint(w, `{"success":true,"games":[]}`)
		}
	}))
	defer srv.Close()
	defer swapExophaseURLs(srv.URL)()

	rec, err := FetchUbisoftRecord(context.Background(), &ExophaseClient{User: "DaltonSW"}, "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if rec.LastError == "" {
		t.Error("expected a last_error for a game not on the profile")
	}
	if len(rec.Achievements) != 0 {
		t.Error("a miss must not invent achievements")
	}
	// A record carrying only LastError, with Achievements/Unlocked/Total left
	// zero, is what model.SaveRecord's merge treats as "keep the existing
	// data" — see model's own TestMergeProvider_NeverRetractsAnUnlock.
}

func swapExophaseURLs(base string) func() {
	site, games, earned, media := exophaseBaseURL, exophaseGamesURL, exophaseEarnedURL, exophaseMediaBaseURL
	exophaseBaseURL = base
	exophaseGamesURL = base + "/public/player/%s/games"
	exophaseEarnedURL = base + "/public/player/%s/game/%s/earned"
	exophaseMediaBaseURL = base
	return func() {
		exophaseBaseURL, exophaseGamesURL, exophaseEarnedURL, exophaseMediaBaseURL = site, games, earned, media
	}
}
