package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// FrontMatter is the authored layer of a game: the fields a human writes.
// The tool only read this after creation until the review/edit flow added a
// write path — see Doc.Save.
type FrontMatter struct {
	Title               string `yaml:"title"`
	Platform            string `yaml:"platform"`
	RetroAchievementsID any    `yaml:"retroachievements_id"`
	SteamAppID          any    `yaml:"steam_appid"`
	Status              string `yaml:"status"`

	// Dates are typed as strings on purpose. YAML resolves an unquoted
	// 2026-01-04 to a timestamp, and decoding that into `any` yields a
	// time.Time whose text form is no longer what the file says.
	Started  string `yaml:"started"`
	Finished string `yaml:"finished"`

	// Rating is written bare (`rating: 9`), so it decodes as an int. `any`
	// plus scalarString keeps whichever form the file used.
	Rating any  `yaml:"rating"`
	Draft  bool `yaml:"draft"`

	// Extra preserves keys this struct doesn't model — cover, cascade, and
	// anything else. Without it, a rewrite would silently drop them, the same
	// class of loss Extra already prevents on PlaythroughEntry.
	Extra map[string]any `yaml:",inline"`
}

// RatingString renders the rating as the form the TUI edits.
func (f FrontMatter) RatingString() string { return scalarString(f.Rating) }

// SetRating stores a rating typed as text, as a number when it is one, so
// the file keeps `rating: 9` rather than `rating: "9"`.
func (f *FrontMatter) SetRating(s string) {
	s = strings.TrimSpace(s)
	if s == "" {
		f.Rating = nil
		return
	}
	if n, err := strconv.Atoi(s); err == nil {
		f.Rating = n
		return
	}
	f.Rating = s
}

// Doc is a loaded game entry. fmRaw/prefix/suffix are kept so a front-matter
// edit can be written back as a splice — replacing only the front-matter
// block — rather than reconstructing the file from the decoded struct, which
// would risk rewriting hand-written prose in the body below the delimiters.
type Doc struct {
	Path string
	FM   FrontMatter

	fmRaw  []byte // front-matter YAML exactly as read — the "before" side of the loss-check
	prefix []byte // raw bytes through the end of the opening "---\n" line
	suffix []byte // raw bytes from the closing "---" line through EOF (delimiter + markdown body)
}

func LoadDoc(path string) (*Doc, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fm, prefix, fmRaw, suffix, err := parseFrontMatter(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &Doc{Path: path, FM: *fm, fmRaw: fmRaw, prefix: prefix, suffix: suffix}, nil
}

// parseFrontMatter splits raw into the bytes before the front matter (the
// opening delimiter line), the front-matter YAML text itself, and the bytes
// from the closing delimiter onward (which includes the markdown body). The
// three concatenate back to raw exactly, so a rewrite can replace only the
// middle piece.
func parseFrontMatter(raw []byte) (fm *FrontMatter, prefix, fmRaw, suffix []byte, err error) {
	text := string(raw)
	lines := strings.SplitAfter(text, "\n")

	start, end := -1, -1
	offset := 0
	starts := make([]int, len(lines))
	for i, l := range lines {
		starts[i] = offset
		if strings.TrimSpace(l) == "---" {
			if start == -1 {
				start = i
			} else {
				end = i
				break
			}
		}
		offset += len(l)
	}
	if start == -1 || end == -1 {
		return nil, nil, nil, nil, fmt.Errorf("could not find front matter delimiters")
	}

	prefixEnd := starts[start] + len(lines[start])
	fmEnd := starts[end]
	prefix = raw[:prefixEnd]
	fmRaw = raw[prefixEnd:fmEnd]
	suffix = raw[fmEnd:]

	var decoded FrontMatter
	if err := yaml.Unmarshal(fmRaw, &decoded); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("parsing front matter: %w", err)
	}
	return &decoded, prefix, fmRaw, suffix, nil
}

// encodeFM renders just the front-matter YAML block, matching what
// collectYAMLFields expects to compare against fmRaw.
func (d *Doc) encodeFM() ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(d.FM); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Encode renders the whole file as Save would write it. Only the block
// between prefix and suffix changes — the markdown body is sliced from the
// original bytes, never reconstructed.
func (d *Doc) Encode() ([]byte, error) {
	fm, err := d.encodeFM()
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(d.prefix)+len(fm)+len(d.suffix))
	out = append(out, d.prefix...)
	out = append(out, fm...)
	out = append(out, d.suffix...)
	return out, nil
}

// Save writes the file atomically, splicing the current FM back into the
// original bytes. This is a whole-file rewrite of the only copy of this
// data, so it uses the same atomic-write helper playthroughs.go does.
func (d *Doc) Save() error {
	out, err := d.Encode()
	if err != nil {
		return err
	}
	return writeFileAtomic(d.Path, out, 0o644)
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
