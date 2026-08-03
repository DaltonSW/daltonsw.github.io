package xbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.dalton.dog/gamelog/internal/model"
)

// XBLClient talks to OpenXBL (https://xbl.io), an unofficial Xbox Live API.
// Xbox has no public first-party API, so this is the same kind of mirror
// Exophase is for PlayStation — except OpenXBL requires a personal API key
// (from signing into xbl.io with the same Microsoft account), not just a
// public profile name.
//
// Two sharp edges, both found against live responses rather than docs:
//
//   - Every response body is wrapped in {"content": ..., "code": 200} —
//     the caller unwraps content, not the top level.
//   - Modern titles' achievement endpoint
//     (/achievements/player/{xuid}/title/{titleId}) returns an empty list
//     for legacy Xbox 360 titles. The x360-specific endpoint used here
//     (/achievements/x360/{xuid}/title/{titleId}) is required for those, and
//     it also only reports *unlocked* achievements — like Exophase's earned
//     list, the total-possible count comes from titleHistory instead.
//   - Some of the oldest unlocks on an account report timeUnlocked as
//     1753-01-01T00:00:00Z — .NET's SqlDateTime.MinValue, not a real date.
//     See xboxUnsetTime.
var (
	AccountURL       = "https://xbl.io/api/v2/account"
	TitleHistoryURL  = "https://xbl.io/api/v2/player/titleHistory"
	X360Achievements = "https://xbl.io/api/v2/achievements/x360/%s/title/%s"
)

// XBLClient talks to OpenXBL.
type XBLClient struct {
	APIKey string
	HTTP   *http.Client
}

func (c *XBLClient) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c *XBLClient) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Authorization", c.APIKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("xbox: %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("xbox: reading %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("xbox: %s: unexpected status %s", url, resp.Status)
	}
	return body, nil
}

// XUID resolves the API key owner's Xbox User ID.
func (c *XBLClient) XUID(ctx context.Context) (string, error) {
	body, err := c.get(ctx, AccountURL)
	if err != nil {
		return "", err
	}
	var parsed struct {
		Content struct {
			ProfileUsers []struct {
				ID string `json:"id"`
			} `json:"profileUsers"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("xbox: decoding account response: %w", err)
	}
	if len(parsed.Content.ProfileUsers) == 0 || parsed.Content.ProfileUsers[0].ID == "" {
		return "", fmt.Errorf("xbox: account response has no profile")
	}
	return parsed.Content.ProfileUsers[0].ID, nil
}

// Title is one game in the account's title history — the source of the
// achievement denominator and last-played date, since the x360 achievements
// endpoint only reports what's unlocked (see the package doc comment).
type Title struct {
	TitleID     string   `json:"titleId"`
	Name        string   `json:"name"`
	Devices     []string `json:"devices"`
	Achievement struct {
		Current int `json:"currentAchievements"`
		Total   int `json:"totalAchievements"`
	} `json:"achievement"`
	TitleHistory struct {
		LastTimePlayed string `json:"lastTimePlayed"`
	} `json:"titleHistory"`
}

// TitleHistory lists every title on the account, keyed by titleId.
func (c *XBLClient) TitleHistory(ctx context.Context) (map[string]Title, error) {
	body, err := c.get(ctx, TitleHistoryURL)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Content struct {
			Titles []Title `json:"titles"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("xbox: decoding title history: %w", err)
	}
	out := make(map[string]Title, len(parsed.Content.Titles))
	for _, t := range parsed.Content.Titles {
		out[t.TitleID] = t
	}
	return out, nil
}

// X360Achievement is one achievement the x360 endpoint reported — always
// unlocked; the endpoint doesn't list locked ones at all.
type X360Achievement struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Gamerscore   int    `json:"gamerscore"`
	Unlocked     bool   `json:"unlocked"`
	TimeUnlocked string `json:"timeUnlocked"`
}

