package server

import (
	"os"
	"testing"

	"go.dalton.dog/gamelog/internal/testutil"
)

// Every test runs offline: stubs only, never the real APIs.
func TestMain(m *testing.M) {
	os.Exit(testutil.RunOffline(m))
}
