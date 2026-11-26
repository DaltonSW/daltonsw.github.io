package steam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"sync"
	"time"

	"go.dalton.dog/gamelog/internal/model"
)

// Endpoint bases. Vars rather than consts so tests can point them at a local
// server; never reassigned in production code.
var (
	PlayerAchievementsURL = "https://api.steampowered.com/ISteamUserStats/GetPlayerAchievements/v0001/"
	OwnedGamesURL         = "https://api.steampowered.com/IPlayerService/GetOwnedGames/v0001/"
	ResolveVanityURL      = "https://api.steampowered.com/ISteamUser/ResolveVanityURL/v0001/"
	SchemaForGameURL      = "https://api.steampowered.com/ISteamUserStats/GetSchemaForGame/v2/"
	GlobalPercentagesURL  = "https://api.steampowered.com/ISteamUserStats/GetGlobalAchievementPercentagesForApp/v0002/"
	UserStatsURL          = "https://api.steampowered.com/ISteamUserStats/GetUserStatsForGame/v0002/"
)

// SteamClient talks to the Steam Web API.
type SteamClient struct {
	APIKey  string
	SteamID string // a SteamID64, or a vanity name resolved on first use
	HTTP    *http.Client

	mu         sync.Mutex // guards resolvedID on a shared client
	resolvedID string     // cache for resolveSteamID
}

var steamID64Pattern = regexp.MustCompile(`^\d{17}$`)

