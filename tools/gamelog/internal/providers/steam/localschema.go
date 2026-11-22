package steam

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
)

// The Steam Web API blanks the description of hidden achievements. The Steam
// client's own cache, appcache/stats/UserGameStatsSchema_<appid>.bin, keeps the
// full text. It only exists for games the client has fetched a schema for, so
// every failure here is a soft one: callers fall back to what the API gave.

// LocalStatsDir overrides where the Steam client's stats cache is looked for.
// Empty means auto-detect (STEAM_STATS_DIR, then the usual Linux locations).
var LocalStatsDir string

// LocalAchievement is the English display text for one achievement.
type LocalAchievement struct {
	Name        string
	Description string
}

func statsDirs() []string {
	if LocalStatsDir != "" {
		return []string{LocalStatsDir}
	}
	if d := os.Getenv("STEAM_STATS_DIR"); d != "" {
		return []string{d}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(home, ".local/share/Steam/appcache/stats"),
		filepath.Join(home, ".steam/steam/appcache/stats"),
	}
}

// LoadLocalSchema returns English names/descriptions keyed by achievement API
// name, or nil if the game's schema isn't cached or can't be read.
func LoadLocalSchema(appID string) map[string]LocalAchievement {
	for _, dir := range statsDirs() {
		b, err := os.ReadFile(filepath.Join(dir, "UserGameStatsSchema_"+appID+".bin"))
		if err != nil {
			continue
		}
		return parseLocalSchema(b)
	}
	return nil
}

func parseLocalSchema(b []byte) map[string]LocalAchievement {
	root, err := parseBinaryVDF(b)
	if err != nil {
		return nil
	}
	out := map[string]LocalAchievement{}
	// <appid> -> stats -> <n> -> bits -> <m> -> {name, display{name,desc}}
	for _, app := range root {
		appObj, _ := app.(vdfObject)
		stats, _ := appObj["stats"].(vdfObject)
		for _, stat := range stats {
			statObj, _ := stat.(vdfObject)
			bits, _ := statObj["bits"].(vdfObject)
			for _, bit := range bits {
				ach, _ := bit.(vdfObject)
				key, _ := ach["name"].(string)
				display, _ := ach["display"].(vdfObject)
				if key == "" || display == nil {
					continue
				}
				out[key] = LocalAchievement{
					Name:        englishText(display["name"]),
					Description: englishText(display["desc"]),
				}
			}
		}
	}
	return out
}

func englishText(v any) string {
	switch t := v.(type) {
	case vdfObject:
		s, _ := t["english"].(string)
		return s
	case string:
		return t
	}
	return ""
}

// vdfObject is a decoded binary-VDF (KeyValues) object. Values are vdfObject,
// string, int32, float32 or uint64.
type vdfObject map[string]any

var errBadVDF = errors.New("malformed binary VDF")

func parseBinaryVDF(b []byte) (vdfObject, error) {
	pos := 0
	var cstr func() (string, error)
	cstr = func() (string, error) {
		for i := pos; i < len(b); i++ {
			if b[i] == 0 {
				s := string(b[pos:i])
				pos = i + 1
				return s, nil
			}
		}
		return "", errBadVDF
	}
	var obj func() (vdfObject, error)
	obj = func() (vdfObject, error) {
		d := vdfObject{}
		for pos < len(b) {
			t := b[pos]
			pos++
			if t == 8 {
				return d, nil
			}
			k, err := cstr()
			if err != nil {
				return nil, err
			}
			switch t {
			case 0:
				if d[k], err = obj(); err != nil {
					return nil, err
				}
			case 1:
				if d[k], err = cstr(); err != nil {
					return nil, err
				}
			case 2, 3:
				if pos+4 > len(b) {
					return nil, errBadVDF
				}
				v := binary.LittleEndian.Uint32(b[pos:])
				pos += 4
				d[k] = v
			case 7:
				if pos+8 > len(b) {
					return nil, errBadVDF
				}
				d[k] = binary.LittleEndian.Uint64(b[pos:])
				pos += 8
			default:
				return nil, errBadVDF
			}
		}
		return d, nil // top level ends at EOF
	}
	return obj()
}
