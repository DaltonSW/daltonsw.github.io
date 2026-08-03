package main

import "testing"

func TestParseExternalID(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", "", false},
		{"  ", "", false},
		{"4650", "4650", false},
		{"  1145360  ", "1145360", false},
		{"https://retroachievements.org/game/4650", "4650", false},
		{"retroachievements.org/game/36125", "36125", false},
		{"https://store.steampowered.com/app/1145360/Hades/", "1145360", false},
		{"https://store.steampowered.com/app/1145360", "1145360", false},
		{"not-an-id", "", true},
		{"https://example.com/game/123", "", true},
		{"4650x", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseExternalID(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q, got %q", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("parseExternalID(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestGameSummary_SuggestLabel(t *testing.T) {
	base := GameSummary{Title: "Hades", Status: "finished", Finished: "2026-03-20"}

	linkedBoth := base
	linkedBoth.RAGameID, linkedBoth.SteamAppID = "4650", "1145360"
	if got := linkedBoth.SuggestLabel(); got != "Hades — finished 2026-03-20  [RA+Steam]" {
		t.Errorf("both linked: got %q", got)
	}

	steamOnly := base
	steamOnly.SteamAppID = "1145360"
	if got := steamOnly.SuggestLabel(); got != "Hades — finished 2026-03-20  [Steam]" {
		t.Errorf("steam only: got %q", got)
	}

	if got := base.SuggestLabel(); got != "Hades — finished 2026-03-20  [not linked]" {
		t.Errorf("unlinked: got %q", got)
	}
}
