package model

import (
	"os"
	"strings"
	"testing"
)

func TestLoadIgnored_MissingFileIsEmptyNotAnError(t *testing.T) {
	list, err := LoadIgnored(t.TempDir())
	if err != nil {
		t.Fatalf("missing ignore list should not be an error: %v", err)
	}
	if len(list.Games) != 0 {
		t.Fatalf("expected an empty list, got %d", len(list.Games))
	}
}

func TestAddIgnored_RoundTripsAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()

	added, err := AddIgnored(dir, IgnoredGame{Provider: ProviderSteam, ID: "440", Title: "Team Fortress 2"})
	if err != nil || !added {
		t.Fatalf("first add: added=%v err=%v", added, err)
	}
	added, err = AddIgnored(dir, IgnoredGame{Provider: ProviderSteam, ID: "440", Title: "Team Fortress 2"})
	if err != nil {
		t.Fatal(err)
	}
	if added {
		t.Error("adding the same game twice should be a no-op")
	}

	list, err := LoadIgnored(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Games) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(list.Games))
	}
	if !list.Has(ProviderSteam, "440") {
		t.Error("Has should find the entry it just wrote")
	}
	if list.Has(ProviderRA, "440") {
		t.Error("Has must be per-provider, not ID-only")
	}
	if list.Games[0].IgnoredOn == "" {
		t.Error("expected an ignored_on date to be stamped")
	}

	raw, err := os.ReadFile(IgnoredPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(raw), "#") {
		t.Error("expected the explanatory header comment to survive a write")
	}
}

func TestRemoveIgnored_PutsTheGameBack(t *testing.T) {
	dir := t.TempDir()
	if _, err := AddIgnored(dir, IgnoredGame{Provider: ProviderSteam, ID: "440"}); err != nil {
		t.Fatal(err)
	}
	if _, err := AddIgnored(dir, IgnoredGame{Provider: ProviderRA, ID: "104"}); err != nil {
		t.Fatal(err)
	}

	removed, err := RemoveIgnored(dir, ProviderSteam, "440")
	if err != nil || !removed {
		t.Fatalf("remove: removed=%v err=%v", removed, err)
	}
	removed, err = RemoveIgnored(dir, ProviderSteam, "440")
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Error("removing something that isn't there should report no change")
	}

	list, err := LoadIgnored(dir)
	if err != nil {
		t.Fatal(err)
	}
	if list.Has(ProviderSteam, "440") {
		t.Error("removed entry should be gone")
	}
	if !list.Has(ProviderRA, "104") {
		t.Error("removing one entry must not disturb the others")
	}
}
