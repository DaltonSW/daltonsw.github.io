package model

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ignoredFilename holds the games that are deliberately never going to be
// logged. It lives in archive/ rather than content/ for the same reason the
// captured history does: it's keyed by provider IDs that never change, and a
// rename or delete in content/ must not be able to disturb it.
//
// It is not a data loss risk in the way the archive is — nothing captured is
// dropped, and un-ignoring a game brings it straight back into the scan — so
// unlike the archive records this file is rewritten whole.
const ignoredFilename = "ignored.yaml"

// IgnoredGame is one game excluded from the scan and backlog lists. Title and
// Reason are for the human reading the file or the un-ignore list; only
// Provider+ID are matched on.
type IgnoredGame struct {
	Provider  string `yaml:"provider"`
	ID        string `yaml:"id"`
	Title     string `yaml:"title,omitempty"`
	Reason    string `yaml:"reason,omitempty"`
	IgnoredOn string `yaml:"ignored_on,omitempty"`
}

// IgnoredList is the whole file. It's a struct rather than a bare slice so
// the YAML has a named key to hang a comment off and room to grow.
type IgnoredList struct {
	Games []IgnoredGame `yaml:"games"`
}

// IgnoredPath is where the list lives for a given archive directory.
func IgnoredPath(archiveDir string) string {
	return filepath.Join(archiveDir, ignoredFilename)
}

// LoadIgnored reads the ignore list, returning an empty list (not an error)
// when nothing has ever been ignored.
func LoadIgnored(archiveDir string) (IgnoredList, error) {
	var list IgnoredList
	raw, err := os.ReadFile(IgnoredPath(archiveDir))
	if err != nil {
		if os.IsNotExist(err) {
			return list, nil
		}
		return list, err
	}
	if err := yaml.Unmarshal(raw, &list); err != nil {
		return list, fmt.Errorf("%s: %w", IgnoredPath(archiveDir), err)
	}
	return list, nil
}

// Has reports whether this provider/id pair is on the list.
func (l IgnoredList) Has(provider, id string) bool {
	for _, g := range l.Games {
		if g.Provider == provider && g.ID == id {
			return true
		}
	}
	return false
}

// SaveIgnored writes the list back, sorted by provider then title so the file
// stays readable and diffs stay small.
func SaveIgnored(archiveDir string, list IgnoredList) error {
	sort.SliceStable(list.Games, func(i, j int) bool {
		a, b := list.Games[i], list.Games[j]
		if a.Provider != b.Provider {
			return a.Provider < b.Provider
		}
		if !strings.EqualFold(a.Title, b.Title) {
			return strings.ToLower(a.Title) < strings.ToLower(b.Title)
		}
		return a.ID < b.ID
	})
	body, err := yaml.Marshal(list)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return err
	}
	header := "# Games deliberately excluded from the scan and backlog lists.\n" +
		"# Nothing is deleted by being here — remove an entry and it comes back.\n"
	return os.WriteFile(IgnoredPath(archiveDir), append([]byte(header), body...), 0o644)
}

// AddIgnored puts one game on the list, or does nothing if it's already
// there. Returns whether the file changed.
func AddIgnored(archiveDir string, g IgnoredGame) (bool, error) {
	list, err := LoadIgnored(archiveDir)
	if err != nil {
		return false, err
	}
	if list.Has(g.Provider, g.ID) {
		return false, nil
	}
	if g.IgnoredOn == "" {
		g.IgnoredOn = time.Now().In(SiteLocation).Format("2006-01-02")
	}
	list.Games = append(list.Games, g)
	return true, SaveIgnored(archiveDir, list)
}

// RemoveIgnored takes a game back off the list, so the next scan offers it
// again. Returns whether anything was removed.
func RemoveIgnored(archiveDir, provider, id string) (bool, error) {
	list, err := LoadIgnored(archiveDir)
	if err != nil {
		return false, err
	}
	kept := make([]IgnoredGame, 0, len(list.Games))
	for _, g := range list.Games {
		if g.Provider == provider && g.ID == id {
			continue
		}
		kept = append(kept, g)
	}
	if len(kept) == len(list.Games) {
		return false, nil
	}
	list.Games = kept
	return true, SaveIgnored(archiveDir, list)
}
