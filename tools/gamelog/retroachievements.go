package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Endpoint base. A var rather than a const so tests can point it at a local
// server; never reassigned in production code.
var (
	raGameProgressURL = "https://retroachievements.org/API/API_GetGameInfoAndUserProgress.php"
	raCompletionURL   = "https://retroachievements.org/API/API_GetUserCompletionProgress.php"
)

// RetroAchievements rejects Go's default "Go-http-client/1.1" User-Agent with
// a 403; their API policy asks for a descriptive one identifying the client.
const raUserAgent = "go.dalton.dog"

// raMinInterval paces requests. RA answers sustained bursts with 429, which
// matters now that a single command can walk dozens of games; one request per
// ~1.2s stays comfortably inside the limit.
const raMinInterval = 1200 * time.Millisecond

// RAClient talks to the RetroAchievements web API.
type RAClient struct {
	Username string
	APIKey   string
	HTTP     *http.Client

	// Throttle is the minimum gap between requests. Zero means
	// raMinInterval; set it explicitly in tests to avoid sleeping.
	Throttle time.Duration
	lastReq  time.Time
}

func (c *RAClient) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func (c *RAClient) interval() time.Duration {
	if c.Throttle != 0 {
		return c.Throttle
	}
	return raMinInterval
}

// do issues a throttled GET, retrying on 429 with backoff. It returns the
// body and status so callers can interpret RA's several success-shaped
// failure modes themselves.
func (c *RAClient) do(ctx context.Context, url string) (body []byte, status int, err error) {
	backoff := 2 * time.Second
	for attempt := 0; ; attempt++ {
		if wait := c.interval() - time.Since(c.lastReq); wait > 0 && !c.lastReq.IsZero() {
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("User-Agent", raUserAgent)

		resp, err := c.httpClient().Do(req)
		c.lastReq = time.Now()
		if err != nil {
			return nil, 0, err
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, resp.StatusCode, readErr
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
		return body, resp.StatusCode, nil
	}
}

// RAAchievement is one entry in a GetGameInfoAndUserProgress response's
// Achievements map. Dates are SQL-datetime strings ("2023-04-05 14:32:10"),
// empty when not yet earned.
type RAAchievement struct {
	ID                 int    `json:"ID"`
	Title              string `json:"Title"`
	Description        string `json:"Description"`
	Points             int    `json:"Points"`
	TrueRatio          int    `json:"TrueRatio"`
	BadgeName          string `json:"BadgeName"`
	DisplayOrder       int    `json:"DisplayOrder"`
	Type               string `json:"Type"`
	DateEarned         string `json:"DateEarned"`
	DateEarnedHardcore string `json:"DateEarnedHardcore"`
}

// Earned returns the achievement's unlock timestamp, preferring the hardcore
// date, and whether it was earned at all.
func (a RAAchievement) Earned() (time.Time, bool, bool) {
	for _, raw := range []string{a.DateEarnedHardcore, a.DateEarned} {
		if t, err := parseRADate(raw); err == nil {
			return t, raw == a.DateEarnedHardcore, true
		}
	}
	return time.Time{}, false, false
}

// RAProgress is the subset of GetGameInfoAndUserProgress's response used
// for date suggestions.
type RAProgress struct {
	Title                  string                   `json:"Title"`
	ConsoleName            string                   `json:"ConsoleName"`
	ImageIcon              string                   `json:"ImageIcon"`
	NumAchievements        int                      `json:"NumAchievements"`
	NumAwardedToUser       int                      `json:"NumAwardedToUser"`
	NumAwardedHardcore     int                      `json:"NumAwardedToUserHardcore"`
	UserCompletion         string                   `json:"UserCompletion"`
	UserCompletionHardcore string                   `json:"UserCompletionHardcore"`
	Achievements           map[string]RAAchievement `json:"Achievements"`
	HighestAwardKind       string                   `json:"HighestAwardKind"`
	HighestAwardDate       string                   `json:"HighestAwardDate"`

	// Raw is the verbatim response body; see SteamAchievementsResult.Raw.
	Raw json.RawMessage `json:"-"`
}

// GetGameProgress fetches a user's achievement progress for one game.
func (c *RAClient) GetGameProgress(ctx context.Context, gameID string) (RAProgress, error) {
	q := url.Values{}
	q.Set("z", c.Username)
	q.Set("y", c.APIKey)
	q.Set("g", gameID)
	q.Set("u", c.Username)
	// Without a=1 the response omits HighestAwardKind/HighestAwardDate
	// entirely (they come back null), which is the only signal that
	// distinguishes a finished game from one still in progress.
	q.Set("a", "1")

	body, status, err := c.do(ctx, raGameProgressURL+"?"+q.Encode())
	if err != nil {
		return RAProgress{}, fmt.Errorf("retroachievements: request for game %s: %w", gameID, err)
	}
	if status != http.StatusOK {
		if msg := raErrorMessage(body); msg != "" {
			return RAProgress{}, fmt.Errorf("retroachievements: game %s: %s (status %d)", gameID, msg, status)
		}
		return RAProgress{}, fmt.Errorf("retroachievements: game %s: unexpected status %d", gameID, status)
	}

	// An unknown game ID is answered with HTTP 200 and a bare `[]`, which
	// would otherwise surface as an unreadable json type error.
	if bytes.HasPrefix(bytes.TrimSpace(body), []byte("[")) {
		return RAProgress{}, fmt.Errorf("retroachievements: no game with ID %s", gameID)
	}

	var progress RAProgress
	if err := json.Unmarshal(body, &progress); err != nil {
		return RAProgress{}, fmt.Errorf("retroachievements: decoding response for game %s: %w", gameID, err)
	}
	progress.Raw = json.RawMessage(body)
	return progress, nil
}

// raErrorMessage pulls the human-readable message out of an RA error body
// (e.g. {"message":"Unauthenticated.", ...}), or "" if there isn't one.
func raErrorMessage(body []byte) string {
	var e struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &e); err != nil {
		return ""
	}
	return e.Message
}

// RAPlayedGame is one entry in the user's completion-progress list: every
// game they've earned at least one achievement in.
type RAPlayedGame struct {
	GameID           int    `json:"GameID"`
	Title            string `json:"Title"`
	ConsoleName      string `json:"ConsoleName"`
	NumAwarded       int    `json:"NumAwarded"`
	MaxPossible      int    `json:"MaxPossible"`
	HighestAwardKind string `json:"HighestAwardKind"`
	HighestAwardDate string `json:"HighestAwardDate"`
	MostRecentDate   string `json:"MostRecentAwardedDate"`
}

// Finished reports whether this game carries a finishing award.
func (g RAPlayedGame) Finished() bool {
	return raAwardKinds[strings.ToLower(g.HighestAwardKind)]
}

type raCompletionResponse struct {
	Count   int            `json:"Count"`
	Total   int            `json:"Total"`
	Results []RAPlayedGame `json:"Results"`
}

// GetUserCompletionProgress lists every game the user has played, following
// pagination to the end. This is the discovery counterpart to
// GetGameProgress: one cheap call per 500 games rather than one per game.
func (c *RAClient) GetUserCompletionProgress(ctx context.Context) ([]RAPlayedGame, error) {
	const pageSize = 500
	var out []RAPlayedGame

	for offset := 0; ; offset += pageSize {
		q := url.Values{}
		q.Set("z", c.Username)
		q.Set("y", c.APIKey)
		q.Set("u", c.Username)
		q.Set("c", fmt.Sprint(pageSize))
		q.Set("o", fmt.Sprint(offset))

		body, status, err := c.do(ctx, raCompletionURL+"?"+q.Encode())
		if err != nil {
			return nil, fmt.Errorf("retroachievements: completion progress: %w", err)
		}
		if status != http.StatusOK {
			if msg := raErrorMessage(body); msg != "" {
				return nil, fmt.Errorf("retroachievements: completion progress: %s (status %d)", msg, status)
			}
			return nil, fmt.Errorf("retroachievements: completion progress: unexpected status %d", status)
		}

		var page raCompletionResponse
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("retroachievements: decoding completion progress: %w", err)
		}
		out = append(out, page.Results...)
		if len(page.Results) == 0 || len(out) >= page.Total {
			return out, nil
		}
	}
}