// X360Achievements fetches the unlocked-achievement list for one legacy
// Xbox 360 title. Locked achievements aren't in the response at all — the
// denominator comes from TitleHistory.
func (c *XBLClient) X360Achievements(ctx context.Context, xuid, titleID string) ([]X360Achievement, json.RawMessage, error) {
	body, err := c.get(ctx, fmt.Sprintf(X360Achievements, xuid, titleID))
	if err != nil {
		return nil, nil, err
	}
	var parsed struct {
		Content struct {
			Achievements []X360Achievement `json:"achievements"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, nil, fmt.Errorf("xbox: decoding achievements for title %s: %w", titleID, err)
	}
	return parsed.Content.Achievements, body, nil
}

// today formats the current instant as a calendar date in the site's
// timezone, matching forms.Today's convention.
func today() string {
	return time.Now().In(model.SiteLocation).Format("2006-01-02")
}

// xboxUnsetTime is what a genuinely unlocked achievement reports when the
// legacy Xbox 360 achievement service never recorded a real unlock time —
// common for the earliest achievements on an account. It's .NET's
// SqlDateTime.MinValue (1753-01-01 UTC), not a real date; found against live
// responses, where it was silently corrupting First/Last to "1752-12-31" in
// the site's timezone.
const xboxUnsetTime = "1753-01-01T00:00:00.0000000Z"

// parseXboxTime parses the fractional-second UTC timestamps OpenXBL reports
// (e.g. "2011-12-22T20:09:54.0270000Z") and renders them the way the archive
// stores dates. Reports false for xboxUnsetTime as well as unparseable input.
func parseXboxTime(s string) (string, bool) {
	if s == xboxUnsetTime {
		return "", false
	}
	t, err := time.Parse("2006-01-02T15:04:05.9999999Z", s)
	if err != nil {
		return "", false
	}
	return t.In(model.SiteLocation).Format(time.RFC3339), true
}

// FetchRecord builds an archive record for one Xbox 360 title. Both API
// calls are scoped to the caller's own account — there's no equivalent of
// Exophase's "any public profile" for Xbox.
func FetchRecord(ctx context.Context, client *XBLClient, titleID string) (*model.ProviderRecord, error) {
	rec := &model.ProviderRecord{ID: titleID, LastAttempt: today(), Source: "openxbl"}

	xuid, err := client.XUID(ctx)
	if err != nil {
		rec.LastError = err.Error()
		return rec, nil
	}
	titles, err := client.TitleHistory(ctx)
	if err != nil {
		rec.LastError = err.Error()
		return rec, nil
	}
	title, ok := titles[titleID]
	if !ok {
		rec.LastError = fmt.Sprintf("no title %s in this account's history", titleID)
		return rec, nil
	}

	rec.Total = title.Achievement.Total
	rec.Platform = "Xbox 360"
	if len(title.TitleHistory.LastTimePlayed) >= 10 {
		rec.LastPlayed = title.TitleHistory.LastTimePlayed[:10]
	}

	achievements, raw, err := client.X360Achievements(ctx, xuid, titleID)
	if err != nil {
		rec.LastError = err.Error()
		return rec, nil
	}
	for _, a := range achievements {
		entry := model.ArchivedAchievement{
			Key:         fmt.Sprint(a.ID),
			Name:        a.Name,
			Description: a.Description,
			Points:      a.Gamerscore,
		}
		if a.Unlocked {
			entry.Unlocked = true
			if date, ok := parseXboxTime(a.TimeUnlocked); ok {
				entry.Date = date
			}
		}
		rec.Achievements = append(rec.Achievements, entry)
	}
	rec.Raw = raw
	rec.Fetched = today()
	rec.Summarize()
	// Summarize (model/archive.go) only counts an unlock toward rec.Unlocked
	// when it carries a real date, so an xboxUnsetTime achievement —
	// genuinely unlocked, just dateless — would otherwise undercount.
	// titleHistory's own count is the true figure; never let it shrink what
	// Summarize already found.
	if title.Achievement.Current > rec.Unlocked {
		rec.Unlocked = title.Achievement.Current
	}
	return rec, nil
}
