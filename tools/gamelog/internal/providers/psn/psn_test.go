package psn

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// psnStub points every psn endpoint at a local server for the duration of a
// test, mirroring steamStub's pattern.
func psnStub(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	oldAuthorize, oldToken, oldTrophy := AuthorizeURL, TokenURL, TrophyBaseURL
	AuthorizeURL = srv.URL + "/authorize"
	TokenURL = srv.URL + "/token"
	TrophyBaseURL = srv.URL + "/trophy/v1"
	t.Cleanup(func() {
		AuthorizeURL, TokenURL, TrophyBaseURL = oldAuthorize, oldToken, oldTrophy
	})
	return srv
}

// authAndToken serves the two OAuth legs any test needing a working client
// has to get through first: the 302-redirect code capture, then the code ->
// access_token exchange.
func authAndToken(t *testing.T, w http.ResponseWriter, r *http.Request) bool {
	t.Helper()
	switch r.URL.Path {
	case "/authorize":
		if got := r.Header.Get("Cookie"); !strings.Contains(got, "npsso=") {
			t.Errorf("authorize request missing npsso cookie, got %q", got)
		}
		w.Header().Set("Location", "com.scee.psxandroid.scecompcall://redirect?code=v3.abc123")
		w.WriteHeader(http.StatusFound)
		return true
	case "/token":
		if got := r.Header.Get("Authorization"); got != oauthClientBasic {
			t.Errorf("token request Authorization = %q, want the PS App client basic auth", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.PostForm.Get("code"); got != "v3.abc123" {
			t.Errorf("token request code = %q, want the code captured from the redirect", got)
		}
		w.Write([]byte(`{"access_token":"tok123","refresh_token":"ref123"}`))
		return true
	}
	return false
}

// TestAuthorize_CapturesCodeFromRedirect confirms the whole npsso -> code ->
// access_token dance produces a bearer token that gets attached to a real
// API request, without the client ever trying to follow the fake
// app-scheme redirect URI.
func TestAuthorize_CapturesCodeFromRedirect(t *testing.T) {
	var sawBearer string
	psnStub(t, func(w http.ResponseWriter, r *http.Request) {
		if authAndToken(t, w, r) {
			return
		}
		if r.URL.Path == "/trophy/v1/users/me/trophyTitles" {
			sawBearer = r.Header.Get("Authorization")
			w.Write([]byte(`{"trophyTitles":[],"totalItemCount":0}`))
			return
		}
		t.Fatalf("unexpected path %s", r.URL.Path)
	})

	c := &PSNClient{Npsso: "test-npsso", Throttle: time.Nanosecond}
	if _, err := c.TrophyTitles(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sawBearer != "Bearer tok123" {
		t.Errorf("Authorization = %q, want Bearer tok123", sawBearer)
	}
}

// The token exchange must happen once per client and be reused, not redone
// on every request.
func TestToken_ExchangedOnce(t *testing.T) {
	var authorizeCalls, tokenCalls int
	psnStub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/authorize":
			authorizeCalls++
			w.Header().Set("Location", "com.scee.psxandroid.scecompcall://redirect?code=v3.abc123")
			w.WriteHeader(http.StatusFound)
		case "/token":
			tokenCalls++
			w.Write([]byte(`{"access_token":"tok123"}`))
		case "/trophy/v1/users/me/trophyTitles":
			w.Write([]byte(`{"trophyTitles":[],"totalItemCount":0}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})

	c := &PSNClient{Npsso: "test-npsso", Throttle: time.Nanosecond}
	for range 3 {
		if _, err := c.TrophyTitles(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if authorizeCalls != 1 || tokenCalls != 1 {
		t.Errorf("authorize=%d token=%d calls, want 1 each (token should be cached)", authorizeCalls, tokenCalls)
	}
}

// A missing/expired npsso means no Location header comes back — that must
// surface as a readable error, not a panic or a silent empty token.
func TestAuthorize_NoRedirectIsAnError(t *testing.T) {
	psnStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	c := &PSNClient{Npsso: "bad-npsso"}
	_, err := c.TrophyTitles(context.Background())
	if err == nil {
		t.Fatal("expected an error when authorize doesn't redirect")
	}
}

// TrophyTitles must follow nextOffset until every title is collected.
func TestTrophyTitles_Paginates(t *testing.T) {
	psnStub(t, func(w http.ResponseWriter, r *http.Request) {
		if authAndToken(t, w, r) {
			return
		}
		offset := r.URL.Query().Get("offset")
		switch offset {
		case "0":
			w.Write([]byte(`{"trophyTitles":[{"npCommunicationId":"NPWR00001_00","trophyTitleName":"Game One"}],"totalItemCount":2,"nextOffset":1}`))
		case "1":
			w.Write([]byte(`{"trophyTitles":[{"npCommunicationId":"NPWR00002_00","trophyTitleName":"Game Two"}],"totalItemCount":2}`))
		default:
			t.Fatalf("unexpected offset %q", offset)
		}
	})

	c := &PSNClient{Npsso: "test-npsso", Throttle: time.Nanosecond}
	got, err := c.TrophyTitles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d titles, want 2 (pagination should have followed nextOffset)", len(got))
	}
	if got["NPWR00002_00"].TrophyTitleName != "Game Two" {
		t.Errorf("second page not merged in: %+v", got)
	}
}

// npServiceName must be forwarded on both the definitions and earned calls
// when the title requires it (PS3/PS4/Vita titles).
func TestTrophyDefinitions_ForwardsNpServiceName(t *testing.T) {
	var sawServiceName string
	psnStub(t, func(w http.ResponseWriter, r *http.Request) {
		if authAndToken(t, w, r) {
			return
		}
		sawServiceName = r.URL.Query().Get("npServiceName")
		w.Write([]byte(`{"trophies":[]}`))
	})

	c := &PSNClient{Npsso: "test-npsso", Throttle: time.Nanosecond}
	if _, err := c.TrophyDefinitions(context.Background(), "NPWR00001_00", "trophy"); err != nil {
		t.Fatal(err)
	}
	if sawServiceName != "trophy" {
		t.Errorf("npServiceName = %q, want %q", sawServiceName, "trophy")
	}
}

// FetchRecord joins trophy definitions (name/description/tier) with earned
// status (unlocked/date) on trophyId, and reports a platinum unlock as
// AwardKind.
func TestFetchRecord_JoinsDefinitionsAndEarnedStatus(t *testing.T) {
	psnStub(t, func(w http.ResponseWriter, r *http.Request) {
		if authAndToken(t, w, r) {
			return
		}
		switch {
		case r.URL.Path == "/trophy/v1/users/me/trophyTitles":
			w.Write([]byte(`{"trophyTitles":[{
				"npServiceName":"trophy2",
				"npCommunicationId":"NPWR00001_00",
				"trophyTitleName":"Astro's Playroom",
				"trophyTitlePlatform":"PS5",
				"progress":100
			}],"totalItemCount":1}`))
		case strings.HasPrefix(r.URL.Path, "/trophy/v1/npCommunicationIds/"):
			w.Write([]byte(`{"trophies":[
				{"trophyId":0,"trophyType":"platinum","trophyName":"Everything","trophyDetail":"Found it all"},
				{"trophyId":1,"trophyType":"bronze","trophyName":"First Step","trophyDetail":"Take one"}
			]}`))
		case strings.HasPrefix(r.URL.Path, "/trophy/v1/users/me/npCommunicationIds/"):
			w.Write([]byte(`{"lastUpdatedDateTime":"2020-11-21T10:45:19Z","trophies":[
				{"trophyId":0,"earned":true,"earnedDateTime":"2020-11-21T10:45:19Z","trophyType":"platinum"},
				{"trophyId":1,"earned":false,"trophyType":"bronze"}
			]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})

	c := &PSNClient{Npsso: "test-npsso", Throttle: time.Nanosecond}
	rec, err := FetchRecord(context.Background(), c, "NPWR00001_00")
	if err != nil {
		t.Fatal(err)
	}
	if rec.LastError != "" {
		t.Fatalf("unexpected LastError: %s", rec.LastError)
	}
	if rec.Source != "" {
		t.Errorf("Source = %q, want empty (first-party fetch)", rec.Source)
	}
	if rec.Total != 2 || rec.Unlocked != 1 {
		t.Fatalf("got Total=%d Unlocked=%d, want 2 and 1", rec.Total, rec.Unlocked)
	}
	if rec.AwardKind != "platinum" || rec.AwardDate != "2020-11-21" {
		t.Errorf("AwardKind/AwardDate = %q/%q, want platinum/2020-11-21", rec.AwardKind, rec.AwardDate)
	}

	var plat, bronze *struct {
		Name     string
		Tier     string
		Unlocked bool
	}
	for i := range rec.Achievements {
		a := rec.Achievements[i]
		switch a.Key {
		case "0":
			plat = &struct {
				Name     string
				Tier     string
				Unlocked bool
			}{a.Name, a.Tier, a.Unlocked}
		case "1":
			bronze = &struct {
				Name     string
				Tier     string
				Unlocked bool
			}{a.Name, a.Tier, a.Unlocked}
		}
	}
	if plat == nil || plat.Name != "Everything" || plat.Tier != "platinum" || !plat.Unlocked {
		t.Errorf("platinum trophy not joined correctly: %+v", plat)
	}
	if bronze == nil || bronze.Name != "First Step" || bronze.Tier != "bronze" || bronze.Unlocked {
		t.Errorf("bronze trophy not joined correctly: %+v", bronze)
	}
}

// A title not on the account (never launched/synced) must not error the
// whole batch — it reports LastError and leaves any existing archive alone.
func TestFetchRecord_UnknownTitleIsNotAHardError(t *testing.T) {
	psnStub(t, func(w http.ResponseWriter, r *http.Request) {
		if authAndToken(t, w, r) {
			return
		}
		w.Write([]byte(`{"trophyTitles":[],"totalItemCount":0}`))
	})

	c := &PSNClient{Npsso: "test-npsso", Throttle: time.Nanosecond}
	rec, err := FetchRecord(context.Background(), c, "NPWR99999_00")
	if err != nil {
		t.Fatalf("expected no hard error, got %v", err)
	}
	if rec.LastError == "" {
		t.Error("expected LastError to explain the missing title")
	}
}
