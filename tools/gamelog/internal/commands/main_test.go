package commands

import (
	"os"
	"testing"

	"go.dalton.dog/gamelog/internal/providers/steam"
	"go.dalton.dog/gamelog/internal/testutil"
)

// Every test runs offline: stubs only, never the real APIs.
func TestMain(m *testing.M) {
	// Keep tests off the developer's real Steam cache, as the steam package does.
	steam.LocalStatsDir = "/nonexistent-steam-stats"
	os.Exit(testutil.RunOffline(m))
}
