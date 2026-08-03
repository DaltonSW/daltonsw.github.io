// Package psn talks to Sony's own PlayStation Network trophy API (v2),
// documented (unofficially, but by reverse-engineering the PS App's own
// traffic) at https://andshrew.github.io/PlayStation-Trophies/#/APIv2. There
// is no first-party developer documentation — Sony has never published one.
//
// Auth is an npsso session cookie (obtained by logging into
// store.playstation.com, then visiting ca.account.sony.com/api/v1/ssocookie)
// exchanged for a short-lived OAuth bearer token. The npsso value is good for
// ~2 months; the exchanged access token for ~60 minutes, which is longer than
// any single CLI run needs, so the exchange happens once per PSNClient and is
// never persisted or refreshed across runs.
package psn

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.dalton.dog/gamelog/internal/model"
)

// Endpoint bases. Vars rather than consts so tests can point them at a local
// server; never reassigned in production code.
var (
	AuthorizeURL  = "https://ca.account.sony.com/api/authz/v3/oauth/authorize"
	TokenURL      = "https://ca.account.sony.com/api/authz/v3/oauth/token"
	TrophyBaseURL = "https://m.np.playstation.com/api/trophy/v1"
)

// oauthClientBasic is the PlayStation App's own public OAuth client
// id:secret pair (base64, per the andshrew docs) — not a per-user secret.
// Every client of this API authenticates as the PS App; what's actually
// scoped to the user is the npsso cookie exchanged alongside it.
const (
	oauthClientID    = "09515159-7237-4370-9b40-3806e67c0891"
	oauthClientBasic = "Basic MDk1MTUxNTktNzIzNy00MzcwLTliNDAtMzgwNmU2N2MwODkxOnVjUGprYTV0bnRCMktxc1A="
	oauthRedirectURI = "com.scee.psxandroid.scecompcall://redirect"
)

// psnMinInterval throttles requests against Sony's rate limit, reported
// (unconfirmed) at ~300 requests per 15 minutes. Generous for a handful of
// PSN games, but --all refreshing a couple dozen at 2-3 calls each is worth
// pacing.
const psnMinInterval = 250 * time.Millisecond

// PSNClient talks to Sony's trophy API for one account.
type PSNClient struct {
	Npsso string
	HTTP  *http.Client

	// Throttle is the minimum gap between requests. Zero means
	// psnMinInterval; set it explicitly in tests to avoid sleeping.
	Throttle time.Duration
	lastReq  time.Time

	tokenOnce   sync.Once
	accessToken string
	tokenErr    error
}

func (c *PSNClient) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (c *PSNClient) interval() time.Duration {
	if c.Throttle != 0 {
		return c.Throttle
	}
	return psnMinInterval
}

func (c *PSNClient) throttle(ctx context.Context) error {
	if wait := c.interval() - time.Since(c.lastReq); wait > 0 && !c.lastReq.IsZero() {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// token returns the bearer token for this client, exchanging npsso for one on
// first use and reusing it for the rest of the client's lifetime.
func (c *PSNClient) token(ctx context.Context) (string, error) {
	c.tokenOnce.Do(func() {
		c.accessToken, c.tokenErr = c.exchangeToken(ctx)
	})
	return c.accessToken, c.tokenErr
}

// exchangeToken performs the two-step npsso -> code -> access_token dance.
func (c *PSNClient) exchangeToken(ctx context.Context) (string, error) {
	code, err := c.authorize(ctx)
	if err != nil {
		return "", err
	}
	return c.tokenFromCode(ctx, code)
}

// authorize trades the npsso cookie for a short-lived authorization code.
// Sony hands the code back as a query parameter on a 302 redirect to a
// (non-resolvable) app-only URI, so the request must not follow the
// redirect — it has to read `code` off the Location header instead.
func (c *PSNClient) authorize(ctx context.Context) (string, error) {
	q := url.Values{}
	q.Set("access_type", "offline")
	q.Set("client_id", oauthClientID)
	q.Set("response_type", "code")
	q.Set("scope", "psn:mobile.v2.core psn:clientapp")
	q.Set("redirect_uri", oauthRedirectURI)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, AuthorizeURL+"?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Cookie", "npsso="+c.Npsso)

	// A copy of the normal client with redirects disabled, so the 302 itself
	// (and its Location header) comes back instead of being followed.
	noRedirect := *c.httpClient()
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}

	resp, err := noRedirect.Do(req)
	if err != nil {
		return "", fmt.Errorf("psn: authorize: %w", err)
	}
	defer resp.Body.Close()

	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", fmt.Errorf("psn: authorize: no redirect in response — check PSN_NPSSO")
	}
	u, err := url.Parse(loc)
	if err != nil {
		return "", fmt.Errorf("psn: authorize: parsing redirect: %w", err)
	}
	code := u.Query().Get("code")
	if !strings.HasPrefix(code, "v3") {
		return "", fmt.Errorf("psn: authorize: no authorization code in redirect — check PSN_NPSSO")
	}
	return code, nil
}

