package retroachievements

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"go.dalton.dog/gamelog/internal/model"
)

func raStub(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	old := raGameProgressURL
	raGameProgressURL = srv.URL
	t.Cleanup(func() { raGameProgressURL = old })
}

// Without a=1 the API omits the award fields entirely, which silently
// downgrades every suggestion to medium confidence.
func TestGetGameProgress_RequestsAwardMetadata(t *testing.T) {
	raStub(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("a"); got != "1" {
			t.Errorf("a = %q, want 1 (award metadata flag)", got)
		}
		w.Write([]byte(`{"Achievements":{}}`))
	})

	c := &RAClient{Username: "u", APIKey: "k"}
	if _, err := c.GetGameProgress(context.Background(), "36125"); err != nil {
		t.Fatal(err)
	}
}

// RA answers Go's default User-Agent with a 403, so every request must
// identify itself.
func TestGetGameProgress_SendsDescriptiveUserAgent(t *testing.T) {
	raStub(t, func(w http.ResponseWriter, r *http.Request) {
		ua := r.Header.Get("User-Agent")
		if ua == "" || strings.HasPrefix(ua, "Go-http-client") {
			t.Errorf("User-Agent = %q, want a descriptive one", ua)
		}
		w.Write([]byte(`{"Achievements":{}}`))
	})

	c := &RAClient{Username: "u", APIKey: "k"}
	if _, err := c.GetGameProgress(context.Background(), "1"); err != nil {
		t.Fatal(err)
	}
}

// An unknown game ID is answered with HTTP 200 and a bare `[]`, which would
// otherwise decode into an unreadable json type error.
func TestGetGameProgress_UnknownGameID(t *testing.T) {
	raStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[]`))
	})

	c := &RAClient{Username: "u", APIKey: "k"}
	_, err := c.GetGameProgress(context.Background(), "90000")
	if err == nil {
		t.Fatal("expected an error for an unknown game ID")
	}
	if !strings.Contains(err.Error(), "no game with ID 90000") {
		t.Errorf("want a readable not-found message, got %v", err)
	}
}

// A valid game that simply has no achievement set returns `{}` and is fine.
func TestGetGameProgress_GameWithNoAchievements(t *testing.T) {
	raStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"Title":"The New Zealand Story","NumAchievements":0,"Achievements":{}}`))
	})

	c := &RAClient{Username: "u", APIKey: "k"}
	got, err := c.GetGameProgress(context.Background(), "33")
	if err != nil {
		t.Fatalf("a game with no achievements should not error: %v", err)
	}
	if len(got.Achievements) != 0 {
		t.Errorf("expected no achievements, got %d", len(got.Achievements))
	}
}

func TestGetGameProgress_SurfacesAPIErrorMessage(t *testing.T) {
	raStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Unauthenticated.","errors":[{"status":"401"}]}`))
	})

	c := &RAClient{Username: "u", APIKey: "bad"}
	_, err := c.GetGameProgress(context.Background(), "1")
	if err == nil {
		t.Fatal("expected an error for a 401")
	}
	if !strings.Contains(err.Error(), "Unauthenticated.") {
		t.Errorf("want the API's own message in the error, got %v", err)
	}
}

func TestRAProgress_DecodesSampleResponse(t *testing.T) {
	raw, err := os.ReadFile("testdata/ra_progress_sample.json")
	if err != nil {
		t.Fatal(err)
	}
	var p RAProgress
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("decoding sample: %v", err)
	}
	if len(p.Achievements) != 3 {
		t.Fatalf("expected 3 achievements, got %d", len(p.Achievements))
	}
	if p.HighestAwardKind != "mastered" {
		t.Fatalf("unexpected HighestAwardKind: %q", p.HighestAwardKind)
	}
	got := RASuggestRange(p)
	if !got.OK || got.Confidence != "high" {
		t.Fatalf("expected a high-confidence suggestion from the sample, got %+v", got)
	}
	if !got.Started.Equal(mustParseRADate(t, "2026-01-05 10:03:41")) {
		t.Fatalf("unexpected started: %v", got.Started)
	}
}

