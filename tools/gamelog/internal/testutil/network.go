// Package testutil holds helpers shared by tests across packages.
package testutil

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
)

// RunOffline runs m with every non-loopback HTTP request refused, and fails
// the run if any was attempted, even when the code under test swallowed the
// error. Use it from TestMain: os.Exit(testutil.RunOffline(m)).
func RunOffline(m *testing.M) int {
	g := &offlineTransport{next: http.DefaultTransport}
	http.DefaultTransport = g
	code := m.Run()
	if len(g.blocked) > 0 {
		fmt.Fprintf(os.Stderr, "FAIL: tests tried to reach the internet (stub these endpoints):\n  %s\n",
			strings.Join(g.blocked, "\n  "))
		if code == 0 {
			code = 1
		}
	}
	return code
}

type offlineTransport struct {
	next    http.RoundTripper
	mu      sync.Mutex
	blocked []string
}

func (t *offlineTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	host := r.URL.Hostname()
	if ip := net.ParseIP(host); host == "localhost" || (ip != nil && ip.IsLoopback()) {
		return t.next.RoundTrip(r)
	}
	// The query is dropped: it can carry API keys.
	u := r.URL.Scheme + "://" + r.URL.Host + r.URL.Path
	t.mu.Lock()
	t.blocked = append(t.blocked, r.Method+" "+u)
	t.mu.Unlock()
	return nil, fmt.Errorf("testutil: external request blocked in tests: %s", u)
}
