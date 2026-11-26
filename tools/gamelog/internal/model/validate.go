package model

import (
	"fmt"
	"slices"
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

// ParseTags splits a comma- or newline-separated "Tags" field into a tag
// list: trimmed, lowercased, blanks and repeats dropped, first-seen order
// kept. No tags round-trips to nil, so the omitempty key stays absent.
func ParseTags(s string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '\n' }) {
		t := strings.ToLower(strings.TrimSpace(part))
		if t != "" && !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	return out
}

// ShrunkListPaths returns the loss-check paths that vanish when the list at
// key goes from oldLen items to newLen: key[newLen]..key[oldLen-1]. A list
// edited in place keeps its indices, so only the tail disappears.
func ShrunkListPaths(key string, oldLen, newLen int) []string {
	var paths []string
	for i := newLen; i < oldLen; i++ {
		paths = append(paths, fmt.Sprintf("%s[%d]", key, i))
	}
	return paths
}

func OrDash(s string) string {
	if s == "" {
		return "ongoing"
	}
	return s
}
