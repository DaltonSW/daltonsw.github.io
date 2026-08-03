package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"
)

// Endpoint bases. Vars rather than consts so tests can point them at a local
// server; never reassigned in production code.
var (
	steamPlayerAchievementsURL = "https://api.steampowered.com/ISteamUserStats/GetPlayerAchievements/v0001/"
	steamOwnedGamesURL         = "https://api.steampowered.com/IPlayerService/GetOwnedGames/v0001/"
	steamResolveVanityURL      = "https://api.steampowered.com/ISteamUser/ResolveVanityURL/v0001/"
	steamSchemaForGameURL      = "https://api.steampowered.com/ISteamUserStats/GetSchemaForGame/v2/"
)

// SteamClient talks to the Steam Web API.
type SteamClient struct {
	APIKey  string
	SteamID string // a SteamID64, or a vanity name resolved on first use
	HTTP    *http.Client

	resolvedID string // cache for resolveSteamID
}

var steamID64Pattern = regexp.MustCompile(`^\d{17}$`)

// resolveSteamID returns the caller's SteamID64. The Steam API only accepts
// the 17-digit form, but a profile URL shows a vanity name
// (steamcommunity.com/id/<name>), so accept either and resolve as needed.
func (c *SteamClient) resolveSteamID(ctx context.Context) (string, error) {
	if steamID64Pattern.MatchString(c.SteamID) {
		return c.SteamID, nil
	}
	if c.resolvedID != "" {
		return c.resolvedID, nil
	}

	q := url.Values{}
	q.Set("key", c.APIKey)
	q.Set("vanityurl", c.SteamID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, steamResolveVanityURL+"?"+q.Encode(), nil)
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

// SteamAchievementsResult is the outcome of a GetPlayerAchievements call:
// either a list of achievements, or a reason none are available (private
// profile, or the game has no achievement schema at all).
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, steamPlayerAchievementsURL+"?"+q.Encode(), nil)
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
	if resp.StatusCode != http.StatusOK &&
		resp.StatusCode != http.StatusBadRequest &&
		resp.StatusCode != http.StatusForbidden {
		return SteamAchievementsResult{}, fmt.Errorf("steam: appid %s: unexpected status %s", appID, resp.Status)
	}

	var parsed steamPlayerAchievementsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return SteamAchievementsResult{}, fmt.Errorf("steam: appid %s: unexpected status %s", appID, resp.Status)
	}
	if resp.StatusCode != http.StatusOK && parsed.PlayerStats.Error == "" {
		return SteamAchievementsResult{}, fmt.Errorf("steam: appid %s: unexpected status %s", appID, resp.Status)
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
}

type steamSchemaResponse struct {
	Game struct {
		AvailableGameStats struct {
			Achievements []SteamSchemaAchievement `json:"achievements"`
		} `json:"availableGameStats"`
	} `json:"game"`
}

// GetSchemaForGame fetches one app's static achievement definitions, keyed by
// apiname. Used only for icon URLs — names/descriptions already come from
// GetPlayerAchievements with "l=english" set. Not player-specific: no
// SteamID needed, and it works even against a private profile.
func (c *SteamClient) GetSchemaForGame(ctx context.Context, appID string) ([]SteamSchemaAchievement, error) {
	q := url.Values{}
	q.Set("key", c.APIKey)
	q.Set("appid", appID)
	q.Set("l", "english")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, steamSchemaForGameURL+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("steam: schema request for appid %s: %w", appID, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("steam: reading schema response for appid %s: %w", appID, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("steam: schema for appid %s: unexpected status %s", appID, resp.Status)
	}

	var parsed steamSchemaResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("steam: decoding schema response for appid %s: %w", appID, err)
	}
	return parsed.Game.AvailableGameStats.Achievements, nil
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, steamOwnedGamesURL+"?"+q.Encode(), nil)
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
	Started       time.Time
	Finished      time.Time
	UnlockedCount int
	TotalCount    int
	OK            bool
}

func steamSuggestRange(achievements []SteamAchievement) SteamSuggestion {
	s := SteamSuggestion{TotalCount: len(achievements)}
	var earliest, latest time.Time
	found := false
	for _, a := range achievements {
		if a.Achieved != 1 || a.UnlockTime <= 0 {
			continue
		}
		s.UnlockedCount++
		t := time.Unix(a.UnlockTime, 0)
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
