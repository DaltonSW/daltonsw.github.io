package nadeo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go.dalton.dog/gamelog/internal/model"
)

// This file covers *behaviour* — request shape, batching, matching, retry,
// paging, headers — using synthetic bodies, because none of that depends on
// what the fields are called.
//
// Decoding is proved separately in decode_test.go, against real response
// bodies captured from a live run and committed under testdata/. Keep that
// split: a synthetic body here is fine, but nothing that asserts a field name
// should be written from documentation. See KNOWN-ISSUES.md for what happened
// the last time it was.

// nadeoStub points both API bases at one local server.
func nadeoStub(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	oldCore, oldLive := CoreBaseURL, LiveBaseURL
	CoreBaseURL, LiveBaseURL = srv.URL, srv.URL
	t.Cleanup(func() { CoreBaseURL, LiveBaseURL = oldCore, oldLive })
	return srv
}

// testClient never sleeps between requests.
func testClient() *Client {
	return &Client{Login: "svc", Password: "pw", Throttle: time.Nanosecond}
}

// fakeJWT builds a token whose payload carries the claims this package reads.
// Unsigned — decodeTokenClaims deliberately doesn't verify, since it's reading
// its own token rather than trusting someone else's.
func fakeJWT(accountID string, exp time.Time) string {
	payload, _ := json.Marshal(tokenClaims{Exp: exp.Unix(), AccountID: accountID})
	return "h." + base64.RawURLEncoding.EncodeToString(payload) + ".s"
}

const testAccountID = "5b4d42f4-c2de-407d-b367-cbff3fe817bc"

// authResponse writes a token response for whatever audience was asked for.
func authResponse(w http.ResponseWriter) {
	fmt.Fprintf(w, `{"accessToken":%q,"refreshToken":"r"}`,
		fakeJWT(testAccountID, time.Now().Add(time.Hour)))
}

func TestAuthenticate_UsesBasicAuthAndRequestedAudience(t *testing.T) {
	var gotAudience, gotUser, gotPass string
	var gotUA string
	nadeoStub(t, func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, _ = r.BasicAuth()
		gotUA = r.Header.Get("User-Agent")
		var body struct {
			Audience string `json:"audience"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		gotAudience = body.Audience
		authResponse(w)
	})

	if _, err := testClient().AccountID(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotUser != "svc" || gotPass != "pw" {
		t.Errorf("basic auth = %q/%q, want svc/pw", gotUser, gotPass)
	}
	if gotAudience != AudienceLive {
		t.Errorf("audience = %q, want %q", gotAudience, AudienceLive)
	}
	// Nadeo asks for an identifying User-Agent; an anonymous client is what
	// gets an IP blocked rather than rate-limited.
	if gotUA == "" || strings.HasPrefix(gotUA, "Go-http-client") {
		t.Errorf("User-Agent = %q, want a descriptive one", gotUA)
	}
}

func TestAccountID_ComesFromTheTokenClaims(t *testing.T) {
	nadeoStub(t, func(w http.ResponseWriter, r *http.Request) { authResponse(w) })

	got, err := testClient().AccountID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != testAccountID {
		t.Errorf("AccountID = %q, want %q", got, testAccountID)
	}
}

// The two audiences are separate tokens and must not be shared, or every Core
// call would go out with a Live token and 401.
func TestToken_FetchesEachAudienceSeparatelyAndCachesIt(t *testing.T) {
	var auths int
	nadeoStub(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/v2/authentication/token/basic") {
			auths++
			authResponse(w)
			return
		}
		w.Write([]byte(`[]`))
	})

	c := testClient()
	ctx := context.Background()
	for range 3 {
		if _, err := c.token(ctx, AudienceLive); err != nil {
			t.Fatal(err)
		}
		if _, err := c.token(ctx, AudienceCore); err != nil {
			t.Fatal(err)
		}
	}
	if auths != 2 {
		t.Errorf("authenticated %d times, want 2 (one per audience, then cached)", auths)
	}
}

func TestAuthenticate_SurfacesTheProvidersOwnMessage(t *testing.T) {
	nadeoStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Invalid credentials."}`))
	})

	_, err := testClient().AccountID(context.Background())
	if err == nil {
		t.Fatal("want an error for bad credentials")
	}
	if !strings.Contains(err.Error(), "Invalid credentials.") {
		t.Errorf("error %q should carry the provider's own message", err)
	}
}

