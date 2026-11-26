package model

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// statsSummaryFilename is the projection of a game's per-game stats, written
// beside achievement-summary.yaml for Hugo to read. Kept separate from it:
// stats are not achievements, and a game like Team Fortress 2 has hundreds that
// every games-list build would otherwise parse for nothing. Same
// publishResources: false cascade, same regenerable-from-archive rule.
const statsSummaryFilename = "stats-summary.yaml"

// StatsSummary is what stats-summary.yaml holds.
type StatsSummary struct {
	Providers []StatsProvider `yaml:"providers"`
}

// StatsProvider is one provider's reported stats for the game.
type StatsProvider struct {
	Provider string      `yaml:"provider"`
	Platform string      `yaml:"platform,omitempty"`
	Fetched  string      `yaml:"fetched,omitempty"`
	Stats    []StatEntry `yaml:"stats"`
}

// StatEntry is one stat with a reported value. Stats the game defines but the
// player has no reading for stay in the archive and are left out here: a row
// of blanks says nothing, and the archive still has them if a layout wants them.
type StatEntry struct {
	Key   string  `yaml:"key"`
	Name  string  `yaml:"name,omitempty"`
	Value float64 `yaml:"value"`
}

func statsSummaryPath(gameDir string) string {
	return filepath.Join(gameDir, statsSummaryFilename)
}

// writeStatsSummary recomputes the projection from whatever is archived for the
// game's linked records. A game with no reported stats gets no file, and a stale
// one is removed.
func writeStatsSummary(archiveDir, gameDir string, links []ProviderLink) error {
	var providers []StatsProvider
	for _, link := range links {
		if link.ID == "" || link.Subset {
			continue
		}
		rec, err := LoadRecord(archiveDir, link.Provider, link.ID)
		if err != nil {
			return err
		}
		if rec == nil {
			continue
		}
		var entries []StatEntry
		for _, s := range rec.Stats {
			if s.Value == nil {
				continue
			}
			entries = append(entries, StatEntry{Key: s.Key, Name: s.Name, Value: *s.Value})
		}
		if len(entries) == 0 {
			continue
		}
		providers = append(providers, StatsProvider{
			Provider: link.Provider,
			Platform: rec.Platform,
			Fetched:  rec.Fetched,
			Stats:    entries,
		})
	}

	path := statsSummaryPath(gameDir)
	if len(providers) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	out, err := yaml.Marshal(StatsSummary{Providers: providers})
	if err != nil {
		return err
	}
	return writeFileAtomic(path, out, 0o644)
}
