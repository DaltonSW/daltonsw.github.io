package model

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func ValidateDate(required bool) func(string) error {
	return func(s string) error {
		if s == "" {
			if required {
				return fmt.Errorf("a date is required (YYYY-MM-DD)")
			}
			return nil
		}
		if _, err := time.Parse("2006-01-02", s); err != nil {
			return fmt.Errorf("must be a real YYYY-MM-DD date or blank")
		}
		return nil
	}
}

func ValidateRating(s string) error {
	if s == "" {
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 10 {
		return fmt.Errorf("must be 1-10 or blank")
	}
	return nil
}

// Today is the current date in the site's timezone, so a date typed into a
// form matches the day the rest of the tool would report.
func Today() string {
	return time.Now().In(SiteLocation).Format("2006-01-02")
}

// ParseSubgames splits a newline-separated "Subgames" text field into a
// roster, trimming each line and dropping blanks, so a game with no roster
// round-trips to nil rather than an empty-but-non-nil slice. Exported so the
// web server's game-info forms can parse the same textarea shape without
// duplicating this logic.
func ParseSubgames(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func OrDash(s string) string {
	if s == "" {
		return "ongoing"
	}
	return s
}
