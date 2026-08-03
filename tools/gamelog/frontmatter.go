package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// FrontMatter is the authored layer of a game: the fields a human writes and
// the tool only ever reads. Playthrough history lives in playthroughs.yaml and
// captured API history lives in archive/, so nothing here is tool-generated
// past the moment the file is created.
type FrontMatter struct {
	Title    string `yaml:"title"`
	Platform string `yaml:"platform"`
	Status   string `yaml:"status"`
	Draft    bool   `yaml:"draft"`

	// Dates are typed as strings on purpose. YAML resolves an unquoted
	// 2026-01-04 to a timestamp, and decoding that into `any` yields a
	// time.Time whose text form is no longer what the file says.
	Started  string `yaml:"started"`
	Finished string `yaml:"finished"`

	// Rating and the external IDs are written bare (`steam_appid: 1145360`),
	// so they decode as ints. `any` plus scalarString keeps whichever form
	// the file used.
	Rating              any `yaml:"rating"`
	RetroAchievementsID any `yaml:"retroachievements_id"`
	SteamAppID          any `yaml:"steam_appid"`
}

// Doc is a loaded game entry. The tool used to hold this file open as a line
// buffer plus a yaml.Node tree so it could splice edits into it; it no longer
// writes front matter at all, so a plain decode is enough.
type Doc struct {
	Path string
	FM   FrontMatter
}

func LoadDoc(path string) (*Doc, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fm, err := parseFrontMatter(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &Doc{Path: path, FM: *fm}, nil
}

// parseFrontMatter decodes the YAML between the leading `---` delimiters,
// ignoring the Markdown body below them.
func parseFrontMatter(raw []byte) (*FrontMatter, error) {
	lines := strings.Split(string(raw), "\n")
	start, end := -1, -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "---" {
			if start == -1 {
				start = i
				continue
			}
			end = i
			break
		}
	}
	if start == -1 || end == -1 {
		return nil, fmt.Errorf("could not find front matter delimiters")
	}

	var fm FrontMatter
	text := strings.Join(lines[start+1:end], "\n")
	if err := yaml.Unmarshal([]byte(text), &fm); err != nil {
		return nil, fmt.Errorf("parsing front matter: %w", err)
	}
	return &fm, nil
}

// GameDir is the bundle directory holding this game's _index.md, its
// playthroughs.yaml, and any nested written playthrough pages.
func (d *Doc) GameDir() string { return filepath.Dir(d.Path) }

// ExternalIDs returns the game's optional RetroAchievements game ID and
// Steam appid, or "" for either that isn't set.
func (d *Doc) ExternalIDs() (raID, steamAppID string) {
	return scalarString(d.FM.RetroAchievementsID), scalarString(d.FM.SteamAppID)
}

// scalarString renders a decoded YAML scalar as the text a form would edit.
// Bare numbers decode as ints, and a missing key decodes as nil.
func scalarString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case uint64:
		return strconv.FormatUint(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return fmt.Sprint(t)
	}
}

// checkNoFieldLoss reports every path present in before but gone from after.
// allowedRemovals names the paths the caller knows it is removing — converting
// a playthrough to `sessions:` moves the flat date pair, and clearing a field
// drops the key. Anything else disappearing is a bug in the operation, and the
// write must not happen.
//
// A path whose value was already empty is not a loss: nothing was there to
// lose, and `omitempty` legitimately drops those on re-encode.
func checkNoFieldLoss(before, after map[string]string, allowedRemovals []string) error {
	allowed := make(map[string]bool, len(allowedRemovals))
	for _, p := range allowedRemovals {
		allowed[p] = true
	}
	var lost []string
	for path, val := range before {
		if val == "" || allowed[path] {
			continue
		}
		if _, kept := after[path]; kept {
			continue
		}
		lost = append(lost, fmt.Sprintf("  %s: %q", path, val))
	}
	if len(lost) == 0 {
		return nil
	}
	sort.Strings(lost)
	return fmt.Errorf("refusing to write — this edit would remove\n%s", strings.Join(lost, "\n"))
}
