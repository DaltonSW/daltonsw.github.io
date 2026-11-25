package commands

import (
	"sync"

	"go.dalton.dog/gamelog/internal/providers/psn"
	"go.dalton.dog/gamelog/internal/providers/retroachievements"
	"go.dalton.dog/gamelog/internal/providers/steam"
	"go.dalton.dog/gamelog/internal/providers/xbox"
)

// One client per credential set for the process (as ExophaseClientFor), so the
// RA throttle, PSN token and Xbox XUID carry across every game in a bulk run.
var (
	clientsMu    sync.Mutex
	raClients    = map[string]*retroachievements.RAClient{}
	steamClients = map[string]*steam.SteamClient{}
	psnClients   = map[string]*psn.PSNClient{}
	xblClients   = map[string]*xbox.XBLClient{}
)

// sharedClient returns m[key], creating it with mk on first use.
func sharedClient[T any](m map[string]*T, key string, mk func() *T) *T {
	clientsMu.Lock()
	defer clientsMu.Unlock()
	c, ok := m[key]
	if !ok {
		c = mk()
		m[key] = c
	}
	return c
}

func raClient(creds Credentials) *retroachievements.RAClient {
	return sharedClient(raClients, creds.RAUsername+"\x00"+creds.RAAPIKey, func() *retroachievements.RAClient {
		return &retroachievements.RAClient{Username: creds.RAUsername, APIKey: creds.RAAPIKey}
	})
}

func steamClient(creds Credentials) *steam.SteamClient {
	return sharedClient(steamClients, creds.SteamAPIKey+"\x00"+creds.SteamID, func() *steam.SteamClient {
		return &steam.SteamClient{APIKey: creds.SteamAPIKey, SteamID: creds.SteamID}
	})
}

func psnClient(creds Credentials) *psn.PSNClient {
	return sharedClient(psnClients, creds.PSNNpsso, func() *psn.PSNClient {
		return &psn.PSNClient{Npsso: creds.PSNNpsso}
	})
}

func xblClient(creds Credentials) *xbox.XBLClient {
	return sharedClient(xblClients, creds.XBLAPIKey, func() *xbox.XBLClient {
		return &xbox.XBLClient{APIKey: creds.XBLAPIKey}
	})
}

// forgetClients drops every shared client, so the next use starts fresh.
func forgetClients() {
	clientsMu.Lock()
	defer clientsMu.Unlock()
	clear(raClients)
	clear(steamClients)
	clear(psnClients)
	clear(xblClients)
}
