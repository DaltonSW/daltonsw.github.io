package externalid

import (
	"fmt"
	"regexp"
	"strings"
)

// External IDs are numeric, but the natural thing to do is paste the URL the
// number came from, so accept both forms and keep only the digits.
var (
	raIDFromURL    = regexp.MustCompile(`retroachievements\.org/game/(\d+)`)
	steamIDFromURL = regexp.MustCompile(`store\.steampowered\.com/app/(\d+)`)
	allDigits      = regexp.MustCompile(`^\d+$`)
)

// ParseExternalID normalises a pasted RetroAchievements or Steam identifier
// into the bare numeric ID. Blank input stays blank — both fields are
// optional. An unrecognised value is returned as an error rather than being
// silently stored, since a wrong ID surfaces much later as a confusing API
// failure.
func ParseExternalID(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	for _, re := range []*regexp.Regexp{raIDFromURL, steamIDFromURL} {
		if m := re.FindStringSubmatch(s); m != nil {
			return m[1], nil
		}
	}
	if allDigits.MatchString(s) {
		return s, nil
	}
	return "", fmt.Errorf("expected a numeric ID or the game's URL")
}

// ValidateExternalID adapts ParseExternalID for huh's Validate hook.
func ValidateExternalID(s string) error {
	_, err := ParseExternalID(s)
	return err
}