// tokenFromCode exchanges an authorization code for a bearer access token.
func (c *PSNClient) tokenFromCode(ctx context.Context, code string) (string, error) {
	body := url.Values{}
	body.Set("code", code)
	body.Set("redirect_uri", oauthRedirectURI)
	body.Set("grant_type", "authorization_code")
	body.Set("token_format", "jwt")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, TokenURL, strings.NewReader(body.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", oauthClientBasic)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("psn: token exchange: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("psn: reading token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("psn: token exchange: unexpected status %s", resp.Status)
	}

	var parsed struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("psn: decoding token response: %w", err)
	}
	if parsed.AccessToken == "" {
		return "", fmt.Errorf("psn: token exchange: response carried no access_token")
	}
	return parsed.AccessToken, nil
}

// get issues a throttled, bearer-authenticated GET against the trophy API.
func (c *PSNClient) get(ctx context.Context, reqURL string) ([]byte, error) {
	if err := c.throttle(ctx); err != nil {
		return nil, err
	}
	token, err := c.token(ctx)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient().Do(req)
	c.lastReq = time.Now()
	if err != nil {
		return nil, fmt.Errorf("psn: %s: %w", reqURL, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("psn: reading %s: %w", reqURL, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("psn: %s: unexpected status %s", reqURL, resp.Status)
	}
	return respBody, nil
}

// TrophyCounts is a title's or trophy group's trophy count, broken down by
// tier.
type TrophyCounts struct {
	Bronze   int `json:"bronze"`
	Silver   int `json:"silver"`
	Gold     int `json:"gold"`
	Platinum int `json:"platinum"`
}

// Sum totals every tier.
func (t TrophyCounts) Sum() int { return t.Bronze + t.Silver + t.Gold + t.Platinum }

// TrophyTitle is one entry from GET /users/me/trophyTitles — a game the
// account has trophies for.
type TrophyTitle struct {
	// NpServiceName is "trophy" for PS3/PS4/Vita titles, "trophy2" for
	// PS5/PC. It must be sent back on every subsequent per-title call for a
	// "trophy" title, so it's captured here rather than re-derived.
	NpServiceName       string       `json:"npServiceName"`
	NpCommunicationID   string       `json:"npCommunicationId"`
	TrophyTitleName     string       `json:"trophyTitleName"`
	TrophyTitleIconURL  string       `json:"trophyTitleIconUrl"`
	TrophyTitlePlatform string       `json:"trophyTitlePlatform"`
	Progress            int          `json:"progress"`
	DefinedTrophies     TrophyCounts `json:"definedTrophies"`
	EarnedTrophies      TrophyCounts `json:"earnedTrophies"`
	LastUpdatedDateTime string       `json:"lastUpdatedDateTime"`
}

type trophyTitlesResponse struct {
	TrophyTitles   []TrophyTitle `json:"trophyTitles"`
	TotalItemCount int           `json:"totalItemCount"`
	NextOffset     int           `json:"nextOffset"`
}

// trophyTitlesPageSize is the documented maximum for this endpoint.
const trophyTitlesPageSize = 800

// TrophyTitles lists every title the account has trophies for, keyed by
// npCommunicationId. This is the discovery call: it's what resolves which
// npServiceName a title needs for every other endpoint, and it's also what
// backs `gamelog psn list`.
func (c *PSNClient) TrophyTitles(ctx context.Context) (map[string]TrophyTitle, error) {
	out := map[string]TrophyTitle{}
	offset := 0
	for {
		q := url.Values{}
		q.Set("limit", strconv.Itoa(trophyTitlesPageSize))
		q.Set("offset", strconv.Itoa(offset))

		body, err := c.get(ctx, TrophyBaseURL+"/users/me/trophyTitles?"+q.Encode())
		if err != nil {
			return nil, fmt.Errorf("psn: trophy titles: %w", err)
		}
		var page trophyTitlesResponse
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("psn: decoding trophy titles: %w", err)
		}
		for _, t := range page.TrophyTitles {
			out[t.NpCommunicationID] = t
		}
		if len(page.TrophyTitles) == 0 || len(out) >= page.TotalItemCount {
			return out, nil
		}
		offset = page.NextOffset
	}
}

// Trophy is one trophy's static definition — not user-specific.
type Trophy struct {
	TrophyID      int    `json:"trophyId"`
	TrophyHidden  bool   `json:"trophyHidden"`
	TrophyType    string `json:"trophyType"` // bronze, silver, gold, platinum
	TrophyName    string `json:"trophyName"`
	TrophyDetail  string `json:"trophyDetail"`
	TrophyIconURL string `json:"trophyIconUrl"`
	TrophyGroupID string `json:"trophyGroupId"`
}

type trophiesResponse struct {
	Trophies []Trophy `json:"trophies"`
}

// TrophyDefinitions fetches every trophy's static metadata for one title.
// npServiceName should be the value TrophyTitles reported for this title;
// leaving it empty only works for trophy2 (PS5/PC) titles. No limit is
// passed, so the API returns every trophy in the title in one response.
func (c *PSNClient) TrophyDefinitions(ctx context.Context, npCommunicationID, npServiceName string) ([]Trophy, error) {
	reqURL := fmt.Sprintf("%s/npCommunicationIds/%s/trophyGroups/all/trophies", TrophyBaseURL, npCommunicationID)
	if npServiceName != "" {
		reqURL += "?npServiceName=" + url.QueryEscape(npServiceName)
	}
	body, err := c.get(ctx, reqURL)
	if err != nil {
		return nil, fmt.Errorf("psn: trophy definitions for %s: %w", npCommunicationID, err)
	}
	var parsed trophiesResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("psn: decoding trophy definitions for %s: %w", npCommunicationID, err)
	}
	return parsed.Trophies, nil
}