// raAwardKinds are the HighestAwardKind values that represent a genuine
// finish, as opposed to partial progress with no award. "beaten-*" means the
// game was completed normally, which for a play log is a better `finished`
// signal than mastery (every achievement) — both count.
var raAwardKinds = map[string]bool{
	"mastered":        true,
	"completed":       true,
	"beaten-hardcore": true,
	"beaten-softcore": true,
}

// RASuggestion is the computed start/finish guess for one game.
type RASuggestion struct {
	Started    time.Time
	Finished   time.Time
	Confidence string // "high" (a finishing award was earned) or "medium" (partial progress, no award)
	AwardKind  string // the raw HighestAwardKind behind a "high" confidence, e.g. "mastered"
	OK         bool
}

// raSuggestRange derives a suggested playthrough date range from a user's
// achievement progress: earliest unlock is the suggested start; the mastery
// date (when present) is the suggested finish at high confidence, otherwise
// the latest unlock stands in at medium confidence.
func raSuggestRange(p RAProgress) RASuggestion {
	var earliest, latest time.Time
	found := false
	for _, a := range p.Achievements {
		for _, raw := range []string{a.DateEarned, a.DateEarnedHardcore} {
			t, err := parseRADate(raw)
			if err != nil {
				continue
			}
			if !found || t.Before(earliest) {
				earliest = t
			}
			if !found || t.After(latest) {
				latest = t
			}
			found = true
		}
	}
	if !found {
		return RASuggestion{}
	}

	s := RASuggestion{Started: earliest, OK: true}
	kind := strings.ToLower(p.HighestAwardKind)
	if raAwardKinds[kind] {
		if t, err := parseRAAwardDate(p.HighestAwardDate); err == nil {
			s.Finished = t
			s.Confidence = "high"
			s.AwardKind = kind
			return s
		}
	}
	s.Finished = latest
	s.Confidence = "medium"
	return s
}

// parseRADate parses a per-achievement DateEarned/DateEarnedHardcore value,
// which RA returns as a zoneless SQL datetime in UTC.
func parseRADate(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("empty date")
	}
	return time.Parse("2006-01-02 15:04:05", s)
}

// parseRAAwardDate parses HighestAwardDate, which — unlike the per-achievement
// dates — comes back as RFC3339 with an explicit offset.
func parseRAAwardDate(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("empty date")
	}
	return time.Parse(time.RFC3339, s)
}