// campaignHandler serves `count` campaigns, paging on offset/length.
func campaignHandler(t *testing.T, count, tracksEach int) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/v2/authentication/token/basic"):
			authResponse(w)

		case strings.Contains(r.URL.Path, "/api/campaign/official"):
			offset := atoiDefault(r.URL.Query().Get("offset"), 0)
			length := atoiDefault(r.URL.Query().Get("length"), 1)

			list := []map[string]any{}
			for i := offset; i < min(offset+length, count); i++ {
				playlist := []map[string]any{}
				for p := range tracksEach {
					playlist = append(playlist, map[string]any{
						"id": p, "position": p,
						"mapUid": fmt.Sprintf("s%d-m%d", i, p),
					})
				}
				list = append(list, map[string]any{
					"id":        1000 + i,
					"seasonUid": fmt.Sprintf("season-%d", i),
					"name":      fmt.Sprintf("Season %d", i),
					// Newer campaigns have a *smaller* offset, so start times
					// descend as offset grows.
					"startTimestamp": 1700000000 - int64(i)*1000000,
					"endTimestamp":   1700500000 - int64(i)*1000000,
					"playlist":       playlist,
				})
			}
			json.NewEncoder(w).Encode(map[string]any{"itemCount": count, "campaignList": list})

		default:
			t.Errorf("unexpected request %s", r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func atoiDefault(s string, def int) int {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return def
	}
	return n
}

func TestCampaigns_PagesThroughItemCountAndSortsOldestFirst(t *testing.T) {
	nadeoStub(t, campaignHandler(t, 25, 2))

	got, raws, err := testClient().Campaigns(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 25 {
		t.Fatalf("got %d campaigns, want 25 — paging stopped early", len(got))
	}
	if len(raws) < 2 {
		t.Errorf("got %d raw pages, want every page kept", len(raws))
	}
	for i := 1; i < len(got); i++ {
		if got[i].StartTimestamp < got[i-1].StartTimestamp {
			t.Fatalf("campaigns not sorted oldest-first at %d", i)
		}
	}
}

// A provider claiming more items than it will serve must not spin forever.
func TestCampaigns_StopsOnAnEmptyPageEvenIfItemCountLies(t *testing.T) {
	var pages int
	nadeoStub(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/v2/authentication/token/basic") {
			authResponse(w)
			return
		}
		pages++
		if pages > 10 {
			t.Fatal("Campaigns did not terminate on an empty page")
		}
		json.NewEncoder(w).Encode(map[string]any{"itemCount": 9999, "campaignList": []any{}})
	})

	got, _, err := testClient().Campaigns(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("got %d campaigns, want 0", len(got))
	}
}