// EarnedTrophy is one trophy's earned status for the account — no name or
// description, just whether (and when) it was unlocked.
type EarnedTrophy struct {
	TrophyID         int    `json:"trophyId"`
	TrophyHidden     bool   `json:"trophyHidden"`
	Earned           bool   `json:"earned"`
	EarnedDateTime   string `json:"earnedDateTime"`
	TrophyType       string `json:"trophyType"`
	TrophyRare       int    `json:"trophyRare"`
	TrophyEarnedRate string `json:"trophyEarnedRate"`
}

type earnedTrophiesResponse struct {
	Trophies            []EarnedTrophy `json:"trophies"`
	LastUpdatedDateTime string         `json:"lastUpdatedDateTime"`
}

// EarnedResult is the outcome of an EarnedTrophies call.
type EarnedResult struct {
	Trophies []EarnedTrophy
	// LastUpdatedDateTime is the title-level "most recent trophy earned"
	// date, used as the archive record's LastPlayed.
	LastUpdatedDateTime string
	// Raw is the verbatim response body, kept alongside TrophyDefinitions'
	// in the archived record so nothing normalised away is unrecoverable.
	Raw json.RawMessage
}

// EarnedTrophies fetches the account's earned status for one title.
// npServiceName follows the same rule as TrophyDefinitions.
func (c *PSNClient) EarnedTrophies(ctx context.Context, npCommunicationID, npServiceName string) (EarnedResult, error) {
	reqURL := fmt.Sprintf("%s/users/me/npCommunicationIds/%s/trophyGroups/all/trophies", TrophyBaseURL, npCommunicationID)
	if npServiceName != "" {
		reqURL += "?npServiceName=" + url.QueryEscape(npServiceName)
	}
	body, err := c.get(ctx, reqURL)
	if err != nil {
		return EarnedResult{}, fmt.Errorf("psn: earned trophies for %s: %w", npCommunicationID, err)
	}
	var parsed earnedTrophiesResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return EarnedResult{}, fmt.Errorf("psn: decoding earned trophies for %s: %w", npCommunicationID, err)
	}
	return EarnedResult{
		Trophies:            parsed.Trophies,
		LastUpdatedDateTime: parsed.LastUpdatedDateTime,
		Raw:                 json.RawMessage(body),
	}, nil
}