func mustParseRADate(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := ParseRADate(s)
	if err != nil {
		t.Fatalf("ParseRADate(%q): %v", s, err)
	}
	return tm
}

func TestParseRADate(t *testing.T) {
	got := mustParseRADate(t, "2026-01-05 14:32:10")
	want := time.Date(2026, 1, 5, 14, 32, 10, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	if _, err := ParseRADate(""); err == nil {
		t.Fatal("expected error for empty date")
	}
	if _, err := ParseRADate("not-a-date"); err == nil {
		t.Fatal("expected error for malformed date")
	}
}

func TestRASuggestRange_Mastered(t *testing.T) {
	p := RAProgress{
		Achievements: map[string]RAAchievement{
			"1": {DateEarned: "2026-01-05 10:00:00"},
			"2": {DateEarned: "2026-03-20 22:00:00"},
		},
		HighestAwardKind: "mastered",
		// RFC3339 with an offset — the shape the live API actually returns
		// for HighestAwardDate, unlike the per-achievement dates above.
		HighestAwardDate: "2026-03-21T04:00:00+00:00",
	}
	got := RASuggestRange(p)
	if !got.OK {
		t.Fatal("expected OK=true")
	}
	if got.Confidence != "high" {
		t.Fatalf("expected high confidence, got %q", got.Confidence)
	}
	if got.AwardKind != "mastered" {
		t.Fatalf("expected AwardKind=mastered, got %q", got.AwardKind)
	}
	if !got.Started.Equal(mustParseRADate(t, "2026-01-05 10:00:00")) {
		t.Fatalf("unexpected started: %v", got.Started)
	}
	if !got.Finished.Equal(time.Date(2026, 3, 21, 4, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected finished: %v", got.Finished)
	}
}

// "beaten-hardcore" is a real award kind that means the game was finished —
// the most relevant finish signal there is for a play log.
func TestRASuggestRange_BeatenCountsAsFinished(t *testing.T) {
	for _, kind := range []string{"beaten-hardcore", "beaten-softcore", "completed", "Mastered"} {
		t.Run(kind, func(t *testing.T) {
			p := RAProgress{
				Achievements:     map[string]RAAchievement{"1": {DateEarned: "2026-06-29 14:32:22"}},
				HighestAwardKind: kind,
				HighestAwardDate: "2026-07-11T18:55:04+00:00",
			}
			got := RASuggestRange(p)
			if got.Confidence != "high" {
				t.Fatalf("expected high confidence for kind %q, got %q", kind, got.Confidence)
			}
			if !got.Finished.Equal(time.Date(2026, 7, 11, 18, 55, 4, 0, time.UTC)) {
				t.Fatalf("expected the award date as finished, got %v", got.Finished)
			}
		})
	}
}

// An award kind the API reports but we don't treat as a finish must not
// silently borrow the award date.
func TestRASuggestRange_UnknownAwardKindFallsBackToLatestUnlock(t *testing.T) {
	p := RAProgress{
		Achievements:     map[string]RAAchievement{"1": {DateEarned: "2026-01-05 10:00:00"}},
		HighestAwardKind: "some-future-award",
		HighestAwardDate: "2026-09-09T12:00:00+00:00",
	}
	got := RASuggestRange(p)
	if got.Confidence != "medium" {
		t.Fatalf("expected medium confidence, got %q", got.Confidence)
	}
	if !got.Finished.Equal(mustParseRADate(t, "2026-01-05 10:00:00")) {
		t.Fatalf("expected fallback to latest unlock, got %v", got.Finished)
	}
}

func TestParseRAAwardDate(t *testing.T) {
	got, err := ParseRAAwardDate("2026-07-18T15:05:30+00:00")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(time.Date(2026, 7, 18, 15, 5, 30, 0, time.UTC)) {
		t.Fatalf("unexpected time: %v", got)
	}
	// The per-achievement layout must not be accepted here, and vice versa —
	// conflating the two is what broke the mastery path.
	if _, err := ParseRAAwardDate("2026-07-18 15:05:30"); err == nil {
		t.Error("expected SQL-datetime to be rejected by ParseRAAwardDate")
	}
	if _, err := ParseRADate("2026-07-18T15:05:30+00:00"); err == nil {
		t.Error("expected RFC3339 to be rejected by ParseRADate")
	}
	if _, err := ParseRAAwardDate(""); err == nil {
		t.Error("expected error for empty award date")
	}
}

func TestRASuggestRange_PartialProgressNoMastery(t *testing.T) {
	p := RAProgress{
		Achievements: map[string]RAAchievement{
			"1": {DateEarned: "2026-01-05 10:00:00"},
			"2": {DateEarned: "2026-02-01 12:00:00"},
		},
		// no HighestAwardKind/Date
	}
	got := RASuggestRange(p)
	if !got.OK {
		t.Fatal("expected OK=true")
	}
	if got.Confidence != "medium" {
		t.Fatalf("expected medium confidence, got %q", got.Confidence)
	}
	if !got.Finished.Equal(mustParseRADate(t, "2026-02-01 12:00:00")) {
		t.Fatalf("expected finished to fall back to latest earned date, got %v", got.Finished)
	}
}

func TestRASuggestRange_HardcoreDateUsed(t *testing.T) {
	p := RAProgress{
		Achievements: map[string]RAAchievement{
			"1": {DateEarnedHardcore: "2026-01-05 10:00:00"},
		},
	}
	got := RASuggestRange(p)
	if !got.OK {
		t.Fatal("expected OK=true from hardcore-only date")
	}
}

func TestRASuggestRange_NoAchievements(t *testing.T) {
	got := RASuggestRange(RAProgress{})
	if got.OK {
		t.Fatal("expected OK=false when no achievements earned")
	}
}

func TestFetchRARecord_CapturesLockedAndRaw(t *testing.T) {
	raStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
		  "Title":"Kirby","ConsoleName":"Wii","NumAchievements":3,"UserCompletion":"66.67%",
		  "UserTotalPlaytime":1029,
		  "HighestAwardKind":"beaten-hardcore","HighestAwardDate":"2026-07-11T18:55:04+00:00",
		  "Achievements":{
		    "1":{"ID":1,"Title":"First","Description":"d1","Points":1,"BadgeName":"111",
		         "DateEarned":"2026-06-29 14:04:08","DateEarnedHardcore":"2026-06-29 14:04:08"},
		    "2":{"ID":2,"Title":"Locked","Description":"d2","Points":5,"BadgeName":"222"},
		    "3":{"ID":3,"Title":"Softcore","Description":"d3","Points":2,"BadgeName":"333",
		         "DateEarned":"2026-06-30 10:00:00"}
		  }}`))
	})

	client := &RAClient{Username: "u", APIKey: "k", Throttle: time.Nanosecond}
	rec, err := FetchRecord(context.Background(), client, "104", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Achievements) != 3 {
		t.Fatalf("locked achievements must be kept, got %d of 3", len(rec.Achievements))
	}
	if rec.Unlocked != 2 || rec.Total != 3 {
		t.Errorf("counts wrong: %d/%d", rec.Unlocked, rec.Total)
	}
	if rec.Platform != "Wii" || rec.Completion != "66.67%" {
		t.Errorf("metadata not captured: %+v", rec)
	}
	// RA reports UserTotalPlaytime in seconds; PlaytimeMins is minutes
	// everywhere else in the archive, so it must be converted, not copied.
	if rec.PlaytimeMins != 17 {
		t.Errorf("playtime = %d mins, want 17 (1029s)", rec.PlaytimeMins)
	}
	if len(rec.Raw) == 0 {
		t.Error("raw response should be retained")
	}
	byKey := map[string]model.ArchivedAchievement{}
	for _, a := range rec.Achievements {
		byKey[a.Key] = a
	}
	if !byKey["1"].Hardcore {
		t.Error("hardcore unlock should be flagged")
	}
	if byKey["3"].Hardcore {
		t.Error("softcore-only unlock must not be flagged hardcore")
	}
	if byKey["2"].Unlocked {
		t.Error("locked achievement must not be marked unlocked")
	}
	if byKey["2"].Icon != "222" {
		t.Errorf("badge not captured: %q", byKey["2"].Icon)
	}
}

// Dates must round-trip through the RFC3339 parsing Hugo's `time` uses.
func TestStamp_IsParseableRFC3339InSiteZone(t *testing.T) {
	winter := stamp(time.Date(2026, 12, 20, 17, 6, 33, 0, time.UTC))
	summer := stamp(time.Date(2026, 10, 23, 22, 39, 49, 0, time.UTC))

	for _, s := range []string{winter, summer} {
		parsed, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatalf("stamp %q is not RFC3339: %v", s, err)
		}
		if parsed.In(model.SiteLocation).Format("2006-01-02") != s[:10] {
			t.Errorf("date prefix %q disagrees with the instant it encodes", s)
		}
	}
	if winter[19:] == summer[19:] {
		t.Errorf("expected different UTC offsets across DST, both were %q", winter[19:])
	}
}

func raRecentStub(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	old := raRecentlyPlayedURL
	raRecentlyPlayedURL = srv.URL
	t.Cleanup(func() { raRecentlyPlayedURL = old })
}

// The API caps `c` at 50, so anything past the newest 50 games is only
// reachable through `o` paging.
func TestGetRecentlyPlayed_WalksPages(t *testing.T) {
	var offsets []string
	raRecentStub(t, func(w http.ResponseWriter, r *http.Request) {
		offsets = append(offsets, r.URL.Query().Get("o"))
		if r.URL.Query().Get("o") == "0" {
			games := make([]RARecentGame, raRecentPageSize)
			for i := range games {
				games[i] = RARecentGame{GameID: i + 1, LastPlayed: "2026-08-05 03:38:56"}
			}
			json.NewEncoder(w).Encode(games)
			return
		}
		json.NewEncoder(w).Encode([]RARecentGame{{GameID: 7601, LastPlayed: "2026-01-02 10:00:00"}})
	})

	client := &RAClient{Username: "u", APIKey: "k", Throttle: time.Nanosecond}
	games, err := client.GetRecentlyPlayed(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != raRecentPageSize+1 {
		t.Fatalf("got %d games, want %d across two pages", len(games), raRecentPageSize+1)
	}
	if want := []string{"0", "50"}; strings.Join(offsets, ",") != strings.Join(want, ",") {
		t.Errorf("offsets requested = %v, want %v", offsets, want)
	}
	if got := games[len(games)-1].ID(); got != "7601" {
		t.Errorf("second page not appended, last id = %q", got)
	}
}

// The progress endpoint has no last-played field, so without this a session
// that unlocked nothing leaves the record stuck at the newest unlock.
func TestFetchRARecord_LastPlayedComesFromRecentlyPlayed(t *testing.T) {
	raStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"Title":"Layton","ConsoleName":"Nintendo DS","NumAchievements":2,
		  "Achievements":{"1":{"ID":1,"Title":"First","DateEarnedHardcore":"2026-07-27 02:40:24"}}}`))
	})

	client := &RAClient{Username: "u", APIKey: "k", Throttle: time.Nanosecond}
	// Zoneless, like DateEarned: parsed as UTC, reported in the site's zone.
	recent := &RARecentGame{GameID: 7601, LastPlayed: "2026-08-05 14:38:56"}
	rec, err := FetchRecord(context.Background(), client, "7601", recent)
	if err != nil {
		t.Fatal(err)
	}
	if rec.LastPlayed != "2026-08-05" {
		t.Errorf("last_played = %q, want the played date 2026-08-05, not the unlock date", rec.LastPlayed)
	}
	if rec.Last != "2026-07-26" {
		t.Errorf("last unlock = %q, want the unlock date left alone", rec.Last)
	}
}