func TestPersonalBests_ChunksAtFiftyMaps(t *testing.T) {
	var batches []int
	nadeoStub(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/v2/authentication/token/basic") {
			authResponse(w)
			return
		}
		var body struct {
			Maps []MapRef `json:"maps"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		batches = append(batches, len(body.Maps))

		out := []Record{}
		for _, m := range body.Maps {
			out = append(out, Record{GroupUID: m.GroupUID, MapUID: m.MapUID, Score: 20000})
		}
		json.NewEncoder(w).Encode(out)
	})

	refs := make([]MapRef, 120)
	for i := range refs {
		refs[i] = MapRef{MapUID: fmt.Sprintf("m%d", i), GroupUID: "s1"}
	}

	got, _, err := testClient().PersonalBests(context.Background(), refs, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Over 50 per request the API silently truncates, so exceeding the cap
	// loses data rather than erroring.
	want := []int{50, 50, 20}
	if fmt.Sprint(batches) != fmt.Sprint(want) {
		t.Errorf("batch sizes = %v, want %v", batches, want)
	}
	if len(got) != 120 {
		t.Errorf("got %d records, want 120", len(got))
	}
}

// Invalid pairs are omitted from the response rather than returned empty, so
// results must be matched by id — never zipped with the request by position.
func TestPersonalBests_MatchesOnMapAndGroupNotPosition(t *testing.T) {
	nadeoStub(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/v2/authentication/token/basic") {
			authResponse(w)
			return
		}
		// Only the third request entry comes back, and out of order.
		json.NewEncoder(w).Encode([]Record{{GroupUID: "s1", MapUID: "m3", Score: 19976}})
	})

	refs := []MapRef{
		{MapUID: "m1", GroupUID: "s1"},
		{MapUID: "m2", GroupUID: "s1"},
		{MapUID: "m3", GroupUID: "s1"},
	}
	got, _, err := testClient().PersonalBests(context.Background(), refs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d records, want 1", len(got))
	}
	if r, ok := got[recordKey("m3", "s1")]; !ok || r.Score != 19976 {
		t.Errorf("record landed on the wrong map: %+v", got)
	}
	if _, ok := got[recordKey("m1", "s1")]; ok {
		t.Error("m1 got a time it never had — response was matched by position")
	}
}

// The endpoint intermittently returns [] for valid parameters.
func TestPersonalBests_RetriesOnceOnAnEmptyResponse(t *testing.T) {
	var calls int
	nadeoStub(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/v2/authentication/token/basic") {
			authResponse(w)
			return
		}
		calls++
		if calls == 1 {
			w.Write([]byte(`[]`))
			return
		}
		json.NewEncoder(w).Encode([]Record{{GroupUID: "s1", MapUID: "m1", Score: 19976}})
	})

	got, _, err := testClient().PersonalBests(context.Background(),
		[]MapRef{{MapUID: "m1", GroupUID: "s1"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("made %d leaderboard calls, want 2 (one retry)", calls)
	}
	if got[recordKey("m1", "s1")].Score != 19976 {
		t.Error("the retry's result was discarded")
	}
}

func TestPersonalBests_TwoEmptyResponsesMeanNoTimeSet(t *testing.T) {
	var calls int
	nadeoStub(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/v2/authentication/token/basic") {
			authResponse(w)
			return
		}
		calls++
		w.Write([]byte(`[]`))
	})

	got, _, err := testClient().PersonalBests(context.Background(),
		[]MapRef{{MapUID: "m1", GroupUID: "s1"}}, nil)
	if err != nil {
		t.Fatalf("an empty leaderboard is not an error: %v", err)
	}
	if calls != 2 {
		t.Errorf("made %d calls, want exactly 2 — no unbounded retrying", calls)
	}
	if len(got) != 0 {
		t.Errorf("got %d records, want 0", len(got))
	}
}

func TestMapInfos_ChunksUnderTheURILengthCap(t *testing.T) {
	var batches []int
	var longest int
	nadeoStub(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/v2/authentication/token/basic") {
			authResponse(w)
			return
		}
		list := strings.Split(r.URL.Query().Get("mapUidList"), ",")
		batches = append(batches, len(list))
		longest = max(longest, len(r.URL.String()))

		out := []MapInfo{}
		for _, uid := range list {
			out = append(out, MapInfo{MapUID: uid, Name: "Map " + uid, AuthorScore: 20000})
		}
		json.NewEncoder(w).Encode(out)
	})

	uids := make([]string, 400)
	for i := range uids {
		uids[i] = fmt.Sprintf("uid%027d", i) // realistic 30-char UIDs
	}
	got, _, err := testClient().MapInfos(context.Background(), uids, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 400 {
		t.Errorf("got %d maps, want 400", len(got))
	}
	for _, n := range batches {
		if n > mapInfoBatchSize {
			t.Errorf("batch of %d exceeds the %d cap", n, mapInfoBatchSize)
		}
	}
	// The real limit is a 414 at 8220 characters of request URI.
	if longest >= 8220 {
		t.Errorf("longest request URI was %d chars, want well under 8220", longest)
	}
}

func TestDo_RetriesOn429(t *testing.T) {
	var calls int
	nadeoStub(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/v2/authentication/token/basic") {
			authResponse(w)
			return
		}
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"itemCount": 0, "campaignList": []any{}})
	})

	c := testClient()
	c.Throttle = time.Nanosecond
	// Not waiting the real backoff: the point is that it retried at all.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, _, err := c.Campaigns(ctx); err != nil {
		t.Fatal(err)
	}
	if calls < 2 {
		t.Errorf("made %d calls, want a retry after 429", calls)
	}
}

// ── Season selection ────────────────────────────────────────────────────────

// campaigns builds one campaign per name, a year apart and oldest first.
//
// Mid-year on purpose: Since compares in the site's timezone (as every date in
// this tool does), so a start of Jan 1 00:00 UTC really does fall in the
// previous year in America/Chicago. That's correct behaviour, but it isn't what
// these tests are about.
func campaigns(names ...string) []Campaign {
	out := make([]Campaign, len(names))
	for i, n := range names {
		out[i] = Campaign{
			SeasonUID:      "uid-" + n,
			Name:           n,
			StartTimestamp: time.Date(2020+i, 7, 1, 12, 0, 0, 0, time.UTC).Unix(),
		}
	}
	return out
}

func TestSelectCampaigns_DefaultsToTheCurrentSeasonOnly(t *testing.T) {
	all := campaigns("Summer 2020", "Fall 2020", "Winter 2021")

	got, err := selectCampaigns(all, FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "Winter 2021" {
		t.Errorf("got %+v, want just the newest season", got)
	}
}

func TestSelectCampaigns_ByNameAndUID(t *testing.T) {
	all := campaigns("Summer 2020", "Fall 2020")

	got, err := selectCampaigns(all, FetchOptions{Seasons: []string{"fall 2020", "uid-Summer 2020"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
	if got[0].Name != "Fall 2020" || got[1].Name != "Summer 2020" {
		t.Errorf("got %v", []string{got[0].Name, got[1].Name})
	}
}

// A typo'd season must abort rather than quietly fetching something else.
func TestSelectCampaigns_UnknownSeasonIsAnError(t *testing.T) {
	_, err := selectCampaigns(campaigns("Fall 2024"), FetchOptions{Seasons: []string{"Fall 2025"}})
	if err == nil {
		t.Fatal("want an error naming the unknown season")
	}
	if !strings.Contains(err.Error(), "Fall 2025") {
		t.Errorf("error %q should name what wasn't found", err)
	}
}

func TestSelectCampaigns_AllAndSince(t *testing.T) {
	all := campaigns("A", "B", "C") // 2020, 2021, 2022

	got, err := selectCampaigns(all, FetchOptions{All: true})
	if err != nil || len(got) != 3 {
		t.Errorf("All: got %d, %v; want 3", len(got), err)
	}

	got, err = selectCampaigns(all, FetchOptions{Since: 2021})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "B" {
		t.Errorf("Since 2021: got %+v", got)
	}
}

// ── End-to-end against the stub ─────────────────────────────────────────────

// fetchStub serves a complete two-season campaign. accountRecords selects
// whether the Core records endpoint answers or fails, so both the preferred and
// the fallback path can be driven through the same fixture. calls counts hits
// per endpoint.
func fetchStub(t *testing.T, accountRecordsWorks bool, calls map[string]int) {
	t.Helper()
	var mu sync.Mutex
	count := func(k string) {
		mu.Lock()
		calls[k]++
		mu.Unlock()
	}

	nadeoStub(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/v2/authentication/token/basic"):
			authResponse(w)

		case strings.Contains(r.URL.Path, "/api/campaign/official"):
			campaignHandler(t, 2, 3)(w, r)

		case strings.Contains(r.URL.Path, "/maps/by-uid/"):
			count("maps")
			out := []MapInfo{}
			for uid := range strings.SplitSeq(r.URL.Query().Get("mapUidList"), ",") {
				out = append(out, MapInfo{
					MapUID: uid, MapID: "id-" + uid, Name: "Track " + uid,
					AuthorScore: 20000, GoldScore: 21000, SilverScore: 23000, BronzeScore: 29000,
				})
			}
			json.NewEncoder(w).Encode(out)

		case strings.Contains(r.URL.Path, "/mapRecords"):
			// The two scopes are two requests, told apart by which filter they
			// send. Serving them differently is the point: track s0-m0 was a
			// silver during its campaign and a gold by the time it was
			// revisited, which is the case the whole two-scope design exists
			// for.
			season := r.URL.Query().Get("seasonIdList") != ""
			if season {
				count("season-records")
			} else {
				count("alltime-records")
			}
			if !accountRecordsWorks {
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"message":"Forbidden."}`))
				return
			}

			mk := func(mapID string, ms, medal int, scope, ts string) AccountRecord {
				rec := AccountRecord{
					MapID: mapID, MapRecordID: "run-" + mapID, Medal: medal,
					ScopeType: scope, GameMode: "TimeAttack", Timestamp: ts,
					URL: "https://core.trackmania.nadeo.live/mapRecords/" + mapID + "/replay",
				}
				rec.RecordScore.Time = ms
				return rec
			}

			out := []AccountRecord{}
			if season {
				out = append(out,
					mk("id-s0-m0", 22000, 2, ScopeSeason, "2020-08-23T23:09:49+00:00"),
					mk("id-s1-m0", 19976, 4, ScopeSeason, "2020-11-02T10:00:00+00:00"),
				)
				secret := mk("id-s0-m1", secretScore, 0, ScopeSeason, "2020-08-24T00:00:00+00:00")
				out = append(out, secret)
			} else {
				out = append(out,
					mk("id-s0-m0", 20500, 3, ScopePersonalBest, "2024-07-28T15:09:27+00:00"),
					mk("id-s1-m0", 19976, 4, ScopePersonalBest, "2020-11-02T10:00:00+00:00"),
					// A season-scoped entry riding along in a mapIdList
					// response, which is exactly what the scope filter is for.
					mk("id-s0-m0", 22000, 2, ScopeSeason, "2020-08-23T23:09:49+00:00"),
				)
			}
			json.NewEncoder(w).Encode(out)

		case strings.Contains(r.URL.Path, "/leaderboard/group/map"):
			count("leaderboard")
			var body struct {
				Maps []MapRef `json:"maps"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			out := []Record{}
			for _, m := range body.Maps {
				if strings.HasSuffix(m.MapUID, "-m0") {
					out = append(out, Record{GroupUID: m.GroupUID, MapUID: m.MapUID, Score: 19976})
				}
			}
			json.NewEncoder(w).Encode(out)

		default:
			t.Errorf("unexpected request %s", r.URL)
		}
	})
}

// trackIn locates a track by season uid and map uid. Campaigns are stored
// oldest-first, so positional lookup would silently test the wrong season.
func trackIn(t *testing.T, rec *model.NadeoRecord, seasonUID, mapUID string) model.NadeoTrack {
	t.Helper()
	for _, c := range rec.Campaigns {
		if c.SeasonUID != seasonUID {
			continue
		}
		for _, tr := range c.Tracks {
			if tr.MapUID == mapUID {
				return tr
			}
		}
		t.Fatalf("season %s has no track %s", seasonUID, mapUID)
	}
	t.Fatalf("record has no season %s", seasonUID)
	return model.NadeoTrack{}
}

func TestFetchRecord_BuildsTracksWithMedalsAndTimes(t *testing.T) {
	calls := map[string]int{}
	fetchStub(t, true, calls)

	rec, err := FetchRecord(context.Background(), testClient(), FetchOptions{All: true})
	if err != nil {
		t.Fatal(err)
	}
	if rec.LastError != "" {
		t.Fatalf("LastError = %q", rec.LastError)
	}
	if rec.ID != testAccountID {
		t.Errorf("ID = %q, want the account id", rec.ID)
	}
	if len(rec.Campaigns) != 2 {
		t.Fatalf("got %d campaigns, want 2", len(rec.Campaigns))
	}

	// Campaigns come back oldest-first, so index is not season number — look
	// them up by uid rather than assuming an order.
	// s0-m0: silver when the campaign closed, gold once it was revisited. Both
	// figures are kept, because neither can be recovered from the other.
	first := trackIn(t, rec, "season-0", "s0-m0")
	if first.SeasonBestMs != 22000 || first.SeasonMedal() != "silver" {
		t.Errorf("season best = %dms / %s, want 22000 / silver", first.SeasonBestMs, first.SeasonMedal())
	}
	if first.PersonalBestMs != 20500 || first.Medal() != "gold" {
		t.Errorf("all-time best = %dms / %s, want 20500 / gold", first.PersonalBestMs, first.Medal())
	}
	if !first.ImprovedSinceSeason() {
		t.Error("ImprovedSinceSeason = false, want true — silver during the season, gold after")
	}
	if first.SeasonDrivenAt != "2020-08-23" || first.DrivenAt != "2024-07-28" {
		t.Errorf("dates = season %q / all-time %q", first.SeasonDrivenAt, first.DrivenAt)
	}
	if first.Name == "" || first.MapID == "" {
		t.Errorf("map metadata not attached: %+v", first)
	}
	if first.RecordID == "" || first.ReplayURL == "" || first.NadeoMedal != 3 {
		t.Errorf("run metadata not captured: %+v", first)
	}

	// A track whose medal never moved must not be reported as improved.
	unchanged := trackIn(t, rec, "season-1", "s1-m0")
	if unchanged.PersonalBestMs != unchanged.SeasonBestMs || unchanged.ImprovedSinceSeason() {
		t.Errorf("unrevisited track reported as improved: %+v", unchanged)
	}

	// s0-m1 came back with the secret-threshold sentinel.
	second := trackIn(t, rec, "season-0", "s0-m1")
	if second.Driven() {
		t.Errorf("track 2 should have no time: %+v", second)
	}
	// Thresholds are captured even for an undriven track, so a later time can
	// be scored without re-fetching.
	if second.AuthorMs != 20000 {
		t.Error("undriven track lost its medal thresholds")
	}

	// When account records answer, the leaderboard shouldn't be called at all —
	// that's the request saving this path was for. One request per scope.
	if calls["leaderboard"] != 0 {
		t.Errorf("leaderboard called %d times despite account records working", calls["leaderboard"])
	}
	if calls["season-records"] != 1 || calls["alltime-records"] != 1 {
		t.Errorf("record calls = %v, want one per scope", calls)
	}
}

// A mapIdList request carries no season filter, so season-scoped entries for
// those maps come back alongside the all-time ones. Taking them at face value
// would file a frozen campaign time as the current best.
func TestFetchRecord_AllTimePassIgnoresSeasonScopedEntries(t *testing.T) {
	calls := map[string]int{}
	fetchStub(t, true, calls)

	rec, err := FetchRecord(context.Background(), testClient(), FetchOptions{All: true})
	if err != nil {
		t.Fatal(err)
	}
	// The stub returns both a 20500 PersonalBest and a 22000 Season entry for
	// this map in the all-time response.
	if got := trackIn(t, rec, "season-0", "s0-m0").PersonalBestMs; got != 20500 {
		t.Errorf("PersonalBestMs = %d, want 20500 — a Season entry was mistaken for the all-time best", got)
	}
}

// 4294967295 is math.MaxUint32, the sentinel for a map with a secret threshold
// score — not a 49-day lap.
func TestFetchRecord_SecretThresholdScoreIsNotATime(t *testing.T) {
	calls := map[string]int{}
	fetchStub(t, true, calls)

	rec, err := FetchRecord(context.Background(), testClient(), FetchOptions{All: true})
	if err != nil {
		t.Fatal(err)
	}
	// id-s0-m1 came back with the sentinel; it must read as undriven.
	for _, camp := range rec.Campaigns {
		for _, tr := range camp.Tracks {
			if tr.PersonalBestMs == secretScore {
				t.Fatalf("sentinel recorded as a real time: %+v", tr)
			}
		}
	}
	if tr := rec.Campaigns[0].Tracks[1]; tr.Driven() {
		t.Errorf("track with a secret threshold score should be undriven, got %dms", tr.PersonalBestMs)
	}
}

// A 403 from account records is not fatal — the leaderboard can still finish
// the capture, just more slowly and without dates.
func TestFetchRecord_FallsBackToTheLeaderboard(t *testing.T) {
	calls := map[string]int{}
	fetchStub(t, false, calls)

	rec, err := FetchRecord(context.Background(), testClient(), FetchOptions{All: true})
	if err != nil {
		t.Fatal(err)
	}
	if rec.LastError != "" {
		t.Fatalf("LastError = %q — the fallback should have completed the capture", rec.LastError)
	}
	if calls["leaderboard"] == 0 {
		t.Fatal("leaderboard was never called after account records failed")
	}
	first := rec.Campaigns[0].Tracks[0]
	if first.PersonalBestMs != 19976 {
		t.Errorf("PersonalBestMs = %d, want 19976 from the fallback", first.PersonalBestMs)
	}
	if first.DrivenAt != "" {
		t.Errorf("DrivenAt = %q, want empty — the leaderboard reports no date", first.DrivenAt)
	}
}

// The house contract: a provider failure is recorded, not returned.
func TestFetchRecord_ProviderFailureIsRecordedNotReturned(t *testing.T) {
	nadeoStub(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/v2/authentication/token/basic") {
			authResponse(w)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message":"nope"}`))
	})

	rec, err := FetchRecord(context.Background(), testClient(), FetchOptions{All: true})
	if err != nil {
		t.Fatalf("a provider failure must not abort the run: %v", err)
	}
	if rec.LastError == "" {
		t.Error("LastError not recorded")
	}
	if rec.LastAttempt == "" {
		t.Error("LastAttempt not recorded")
	}
	if rec.Fetched != "" {
		t.Errorf("Fetched = %q, want empty — no data arrived", rec.Fetched)
	}
}

// Bad credentials should abort rather than being written into the archive as a
// transient provider error.
func TestFetchRecord_BadCredentialsAbortTheRun(t *testing.T) {
	nadeoStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Invalid credentials."}`))
	})

	if _, err := FetchRecord(context.Background(), testClient(), FetchOptions{All: true}); err == nil {
		t.Fatal("want an error for bad credentials")
	}
}

func TestFetchRecord_EmitsProgressForEachStage(t *testing.T) {
	nadeoStub(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/v2/authentication/token/basic"):
			authResponse(w)
		case strings.Contains(r.URL.Path, "/api/campaign/official"):
			campaignHandler(t, 1, 2)(w, r)
		case strings.Contains(r.URL.Path, "/maps/by-uid/"):
			w.Write([]byte(`[]`))
		default:
			w.Write([]byte(`[]`))
		}
	})

	seen := map[Stage]bool{}
	_, err := FetchRecord(context.Background(), testClient(), FetchOptions{
		All:     true,
		OnEvent: func(e Event) { seen[e.Stage] = true },
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []Stage{StageAuth, StageCampaigns, StageMaps, StageRecords, StageSeason} {
		if !seen[want] {
			t.Errorf("no progress event for stage %q", want)
		}
	}
}