// resolveSteamID returns the caller's SteamID64. The Steam API only accepts
// the 17-digit form, but a profile URL shows a vanity name
// (steamcommunity.com/id/<name>), so accept either and resolve as needed.
func (c *SteamClient) resolveSteamID(ctx context.Context) (string, error) {
	if steamID64Pattern.MatchString(c.SteamID) {
		return c.SteamID, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.resolvedID != "" {
		return c.resolvedID, nil
	}

	q := url.Values{}
	q.Set("key", c.APIKey)
	q.Set("vanityurl", c.SteamID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ResolveVanityURL+"?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("steam: resolving vanity name %q: %w", c.SteamID, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("steam: reading vanity resolution for %q: %w", c.SteamID, err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("steam: resolving vanity name %q: unexpected status %s", c.SteamID, resp.Status)
	}

	var parsed struct {
		Response struct {
			SteamID string `json:"steamid"`
			Success int    `json:"success"`
		} `json:"response"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("steam: decoding vanity resolution for %q: %w", c.SteamID, err)
	}
	if parsed.Response.Success != 1 || parsed.Response.SteamID == "" {
		return "", fmt.Errorf("steam: no account found for SteamID64 or vanity name %q", c.SteamID)
	}

	c.resolvedID = parsed.Response.SteamID
	return c.resolvedID, nil
}

func (c *SteamClient) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// SteamAchievement is one entry in a GetPlayerAchievements response.
type SteamAchievement struct {
	APIName    string `json:"apiname"`
	Achieved   int    `json:"achieved"`
	UnlockTime int64  `json:"unlocktime"`
	// Name and Description are only populated when the request asks for a
	// language; without it Steam returns the internal apiname only.
	Name        string `json:"name"`
	Description string `json:"description"`
}

// DisplayName is the human-readable achievement name, falling back to the
// internal API name.
func (a SteamAchievement) DisplayName() string {
	if a.Name != "" {
		return a.Name
	}
	return a.APIName
}

type steamPlayerAchievementsResponse struct {
	PlayerStats struct {
		Success      bool               `json:"success"`
		Error        string             `json:"error"`
		Achievements []SteamAchievement `json:"achievements"`
	} `json:"playerstats"`
}

var errNoSchema = errors.New("steam: schema not requested")

// buildStats joins the player's readings to the game's stat definitions. The
// definitions come first, in the game's order, so a stat the game tracks but
// Steam reported nothing for is still on record with its display name; a
// reading with no definition (or no schema at all) is kept under its own key.
func buildStats(readings []SteamUserStat, defs []SteamSchemaStat) []model.ArchivedStat {
	values := make(map[string]float64, len(readings))
	for _, r := range readings {
		values[r.Name] = r.Value
	}
	var out []model.ArchivedStat
	seen := make(map[string]bool, len(defs)+len(readings))
	add := func(key, name string, def float64) {
		if seen[key] {
			return
		}
		seen[key] = true
		st := model.ArchivedStat{Key: key, Default: def}
		// Steam fills a blank display name with the key; storing it again
		// would only make a blank look like a name.
		if name != key {
			st.Name = name
		}
		if v, ok := values[key]; ok {
			st.Value = &v
		}
		out = append(out, st)
	}
	for _, d := range defs {
		add(d.Name, d.DisplayName, d.DefaultValue)
	}
	for _, r := range readings {
		add(r.Name, "", 0)
	}
	return out
}

// StatusError is a response whose status the caller has no contract for. It
// keeps the code so a caller can tell an outage from a refusal.
type StatusError struct {
	What   string // what was being fetched, e.g. "appid 219740"
	Code   int
	Status string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("steam: %s: unexpected status %s", e.What, e.Status)
}

// SteamAchievementsResult is the outcome of a GetPlayerAchievements call:
// either a list of achievements, or a reason none are available (private
// profile, or the game has no achievement schema at all).
// noStatsError is GetPlayerAchievements' answer (HTTP 400) for an app with no
// achievements defined.
const noStatsError = "Requested app has no stats"

type SteamAchievementsResult struct {
	Achievements []SteamAchievement
	Success      bool
	Error        string
	// Raw is the verbatim response body, kept so the archive can store what
	// the provider actually said rather than only our reading of it.
	Raw json.RawMessage
}

// GetPlayerAchievements fetches the caller's achievement unlock state for
// one app.
func (c *SteamClient) GetPlayerAchievements(ctx context.Context, appID string) (SteamAchievementsResult, error) {
	steamID, err := c.resolveSteamID(ctx)
	if err != nil {
		return SteamAchievementsResult{}, err
	}

	q := url.Values{}
	q.Set("appid", appID)
	q.Set("key", c.APIKey)
	q.Set("steamid", steamID)
	// Asking for a language is what makes Steam return display names and
	// descriptions instead of bare internal identifiers.
	q.Set("l", "english")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, PlayerAchievementsURL+"?"+q.Encode(), nil)
	if err != nil {
		return SteamAchievementsResult{}, err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return SteamAchievementsResult{}, fmt.Errorf("steam: request for appid %s: %w", appID, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return SteamAchievementsResult{}, fmt.Errorf("steam: reading response for appid %s: %w", appID, err)
	}
	// The two expected "no data" cases — a game with no achievement schema
	// (400) and a profile that isn't public (403) — are reported as HTTP
	// errors carrying a useful JSON explanation, so the body is worth more
	// than the status. Anything else is a genuine failure.
	statusErr := &StatusError{What: "appid " + appID, Code: resp.StatusCode, Status: resp.Status}
	if resp.StatusCode != http.StatusOK &&
		resp.StatusCode != http.StatusBadRequest &&
		resp.StatusCode != http.StatusForbidden {
		return SteamAchievementsResult{}, statusErr
	}

	var parsed steamPlayerAchievementsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return SteamAchievementsResult{}, statusErr
	}
	if resp.StatusCode != http.StatusOK && parsed.PlayerStats.Error == "" {
		return SteamAchievementsResult{}, statusErr
	}

	return SteamAchievementsResult{
		Achievements: parsed.PlayerStats.Achievements,
		Success:      parsed.PlayerStats.Success,
		Error:        parsed.PlayerStats.Error,
		Raw:          json.RawMessage(body),
	}, nil
}

// SteamSchemaAchievement is one achievement's static definition — not
// player-specific, so it carries the icon URLs GetPlayerAchievements doesn't:
// Icon is shown once unlocked, IconGray beforehand.
type SteamSchemaAchievement struct {
	APIName  string `json:"name"`
	Icon     string `json:"icon"`
	IconGray string `json:"icongray"`
	// Hidden is 1 when the developer marked this a secret achievement,
	// spoiled by name/description alone.
	Hidden int `json:"hidden"`
}

// SteamSchemaStat is one stat a game defines. DisplayName is frequently blank
// or just the key repeated; DefaultValue is what the player has until the game
// writes something else.
type SteamSchemaStat struct {
	Name         string  `json:"name"`
	DefaultValue float64 `json:"defaultvalue"`
	DisplayName  string  `json:"displayName"`
}

// SteamSchema is a game's static definitions.
type SteamSchema struct {
	Achievements []SteamSchemaAchievement
	Stats        []SteamSchemaStat
}

type steamSchemaResponse struct {
	Game struct {
		AvailableGameStats struct {
			Achievements []SteamSchemaAchievement `json:"achievements"`
			Stats        []SteamSchemaStat        `json:"stats"`
		} `json:"availableGameStats"`
	} `json:"game"`
}

// GetSchemaForGame fetches one app's static achievement definitions, keyed by
// apiname. Used only for icon URLs — names/descriptions already come from
// GetPlayerAchievements with "l=english" set. Not player-specific: no
// SteamID needed, and it works even against a private profile.
func (c *SteamClient) GetSchemaForGame(ctx context.Context, appID string) ([]SteamSchemaAchievement, error) {
	schema, err := c.GetGameSchema(ctx, appID)
	return schema.Achievements, err
}

// GetGameSchema is GetSchemaForGame with the stat definitions kept too.
func (c *SteamClient) GetGameSchema(ctx context.Context, appID string) (SteamSchema, error) {
	q := url.Values{}
	q.Set("key", c.APIKey)
	q.Set("appid", appID)
	q.Set("l", "english")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, SchemaForGameURL+"?"+q.Encode(), nil)
	if err != nil {
		return SteamSchema{}, err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return SteamSchema{}, fmt.Errorf("steam: schema request for appid %s: %w", appID, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return SteamSchema{}, fmt.Errorf("steam: reading schema response for appid %s: %w", appID, err)
	}
	if resp.StatusCode != http.StatusOK {
		return SteamSchema{}, fmt.Errorf("steam: schema for appid %s: unexpected status %s", appID, resp.Status)
	}

	var parsed steamSchemaResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return SteamSchema{}, fmt.Errorf("steam: decoding schema response for appid %s: %w", appID, err)
	}
	return SteamSchema{
		Achievements: parsed.Game.AvailableGameStats.Achievements,
		Stats:        parsed.Game.AvailableGameStats.Stats,
	}, nil
}

// SteamUserStat is one reading from GetUserStatsForGame.
type SteamUserStat struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

// SteamUserStatsResult is the player's stats for one app. Available is false
// when Steam has no stats for the app at all (a 400 with an empty body).
// HasAchievements reports whether the response lists any achievements, which is
// how an app with stats but none of them is told apart from one whose
// achievement fetch failed.
type SteamUserStatsResult struct {
	Available       bool
	HasAchievements bool
	Stats           []SteamUserStat
	Error           string
}

type steamUserStatsResponse struct {
	PlayerStats struct {
		Error        string            `json:"error"`
		Stats        []SteamUserStat   `json:"stats"`
		Achievements []json.RawMessage `json:"achievements"`
	} `json:"playerstats"`
}

// GetUserStats fetches the caller's per-game stats. Steam omits any stat still
// at its default, so an app can answer 200 with nothing in it. A private
// profile is a 403 carrying a reason, reported in Error like the achievement
// call does; a 400 means the app defines no stats.
func (c *SteamClient) GetUserStats(ctx context.Context, appID string) (SteamUserStatsResult, error) {
	steamID, err := c.resolveSteamID(ctx)
	if err != nil {
		return SteamUserStatsResult{}, err
	}

	q := url.Values{}
	q.Set("appid", appID)
	q.Set("key", c.APIKey)
	q.Set("steamid", steamID)
	q.Set("l", "english")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, UserStatsURL+"?"+q.Encode(), nil)
	if err != nil {
		return SteamUserStatsResult{}, err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return SteamUserStatsResult{}, fmt.Errorf("steam: stats request for appid %s: %w", appID, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return SteamUserStatsResult{}, fmt.Errorf("steam: reading stats response for appid %s: %w", appID, err)
	}
	statusErr := &StatusError{What: "stats for appid " + appID, Code: resp.StatusCode, Status: resp.Status}

	var parsed steamUserStatsResponse
	switch resp.StatusCode {
	case http.StatusOK:
		if err := json.Unmarshal(body, &parsed); err != nil {
			return SteamUserStatsResult{}, statusErr
		}
		return SteamUserStatsResult{
			Available:       true,
			HasAchievements: len(parsed.PlayerStats.Achievements) > 0,
			Stats:           parsed.PlayerStats.Stats,
		}, nil
	case http.StatusBadRequest:
		return SteamUserStatsResult{}, nil
	case http.StatusForbidden:
		if json.Unmarshal(body, &parsed) == nil && parsed.PlayerStats.Error != "" {
			return SteamUserStatsResult{Error: parsed.PlayerStats.Error}, nil
		}
	}
	return SteamUserStatsResult{}, statusErr
}

type steamGlobalPercentagesResponse struct {
	AchievementPercentages struct {
		Achievements []struct {
			Name string `json:"name"`
			// A string in v0002 ("34.5"); v0001 used a bare number.
			Percent json.Number `json:"percent"`
		} `json:"achievements"`
	} `json:"achievementpercentages"`
}

// GetGlobalPercentages fetches the share of all players (0-100) who earned
// each achievement, keyed by apiname. Public: no key or SteamID needed. An
// unknown or stats-less app answers 403 rather than an empty list.
func (c *SteamClient) GetGlobalPercentages(ctx context.Context, appID string) (map[string]float64, error) {
	q := url.Values{}
	q.Set("gameid", appID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, GlobalPercentagesURL+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("steam: global percentages request for appid %s: %w", appID, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("steam: reading global percentages for appid %s: %w", appID, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("steam: global percentages for appid %s: unexpected status %s", appID, resp.Status)
	}

	var parsed steamGlobalPercentagesResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("steam: decoding global percentages for appid %s: %w", appID, err)
	}
	out := make(map[string]float64, len(parsed.AchievementPercentages.Achievements))
	for _, a := range parsed.AchievementPercentages.Achievements {
		if f, err := a.Percent.Float64(); err == nil {
			out[a.Name] = f
		}
	}
	return out, nil
}

// SteamOwnedGame is one entry in the user's library. LastPlayed is a Unix
// timestamp, 0 if never played.
type SteamOwnedGame struct {
	AppID        int    `json:"appid"`
	Name         string `json:"name"`
	PlaytimeMins int    `json:"playtime_forever"`
	LastPlayed   int64  `json:"rtime_last_played"`
	IconURL      string `json:"img_icon_url"`

	// Per-device breakdowns. Steam reports these separately and they don't
	// necessarily sum to PlaytimeMins, so all are worth keeping.
	PlaytimeWindows      int `json:"playtime_windows_forever"`
	PlaytimeMac          int `json:"playtime_mac_forever"`
	PlaytimeLinux        int `json:"playtime_linux_forever"`
	PlaytimeDeck         int `json:"playtime_deck_forever"`
	PlaytimeDisconnected int `json:"playtime_disconnected"`
}

type steamOwnedGamesResponse struct {
	Response struct {
		Games []SteamOwnedGame `json:"games"`
	} `json:"response"`
}

// GetOwnedGames lists the user's library with names and playtimes. Steam
// returns the whole library in one response, so this doubles as the discovery
// call and as the backing data for GetOwnedGamePlaytime.
func (c *SteamClient) GetOwnedGames(ctx context.Context) ([]SteamOwnedGame, error) {
	steamID, err := c.resolveSteamID(ctx)
	if err != nil {
		return nil, err
	}

	q := url.Values{}
	q.Set("key", c.APIKey)
	q.Set("steamid", steamID)
	q.Set("include_appinfo", "1")
	// Free-to-play titles are excluded from the owned-games list unless
	// asked for, which would silently drop playtime for anything F2P.
	q.Set("include_played_free_games", "1")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, OwnedGamesURL+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("steam: owned games request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("steam: reading owned games response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("steam: owned games: unexpected status %s", resp.Status)
	}

	var parsed steamOwnedGamesResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("steam: decoding owned games response: %w", err)
	}
	return parsed.Response.Games, nil
}

// GetOwnedGamePlaytime returns the caller's total all-time playtime (in
// minutes) for one app, for context alongside the achievement-based date
// guess. Steam has no playtime-over-time API, so this is never used for
// date suggestions themselves. found is false if the app isn't in the
// owned-games list (e.g. private profile).
func (c *SteamClient) GetOwnedGamePlaytime(ctx context.Context, appID string) (minutes int, found bool, err error) {
	games, err := c.GetOwnedGames(ctx)
	if err != nil {
		return 0, false, err
	}
	for _, g := range games {
		if strconv.Itoa(g.AppID) == appID {
			return g.PlaytimeMins, true, nil
		}
	}
	return 0, false, nil
}

// SteamSuggestion is the computed start/finish guess for one game, derived
// purely from achievement unlock timestamps. Always lower-confidence than
// an equivalent RetroAchievements suggestion — Steam has no separate
// "completion" signal, so this is a guess even when every achievement is
// unlocked.
type SteamSuggestion struct {
	Started  time.Time
	Finished time.Time
	// Unlocks is every unlock instant, so callers can tell which of them a
	// logged playthrough already covers (commands.WithCoverage).
	Unlocks       []time.Time
	UnlockedCount int
	TotalCount    int
	OK            bool
}

func SteamSuggestRange(achievements []SteamAchievement) SteamSuggestion {
	s := SteamSuggestion{TotalCount: len(achievements)}
	var earliest, latest time.Time
	found := false
	for _, a := range achievements {
		if a.Achieved != 1 || a.UnlockTime <= 0 {
			continue
		}
		s.UnlockedCount++
		t := time.Unix(a.UnlockTime, 0)
		s.Unlocks = append(s.Unlocks, t)
		if !found || t.Before(earliest) {
			earliest = t
		}
		if !found || t.After(latest) {
			latest = t
		}
		found = true
	}
	if !found {
		return s
	}
	s.Started = earliest
	s.Finished = latest
	s.OK = true
	return s
}

// today formats the current instant as a calendar date in the site's
// timezone, matching model.Today's convention.
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

// FetchRecord builds a provider record from Steam. owned may be nil; it
// supplies playtime, which the achievements endpoint doesn't carry.
func FetchRecord(ctx context.Context, client *SteamClient, appID string, owned *SteamOwnedGame) (*model.ProviderRecord, error) {
	rec := &model.ProviderRecord{ID: appID, LastAttempt: today(), Platform: "PC"}

	if owned != nil {
		rec.PlaytimeMins = owned.PlaytimeMins
		rec.Icon = owned.IconURL
		rec.Playtime = map[string]int{}
		for name, mins := range map[string]int{
			"windows":      owned.PlaytimeWindows,
			"mac":          owned.PlaytimeMac,
			"linux":        owned.PlaytimeLinux,
			"deck":         owned.PlaytimeDeck,
			"disconnected": owned.PlaytimeDisconnected,
		} {
			if mins > 0 {
				rec.Playtime[name] = mins
			}
		}
		if owned.LastPlayed > 0 {
			rec.LastPlayed = day(time.Unix(owned.LastPlayed, 0))
		}
	}

	res, err := client.GetPlayerAchievements(ctx, appID)

	// Per-game stats are read for every app, whatever the achievement fetch did:
	// a game can have stats and no achievements, and a failed achievement call
	// says nothing about whether the stats call works. A failure here is
	// best-effort and never the record's error, since it is the second half of
	// what the record carries.
	us, usErr := client.GetUserStats(ctx, appID)
	var schema SteamSchema
	var schemaErr error = errNoSchema
	if (usErr == nil && us.Available) || (err == nil && len(res.Achievements) > 0) {
		schema, schemaErr = client.GetGameSchema(ctx, appID)
	}
	if usErr == nil && us.Available {
		rec.Stats = buildStats(us.Stats, schema.Stats)
	}

	if err != nil {
		// An app that defines stats but no achievements answers the achievement
		// call with a bare 500 rather than the 400 "no stats" an app with
		// neither gets. The stats call settles it: if that worked and lists
		// no achievements, there are none to fetch. Without that evidence a
		// 500 is a real outage and stays an error.
		var se *StatusError
		if errors.As(err, &se) && se.Code == http.StatusInternalServerError &&
			usErr == nil && us.Available && !us.HasAchievements {
			rec.Fetched = today()
			return rec, nil
		}
		rec.LastError = err.Error()
		return rec, nil
	}
	rec.Raw = res.Raw
	// "No stats" means the app has no achievements to fetch — an answer, not a
	// failure, so it's dated like any successful fetch (steamUnchanged needs
	// that to skip it). The empty achievement list still keeps a merge from
	// touching anything already archived.
	if !res.Success && res.Error == noStatsError {
		rec.Fetched = today()
		return rec, nil
	}
	if !res.Success {
		rec.LastError = model.FirstNonEmpty(res.Error, "achievement data unavailable")
		return rec, nil
	}

	rec.Fetched = today()
	rec.Total = len(res.Achievements)
	for _, a := range res.Achievements {
		entry := model.ArchivedAchievement{
			Key:         a.APIName,
			Name:        a.DisplayName(),
			Description: a.Description,
		}
		if a.Achieved == 1 && a.UnlockTime > 0 {
			entry.Unlocked = true
			entry.Date = stamp(time.Unix(a.UnlockTime, 0))
		}
		rec.Achievements = append(rec.Achievements, entry)
	}

	// Icons and hidden-status aren't in GetPlayerAchievements at all — only
	// the static schema carries them. Best-effort: a game whose schema call
	// fails (or a very old/delisted title with no schema) still keeps its
	// unlock data, just without icons or hidden-status.
	if schemaErr == nil {
		byName := make(map[string]SteamSchemaAchievement, len(schema.Achievements))
		for _, a := range schema.Achievements {
			byName[a.APIName] = a
		}
		for i := range rec.Achievements {
			if a, ok := byName[rec.Achievements[i].Key]; ok {
				if a.Icon != "" {
					rec.Achievements[i].Icon = a.Icon
				}
				rec.Achievements[i].Hidden = a.Hidden == 1
			}
		}
	}

	// Global earn rates: best-effort like the schema. A failure leaves Rarity
	// zero, which the merge reads as "not reported" and keeps any earlier value.
	if pct, err := client.GetGlobalPercentages(ctx, appID); err == nil {
		for i := range rec.Achievements {
			rec.Achievements[i].Rarity = pct[rec.Achievements[i].Key]
		}
	}

	// The API withholds hidden achievements' text; the Steam client's local
	// cache has it. Only blanks are filled, and a missing cache changes nothing.
	var local map[string]LocalAchievement
	for i := range rec.Achievements {
		a := &rec.Achievements[i]
		if !a.Hidden || a.Description != "" {
			continue
		}
		if local == nil {
			if local = LoadLocalSchema(appID); local == nil {
				break
			}
		}
		if l, ok := local[a.Key]; ok {
			a.Description = l.Description
			if a.Name == a.Key && l.Name != "" {
				a.Name = l.Name // DisplayName fell back to the API key
			}
		}
	}

	rec.Summarize()
	if rec.Total > 0 && rec.Unlocked == rec.Total {
		rec.AwardKind = "all achievements"
	}
	return rec, nil
}
