// Package ninjakiwi talks to Ninja Kiwi's public data API
// (https://data.ninjakiwi.com), which serves a player's full save state for
// an "OAK" token — a save-export credential, not an account login. There is
// no first-party documentation beyond the endpoint list Ninja Kiwi publishes
// at https://data.ninjakiwi.com.
//
// One request is a full snapshot of the player's current save, not an event
// log — which is why the merge logic in internal/model/ninjakiwi.go is keyed
// per field rather than a union of new entries the way achievements are.
package ninjakiwi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// BaseURL is the API base. A var rather than a const so tests can point it at
// a local server; never reassigned in production code.
var BaseURL = "https://data.ninjakiwi.com"

// minInterval paces requests. There's no published rate limit, but a fetch is
// a single request per run, so this only matters if something starts calling
// the client in a loop.
const minInterval = 250 * time.Millisecond

// userAgent identifies this project and a contact address, the same courtesy
// extended to Nadeo's services.
const userAgent = "gamelog (https://dalton.dog) / Dalton Williams / email@dalton.dog"

// Client talks to Ninja Kiwi's data API using one OAK token, bound to one
// player's save.
type Client struct {
	OAK  string
	HTTP *http.Client

	// Throttle is the minimum gap between requests. Zero means minInterval;
	// set explicitly in tests to avoid sleeping.
	Throttle time.Duration

	mu      sync.Mutex
	lastReq time.Time
}

// Configured reports whether an OAK token is set.
func (c *Client) Configured() bool { return c.OAK != "" }

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) throttle() time.Duration {
	if c.Throttle > 0 {
		return c.Throttle
	}
	return minInterval
}

func (c *Client) wait() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if gap := c.throttle() - time.Since(c.lastReq); gap > 0 {
		time.Sleep(gap)
	}
	c.lastReq = time.Now()
}

// envelope is the shape of every response: a save endpoint's payload lands in
// Body, an error message (when Success is false) in Error.
type envelope struct {
	Success bool            `json:"success"`
	Error   string          `json:"error,omitempty"`
	Body    json.RawMessage `json:"body,omitempty"`
}

// SaveBody fetches one player's raw BTD6 save body for the given OAK token,
// returning the decoded envelope's body verbatim (undecoded further — that's
// the caller's job) on success.
//
// A non-2xx status, a transport error, or `success: false` in the envelope
// are all reported as plain errors here; converting those into the
// fail-soft LastError contract other providers use happens one layer up, in
// fetch.go — this function just talks to the wire.
func (c *Client) SaveBody(ctx context.Context) (json.RawMessage, error) {
	if !c.Configured() {
		return nil, fmt.Errorf("ninjakiwi: no OAK token configured")
	}
	c.wait()

	url := fmt.Sprintf("%s/btd6/save/%s", strings.TrimRight(BaseURL, "/"), c.OAK)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ninjakiwi: %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}

	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("ninjakiwi: decoding response: %w", err)
	}
	if !env.Success {
		return nil, fmt.Errorf("ninjakiwi: %s", firstNonEmpty(env.Error, "request was not successful"))
	}
	if len(env.Body) == 0 {
		return nil, fmt.Errorf("ninjakiwi: response had no body — token may be expired or invalid")
	}
	return env.Body, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
