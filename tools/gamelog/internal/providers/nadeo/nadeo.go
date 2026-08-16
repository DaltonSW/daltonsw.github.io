// Package nadeo talks to Nadeo's own Trackmania (2020) web services,
// documented (unofficially, by the community) at
// https://webservices.openplanet.dev. There is no first-party documentation.
//
// Auth is the *service account* flow: a login/password pair created at
// trackmania.com and bound to a real Ubisoft account, exchanged for a
// short-lived access token. The older Ubisoft-ticket flow stopped working in
// April 2026 and is not implemented.
//
// Two things about this API drive the shape of everything below:
//
//   - **There are two audiences, and tokens are not interchangeable.** The Live
//     API (campaigns, leaderboards) needs a NadeoLiveServices token; the Core
//     API (map metadata, medal times) needs a NadeoServices one. Both are
//     needed for a complete capture, so both are fetched and cached separately.
//   - **The service account inherits the bound user's identity.** That's what
//     makes the personal-records endpoint work at all: it returns *the
//     authenticated account's* times with no account id in the request. It also
//     means abusing the rate limit affects a real player's account, which is
//     why nadeoMinInterval is deliberately conservative.
//
// Tokens live in memory for the lifetime of a Client and are never written to
// disk, matching the standing decision for PSN in KNOWN-ISSUES.md.
package nadeo

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.dalton.dog/gamelog/internal/model"
)

// Endpoint bases. Vars rather than consts so tests can point them at a local
// server; never reassigned in production code.
var (
	CoreBaseURL = "https://prod.trackmania.core.nadeo.online"
	LiveBaseURL = "https://live-services.trackmania.nadeo.live"
)

// Audiences. Which one an endpoint needs is a property of the endpoint, not of
// the request, so it's threaded through do() rather than held on the Client.
const (
	AudienceCore = "NadeoServices"
	AudienceLive = "NadeoLiveServices"
)

// nadeoUserAgent identifies this project and a contact address. Nadeo's
// community docs ask for both, and an anonymous client is the kind of thing
// that gets an IP range blocked rather than politely rate-limited.
const nadeoUserAgent = "gamelog (https://dalton.dog) / Dalton Williams / email@dalton.dog"

// nadeoMinInterval paces requests. There is no published hard limit; the
// community guidance is to stay around 2 requests/second for bursts. A full
// backfill is ~40 requests, so 500ms costs about 20 seconds and keeps a real
// player's account well clear of anything that looks like abuse.
const nadeoMinInterval = 500 * time.Millisecond

// Batch limits, both imposed by the API rather than chosen:
//
//   - The leaderboard endpoint hard-caps at 50 maps per request and silently
//     truncates beyond that, so exceeding it loses data rather than erroring.
//   - The map-info endpoint has no documented item cap but returns 414 once the
//     request URI reaches 8220 characters (~300 UIDs). 150 is half that, which
//     leaves room for longer UIDs than the ones seen so far.
const (
	recordBatchSize  = 50
	mapInfoBatchSize = 150
)

// Client talks to Nadeo's services as one service account.
type Client struct {
	Login    string
	Password string
	HTTP     *http.Client

	// Throttle is the minimum gap between requests. Zero means
	// nadeoMinInterval; set it explicitly in tests to avoid sleeping.
	Throttle time.Duration
	lastReq  time.Time

	// DumpRaw, when set, is a directory every verbatim response body is written
	// into. It exists so a single scoped live run can produce the fixtures this
	// package's tests are built from, without anyone having to re-run the API
	// to inspect a response shape.
	DumpRaw string

	mu     sync.Mutex
	tokens map[string]*token
	dumped map[string]int
}

type token struct {
	access    string
	expires   time.Time
	accountID string
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	// Longer than the other providers': a 150-map info response is large, and
	// a full campaign list is not small either.
	return &http.Client{Timeout: 30 * time.Second}
}

func (c *Client) interval() time.Duration {
	if c.Throttle != 0 {
		return c.Throttle
	}
	return nadeoMinInterval
}