// today formats the current instant as a calendar date in the site's
// timezone, matching forms.Today's convention.
func today() string {
	return time.Now().In(model.SiteLocation).Format("2006-01-02")
}

// day formats an instant as the calendar date it fell on in the site's
// timezone.
func day(t time.Time) string {
	return t.In(model.SiteLocation).Format("2006-01-02")
}

// stamp renders an instant the way the archive stores dates.
func stamp(t time.Time) string {
	return t.In(model.SiteLocation).Format(time.RFC3339)
}

// combinedRaw is what's actually stored under ProviderRecord.Raw for a PSN
// record: unlike the other providers, no single Sony response carries both
// trophy metadata and earned status, so the two responses this fetch needed
// are kept together.
type combinedRaw struct {
	Definitions []Trophy       `json:"definitions"`
	Earned      []EarnedTrophy `json:"earned"`
}

// FetchRecord builds an archive record for one PSN title. Source is left ""
// (the zero value) since this is a first-party fetch — mergeProvider
// deliberately assigns rather than FirstNonEmpty's Source, so this correctly
// clears an earlier "exophase" marker on a game migrated off the mirror.
func FetchRecord(ctx context.Context, client *PSNClient, npCommunicationID string) (*model.ProviderRecord, error) {
	rec := &model.ProviderRecord{ID: npCommunicationID, LastAttempt: today()}

	titles, err := client.TrophyTitles(ctx)
	if err != nil {
		rec.LastError = err.Error()
		return rec, nil
	}
	title, ok := titles[npCommunicationID]
	if !ok {
		rec.LastError = fmt.Sprintf("no trophy title %s on this account", npCommunicationID)
		return rec, nil
	}
	rec.Platform = title.TrophyTitlePlatform
	rec.Icon = title.TrophyTitleIconURL
	rec.Completion = fmt.Sprintf("%d%%", title.Progress)

	defs, err := client.TrophyDefinitions(ctx, npCommunicationID, title.NpServiceName)
	if err != nil {
		rec.LastError = err.Error()
		return rec, nil
	}
	earned, err := client.EarnedTrophies(ctx, npCommunicationID, title.NpServiceName)
	if err != nil {
		rec.LastError = err.Error()
		return rec, nil
	}

	byID := make(map[int]EarnedTrophy, len(earned.Trophies))
	for _, e := range earned.Trophies {
		byID[e.TrophyID] = e
	}

	rec.Fetched = today()
	rec.Total = len(defs)
	if t, err := time.Parse(time.RFC3339, earned.LastUpdatedDateTime); err == nil {
		rec.LastPlayed = day(t)
	}

	var platinumDate string
	for _, d := range defs {
		e := byID[d.TrophyID]
		entry := model.ArchivedAchievement{
			Key:         strconv.Itoa(d.TrophyID),
			Name:        d.TrophyName,
			Description: d.TrophyDetail,
			Icon:        d.TrophyIconURL,
			Tier:        d.TrophyType,
			Hidden:      d.TrophyHidden,
		}
		if e.Earned {
			entry.Unlocked = true
			if t, err := time.Parse(time.RFC3339, e.EarnedDateTime); err == nil {
				entry.Date = stamp(t)
				if d.TrophyType == "platinum" {
					platinumDate = entry.Date
				}
			}
		}
		rec.Achievements = append(rec.Achievements, entry)
	}
	if platinumDate != "" {
		rec.AwardKind = "platinum"
		rec.AwardDate = platinumDate[:10]
	}

	if raw, err := json.Marshal(combinedRaw{Definitions: defs, Earned: earned.Trophies}); err == nil {
		rec.Raw = raw
	}

	rec.Summarize()
	return rec, nil
}
