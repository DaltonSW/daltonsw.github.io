package commands

import (
	"sync"

	"go.dalton.dog/gamelog/internal/providers/exophase"
)

// exophaseClients holds one client per profile for the life of the process, so
// its memoized profile and games lists (see exophase.ExophaseClient) are
// shared by every fetch in a refresh-all run and every Housekeeping render,
// instead of each game re-reading the same pages.
var (
	exophaseMu      sync.Mutex
	exophaseClients = map[string]*exophase.ExophaseClient{}
)

// ExophaseClientFor returns the shared client for the configured profile.
func ExophaseClientFor(creds Credentials) *exophase.ExophaseClient {
	exophaseMu.Lock()
	defer exophaseMu.Unlock()
	c, ok := exophaseClients[creds.ExophaseUser]
	if !ok {
		c = &exophase.ExophaseClient{User: creds.ExophaseUser}
		exophaseClients[creds.ExophaseUser] = c
	}
	return c
}