func (c *Client) throttle(ctx context.Context) error {
	if wait := c.interval() - time.Since(c.lastReq); wait > 0 && !c.lastReq.IsZero() {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// Configured reports whether this client has credentials to try at all.
func (c *Client) Configured() bool { return c.Login != "" && c.Password != "" }

// token returns a valid access token for one audience, authenticating on first
// use and re-authenticating shortly before expiry. Tokens are cached per
// audience for the client's lifetime and never persisted.
func (c *Client) token(ctx context.Context, audience string) (*token, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.tokens == nil {
		c.tokens = map[string]*token{}
	}
	// A minute of headroom, so a token can't expire between the check and the
	// request it's about to authenticate.
	if t, ok := c.tokens[audience]; ok && time.Until(t.expires) > time.Minute {
		return t, nil
	}

	t, err := c.authenticate(ctx, audience)
	if err != nil {
		return nil, err
	}
	c.tokens[audience] = t
	return t, nil
}

// authenticate performs the service-account basic-auth exchange.
func (c *Client) authenticate(ctx context.Context, audience string) (*token, error) {
	if !c.Configured() {
		return nil, fmt.Errorf("nadeo: no service account configured — set NADEO_SERVICE_LOGIN and NADEO_SERVICE_PASSWORD")
	}
	if err := c.throttle(ctx); err != nil {
		return nil, err
	}

	body, err := json.Marshal(map[string]string{"audience": audience})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		CoreBaseURL+"/v2/authentication/token/basic", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", nadeoUserAgent)
	// The login here is the *service account's*, not the underlying Ubisoft
	// account's — they are different strings and the Ubisoft one will 401.
	req.SetBasicAuth(c.Login, c.Password)

	resp, err := c.httpClient().Do(req)
	c.lastReq = time.Now()
	if err != nil {
		return nil, fmt.Errorf("nadeo: authenticating: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("nadeo: reading auth response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// 401 here means either an unknown login or a bad password; Nadeo
		// distinguishes them in the body, which is worth surfacing verbatim.
		return nil, fmt.Errorf("nadeo: authenticating: status %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}

	var parsed struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("nadeo: decoding auth response: %w", err)
	}
	if parsed.AccessToken == "" {
		return nil, fmt.Errorf("nadeo: auth response carried no accessToken")
	}

	claims, err := decodeTokenClaims(parsed.AccessToken)
	if err != nil {
		return nil, err
	}
	return &token{
		access:    parsed.AccessToken,
		expires:   time.Unix(claims.Exp, 0),
		accountID: model.FirstNonEmpty(claims.AccountID, claims.Sub),
	}, nil
}

type tokenClaims struct {
	Exp       int64  `json:"exp"`
	AccountID string `json:"accountId"`
	Sub       string `json:"sub"`
}

// decodeTokenClaims reads the JWT payload without verifying the signature —
// this is the client reading its own token to find out who it is and when it
// expires, not a server deciding whether to trust one.
func decodeTokenClaims(accessToken string) (tokenClaims, error) {
	var claims tokenClaims
	parts := strings.Split(accessToken, ".")
	if len(parts) != 3 {
		return claims, fmt.Errorf("nadeo: access token is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return claims, fmt.Errorf("nadeo: decoding token payload: %w", err)
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return claims, fmt.Errorf("nadeo: decoding token claims: %w", err)
	}
	return claims, nil
}

// AccountID is the authenticated account's UUID, read from the token's own
// claims rather than a separate call.
func (c *Client) AccountID(ctx context.Context) (string, error) {
	t, err := c.token(ctx, AudienceLive)
	if err != nil {
		return "", err
	}
	if t.accountID == "" {
		return "", fmt.Errorf("nadeo: token carried no account id — is this a dedicated server account rather than a service account?")
	}
	return t.accountID, nil
}

// do issues a throttled, token-authenticated request, retrying on 429 with
// backoff. It returns the body and status so callers can interpret the API's
// several success-shaped failure modes themselves.
func (c *Client) do(ctx context.Context, method, reqURL, audience string, body []byte, dumpAs string) ([]byte, int, error) {
	backoff := 2 * time.Second
	for attempt := 0; ; attempt++ {
		t, err := c.token(ctx, audience)
		if err != nil {
			return nil, 0, err
		}
		if err := c.throttle(ctx); err != nil {
			return nil, 0, err
		}

		var reader io.Reader
		if body != nil {
			reader = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, reqURL, reader)
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("Authorization", "nadeo_v1 t="+t.access)
		req.Header.Set("User-Agent", nadeoUserAgent)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.httpClient().Do(req)
		c.lastReq = time.Now()
		if err != nil {
			return nil, 0, fmt.Errorf("nadeo: %s %s: %w", method, reqURL, err)
		}
		raw, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, resp.StatusCode, fmt.Errorf("nadeo: reading response: %w", readErr)
		}

		if resp.StatusCode == http.StatusTooManyRequests && attempt < 4 {
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			}
			backoff *= 2
			continue
		}
		c.dump(dumpAs, raw)
		return raw, resp.StatusCode, nil
	}
}

// dump writes a verbatim response body into DumpRaw, numbering repeats so a
// paged or batched call keeps every page.
func (c *Client) dump(name string, raw []byte) {
	if c.DumpRaw == "" || name == "" {
		return
	}
	c.mu.Lock()
	if c.dumped == nil {
		c.dumped = map[string]int{}
	}
	n := c.dumped[name]
	c.dumped[name]++
	c.mu.Unlock()

	if err := os.MkdirAll(c.DumpRaw, 0o755); err != nil {
		return
	}
	path := filepath.Join(c.DumpRaw, fmt.Sprintf("%s-%02d.json", name, n))
	// Best-effort: dumping fixtures must never fail a capture.
	_ = os.WriteFile(path, raw, 0o644)
}

// ── Campaigns ───────────────────────────────────────────────────────────────

// Campaign is one official seasonal campaign as the Live API reports it.
type Campaign struct {
	ID             int    `json:"id"`
	SeasonUID      string `json:"seasonUid"`
	Name           string `json:"name"`
	StartTimestamp int64  `json:"startTimestamp"`
	EndTimestamp   int64  `json:"endTimestamp"`
	Playlist       []struct {
		ID       int    `json:"id"`
		Position int    `json:"position"`
		MapUID   string `json:"mapUid"`
	} `json:"playlist"`
}

// campaignPageSize is how many campaigns are asked for at a time. There are
// fewer than two dozen in total, so this is one or two requests either way.
const campaignPageSize = 20

// Campaigns enumerates every official campaign, oldest first, along with the
// verbatim response pages.
//
// `offset` counts *backwards* from the current campaign, so paging walks from
// newest to oldest; the result is sorted by start time afterwards rather than
// relying on the order the API happens to return.
func (c *Client) Campaigns(ctx context.Context) ([]Campaign, []json.RawMessage, error) {
	var all []Campaign
	var raws []json.RawMessage

	for offset := 0; ; offset += campaignPageSize {
		q := url.Values{}
		q.Set("length", strconv.Itoa(campaignPageSize))
		q.Set("offset", strconv.Itoa(offset))

		raw, status, err := c.do(ctx, http.MethodGet,
			LiveBaseURL+"/api/campaign/official?"+q.Encode(), AudienceLive, nil, "campaigns")
		if err != nil {
			return nil, nil, err
		}
		if status != http.StatusOK {
			return nil, nil, fmt.Errorf("nadeo: campaigns: status %s: %s", http.StatusText(status), strings.TrimSpace(string(raw)))
		}

		var page struct {
			ItemCount    int        `json:"itemCount"`
			CampaignList []Campaign `json:"campaignList"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, nil, fmt.Errorf("nadeo: decoding campaigns: %w", err)
		}
		raws = append(raws, json.RawMessage(raw))
		all = append(all, page.CampaignList...)

		// Both guards matter: an empty page ends the walk even if itemCount
		// lies, and itemCount ends it without a wasted empty request.
		if len(page.CampaignList) == 0 || len(all) >= page.ItemCount {
			break
		}
	}

	sort.SliceStable(all, func(i, j int) bool { return all[i].StartTimestamp < all[j].StartTimestamp })
	return all, raws, nil
}

// ── Map metadata ────────────────────────────────────────────────────────────

// MapInfo is one map as the Core API reports it. Medal thresholds are in
// milliseconds, fastest first.
type MapInfo struct {
	MapUID       string `json:"mapUid"`
	MapID        string `json:"mapId"`
	Name         string `json:"name"`
	Author       string `json:"author"`
	ThumbnailURL string `json:"thumbnailUrl"`
	AuthorScore  int    `json:"authorScore"`
	GoldScore    int    `json:"goldScore"`
	SilverScore  int    `json:"silverScore"`
	BronzeScore  int    `json:"bronzeScore"`
}

// MapInfos looks up metadata for many maps, chunked under the URI length cap.
// The result is keyed by mapUid; a map the API doesn't return is simply absent.
func (c *Client) MapInfos(ctx context.Context, uids []string, onProgress func(done int)) (map[string]MapInfo, []json.RawMessage, error) {
	out := make(map[string]MapInfo, len(uids))
	var raws []json.RawMessage

	for start := 0; start < len(uids); start += mapInfoBatchSize {
		end := min(start+mapInfoBatchSize, len(uids))
		batch := uids[start:end]

		q := url.Values{}
		q.Set("mapUidList", strings.Join(batch, ","))
		raw, status, err := c.do(ctx, http.MethodGet,
			CoreBaseURL+"/maps/by-uid/?"+q.Encode(), AudienceCore, nil, "maps")
		if err != nil {
			return nil, nil, err
		}
		if status != http.StatusOK {
			return nil, nil, fmt.Errorf("nadeo: map info: status %d: %s", status, strings.TrimSpace(string(raw)))
		}

		var page []MapInfo
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, nil, fmt.Errorf("nadeo: decoding map info: %w", err)
		}
		raws = append(raws, json.RawMessage(raw))
		for _, m := range page {
			out[m.MapUID] = m
		}
		if onProgress != nil {
			onProgress(end)
		}
	}
	return out, raws, nil
}

// ── Personal records: the Core account-records path ─────────────────────────

// secretScore is what the API returns instead of a time when a map author has
// set a secret threshold score. It is math.MaxUint32, not a 49-day lap, and
// must never be recorded as one.
const secretScore = 4294967295

// seasonIDBatchSize keeps the request URI under the 8220-character 414 limit.
// Season ids are 36-character UUIDs, so 100 is ~3.7KB — comfortable, and more
// than the total number of official campaigns that has ever existed.
const seasonIDBatchSize = 100

// AccountRecord is one of the authenticated account's runs, as the Core API
// reports it. Richer than the Live leaderboard's equivalent: it carries when
// the run was set and a link to the replay, neither of which the leaderboard
// endpoint returns.
type AccountRecord struct {
	AccountID   string `json:"accountId"`
	MapID       string `json:"mapId"`
	MapRecordID string `json:"mapRecordId"`

	// Medal is Nadeo's own grade: 0 none, 1 bronze, 2 silver, 3 gold,
	// 4 author. Confirmed against a live capture, where it agreed with the
	// medal derived from the map's thresholds on all 25 tracks.
	Medal   int  `json:"medal"`
	Removed bool `json:"removed"`

	// ScopeType/ScopeID say which leaderboard this time belongs to. Requesting
	// by seasonIdList yields "Season" scoped to that campaign — the best time
	// set while it was open — rather than "PersonalBest", which is the all-time
	// best on the map. They can differ for a campaign replayed after it closed.
	ScopeType string `json:"scopeType"`
	ScopeID   string `json:"scopeId"`

	// GameMode is "TimeAttack" for campaign Race maps (confirmed live).
	GameMode string `json:"gameMode"`

	Timestamp   string `json:"timestamp"` // RFC3339
	URL         string `json:"url"`
	RecordScore struct {
		Time  int `json:"time"`
		Score int `json:"score"`
	} `json:"recordScore"`
}

// Usable reports whether this entry carries a real time.
func (r AccountRecord) Usable() bool {
	return !r.Removed && r.RecordScore.Time > 0 && r.RecordScore.Time != secretScore
}

// Record scopes. A time is filed under one leaderboard or the other, and the
// two answer different questions: ScopeSeason is what was achieved while a
// campaign was open (frozen when it closes), ScopePersonalBest is the all-time
// best on the map (what the game shows, and what moves when an old track is
// revisited).
const (
	ScopeSeason       = "Season"
	ScopePersonalBest = "PersonalBest"
)

// SeasonRecords fetches the account's times as they stood within the given
// campaigns — the frozen, season-scoped leaderboard.
func (c *Client) SeasonRecords(ctx context.Context, accountID string, seasonUIDs []string, onProgress func(done int)) (map[string]AccountRecord, []json.RawMessage, error) {
	return c.accountRecords(ctx, accountID, "seasonIdList", seasonUIDs, seasonIDBatchSize, ScopeSeason, "season-records", onProgress)
}

// AllTimeRecords fetches the account's current best on each map, regardless of
// when it was driven.
//
// Note this takes mapIds, not mapUids — the two are different identifiers, and
// the map-metadata call is what translates between them.
func (c *Client) AllTimeRecords(ctx context.Context, accountID string, mapIDs []string, onProgress func(done int)) (map[string]AccountRecord, []json.RawMessage, error) {
	return c.accountRecords(ctx, accountID, "mapIdList", mapIDs, mapInfoBatchSize, ScopePersonalBest, "alltime-records", onProgress)
}

// accountRecords fetches records for the authenticated account, keyed by mapId
// and filtered to one scope.
//
// This is the preferred path over PersonalBests: a handful of requests covers
// every track rather than one per 50 maps, and the response carries the run's
// timestamp and replay. The trade is that it works *only* for the authenticated
// account (others get a 403) and reports no zone rankings.
//
// Documented constraints that are load-bearing here:
//   - mapIdList and seasonIdList must not both be sent; both filter every
//     result, so together they intersect rather than union. That's why the two
//     scopes are two passes rather than one.
//   - The endpoint returns at most the 1,000 most recent records. Comfortably
//     above the ~450 official campaign tracks, but it is a ceiling rather than
//     a page — there is no way to ask for the next 1,000.
//   - gameMode is deliberately not sent. The docs recommend it "for
//     consistency", but the correct value differs per map (Race vs Stunt vs a
//     map with clones), and a wrong filter returns an empty list while no
//     filter returns everything — harmless, since results are filtered by
//     scope and indexed by mapId against the maps actually asked for.
func (c *Client) accountRecords(ctx context.Context, accountID, param string, values []string, batchSize int, scope, dumpAs string, onProgress func(done int)) (map[string]AccountRecord, []json.RawMessage, error) {
	out := make(map[string]AccountRecord)
	var raws []json.RawMessage

	for start := 0; start < len(values); start += batchSize {
		end := min(start+batchSize, len(values))

		q := url.Values{}
		q.Set(param, strings.Join(values[start:end], ","))
		raw, status, err := c.do(ctx, http.MethodGet,
			fmt.Sprintf("%s/v2/accounts/%s/mapRecords?%s", CoreBaseURL, url.PathEscape(accountID), q.Encode()),
			AudienceCore, nil, dumpAs)
		if err != nil {
			return nil, nil, err
		}
		if status != http.StatusOK {
			return nil, nil, fmt.Errorf("nadeo: account records (%s): status %d: %s", scope, status, strings.TrimSpace(string(raw)))
		}

		var page []AccountRecord
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, nil, fmt.Errorf("nadeo: decoding account records: %w", err)
		}
		raws = append(raws, json.RawMessage(raw))
		for _, r := range page {
			// Filtering on scope rather than trusting the query parameter to
			// have done it: a mapIdList request has no season filter at all, so
			// season-scoped entries for those maps can come back alongside the
			// all-time ones and would otherwise be mistaken for them.
			if !r.Usable() || r.ScopeType != scope {
				continue
			}
			// Keep the fastest if a map still appears twice — same rule as the
			// archive's merge.
			if prev, ok := out[r.MapID]; ok && prev.RecordScore.Time <= r.RecordScore.Time {
				continue
			}
			out[r.MapID] = r
		}
		if onProgress != nil {
			onProgress(end)
		}
	}
	return out, raws, nil
}

// ── Personal records: the Live leaderboard fallback ─────────────────────────

// MapRef is one (map, season) pair to look a record up for.
type MapRef struct {
	MapUID   string `json:"mapUid"`
	GroupUID string `json:"groupUid"`
}

// Record is the authenticated account's time on one map, in one season.
type Record struct {
	GroupUID string `json:"groupUid"`
	MapUID   string `json:"mapUid"`
	Score    int    `json:"score"` // milliseconds
	Zones    []struct {
		ZoneName string `json:"zoneName"`
		Ranking  struct {
			Position int `json:"position"`
		} `json:"ranking"`
	} `json:"zones"`
}

// WorldRank is the position on the global leaderboard, or 0 if not reported.
func (r Record) WorldRank() int {
	for _, z := range r.Zones {
		if z.ZoneName == "World" {
			return z.Ranking.Position
		}
	}
	return 0
}

// recordKey is how a response entry is matched back to its request. Matching on
// position would be wrong: an invalid or unknown pair is *omitted* from the
// response rather than returned empty, so responses are not 1:1 with requests.
func recordKey(mapUID, groupUID string) string { return groupUID + "\x00" + mapUID }

// PersonalBests fetches the authenticated account's records for many
// (map, season) pairs, in batches under the API's 50-map cap.
func (c *Client) PersonalBests(ctx context.Context, refs []MapRef, onProgress func(done int)) (map[string]Record, []json.RawMessage, error) {
	out := make(map[string]Record, len(refs))
	var raws []json.RawMessage

	for start := 0; start < len(refs); start += recordBatchSize {
		end := min(start+recordBatchSize, len(refs))
		batch := refs[start:end]

		page, raw, err := c.recordBatch(ctx, batch)
		if err != nil {
			return nil, nil, err
		}
		raws = append(raws, raw...)
		for _, r := range page {
			out[recordKey(r.MapUID, r.GroupUID)] = r
		}
		if onProgress != nil {
			onProgress(end)
		}
	}
	return out, raws, nil
}

// recordBatch issues one leaderboard POST, retrying once on an empty response.
//
// This endpoint is known to return `[]` for valid parameters, inconsistently
// and for undetermined reasons. Without the retry an empty batch would be
// recorded as "no time set" on 50 tracks at once — which the merge would then
// (correctly) ignore, but which would still make a first-ever capture silently
// incomplete.
func (c *Client) recordBatch(ctx context.Context, batch []MapRef) ([]Record, []json.RawMessage, error) {
	var raws []json.RawMessage

	for range 2 {
		body, err := json.Marshal(map[string][]MapRef{"maps": batch})
		if err != nil {
			return nil, nil, err
		}
		raw, status, err := c.do(ctx, http.MethodPost,
			LiveBaseURL+"/api/token/leaderboard/group/map", AudienceLive, body, "records")
		if err != nil {
			return nil, nil, err
		}
		if status != http.StatusOK {
			return nil, nil, fmt.Errorf("nadeo: personal bests: status %d: %s", status, strings.TrimSpace(string(raw)))
		}

		var page []Record
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, nil, fmt.Errorf("nadeo: decoding personal bests: %w", err)
		}
		raws = append(raws, json.RawMessage(raw))
		if len(page) > 0 {
			return page, raws, nil
		}
	}
	// Two empty responses in a row is taken at face value: these tracks have no
	// times on them.
	return nil, raws, nil
}
