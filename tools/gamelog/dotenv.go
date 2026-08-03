package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// loadDotEnv reads KEY=VALUE pairs from the first .env file it finds, walking
// up from the working directory so the tool picks up tools/gamelog/.env
// whether it's run from there or from the repo root. Values already present
// in the real environment always win, so an explicit `RA_API_KEY=... gamelog`
// still overrides the file.
//
// Deliberately minimal: no export keywords, no variable interpolation, no
// multi-line values. The file only ever holds four flat credential strings,
// and a dependency for that would be the only non-indirect one in the module.
func loadDotEnv() {
	path, ok := findDotEnv()
	if !ok {
		return
	}
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, value, ok := parseDotEnvLine(scanner.Text())
		if !ok {
			continue
		}
		if _, set := os.LookupEnv(key); set {
			continue
		}
		os.Setenv(key, value)
	}
	// A read error mid-file just means fewer credentials are set, which the
	// caller already reports as "not configured".
	_ = scanner.Err()
}

// parseDotEnvLine splits one .env line into a key and value, reporting false
// for blanks, comments, and anything without an `=`.
func parseDotEnvLine(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	key, value, found := strings.Cut(line, "=")
	if !found {
		return "", "", false
	}
	key = strings.TrimSpace(strings.TrimPrefix(key, "export "))
	if key == "" {
		return "", "", false
	}
	return key, unquote(strings.TrimSpace(value)), true
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// findDotEnv looks for a .env beside the tool's source and then in each
// parent of the working directory, mirroring how findGamesDir locates the
// repo.
func findDotEnv() (string, bool) {
	dir, err := os.Getwd()
	if err != nil {
		return "", false
	}
	for {
		for _, candidate := range []string{
			filepath.Join(dir, ".env"),
			filepath.Join(dir, "tools", "gamelog", ".env"),
		} {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate, true
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}
