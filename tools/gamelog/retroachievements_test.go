package main

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
	got := raSuggestRange(p)
	if !got.OK || got.Confidence != "high" {
		t.Fatalf("expected a high-confidence suggestion from the sample, got %+v", got)
	}
	if !got.Started.Equal(mustParseRADate(t, "2026-01-05 10:03:41")) {
		t.Fatalf("unexpected started: %v", got.Started)
	}
}

func mustParseRADate(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := parseRADate(s)
	if err != nil {
		t.Fatalf("parseRADate(%q): %v", s, err)
	}
	return tm
}

func TestParseRADate(t *testing.T) {
	got := mustParseRADate(t, "2026-01-05 14:32:10")
	want := time.Date(2026, 1, 5, 14, 32, 10, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	if _, err := parseRADate(""); err == nil {
		t.Fatal("expected error for empty date")
	}
	if _, err := parseRADate("not-a-date"); err == nil {
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
	got := raSuggestRange(p)
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
			got := raSuggestRange(p)
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
	got := raSuggestRange(p)
	if got.Confidence != "medium" {
		t.Fatalf("expected medium confidence, got %q", got.Confidence)
	}
	if !got.Finished.Equal(mustParseRADate(t, "2026-01-05 10:00:00")) {
		t.Fatalf("expected fallback to latest unlock, got %v", got.Finished)
	}
}

func TestParseRAAwardDate(t *testing.T) {
	got, err := parseRAAwardDate("2026-07-18T15:05:30+00:00")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(time.Date(2026, 7, 18, 15, 5, 30, 0, time.UTC)) {
		t.Fatalf("unexpected time: %v", got)
	}
	// The per-achievement layout must not be accepted here, and vice versa —
	// conflating the two is what broke the mastery path.
	if _, err := parseRAAwardDate("2026-07-18 15:05:30"); err == nil {
		t.Error("expected SQL-datetime to be rejected by parseRAAwardDate")
	}
	if _, err := parseRADate("2026-07-18T15:05:30+00:00"); err == nil {
		t.Error("expected RFC3339 to be rejected by parseRADate")
	}
	if _, err := parseRAAwardDate(""); err == nil {
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
	got := raSuggestRange(p)
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
	got := raSuggestRange(p)
	if !got.OK {
		t.Fatal("expected OK=true from hardcore-only date")
	}
}

func TestRASuggestRange_NoAchievements(t *testing.T) {
	got := raSuggestRange(RAProgress{})
	if got.OK {
		t.Fatal("expected OK=false when no achievements earned")
	}
}
