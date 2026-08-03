package dotenv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseDotEnvLine(t *testing.T) {
	cases := []struct {
		in         string
		key, value string
		ok         bool
	}{
		{"RA_API_KEY=abc123", "RA_API_KEY", "abc123", true},
		{"  RA_API_KEY = abc123  ", "RA_API_KEY", "abc123", true},
		{`STEAM_ID="765611980"`, "STEAM_ID", "765611980", true},
		{"STEAM_ID='765611980'", "STEAM_ID", "765611980", true},
		{"export STEAM_ID=765611980", "STEAM_ID", "765611980", true},
		{"EMPTY=", "EMPTY", "", true},
		{"# a comment", "", "", false},
		{"", "", "", false},
		{"   ", "", "", false},
		{"no_equals_sign", "", "", false},
		{"=novalue", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			key, value, ok := parseDotEnvLine(tc.in)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if ok && (key != tc.key || value != tc.value) {
				t.Errorf("got (%q, %q), want (%q, %q)", key, value, tc.key, tc.value)
			}
		})
	}
}

// A real environment variable must win over the file, so a one-off override
// on the command line still works.
func TestLoadDotEnv_DoesNotOverrideRealEnvironment(t *testing.T) {
	dir := t.TempDir()
	contents := "GAMELOG_TEST_PRESET=from-file\nGAMELOG_TEST_UNSET=from-file\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("GAMELOG_TEST_PRESET", "from-environment")

	LoadDotEnv()

	if got := os.Getenv("GAMELOG_TEST_PRESET"); got != "from-environment" {
		t.Errorf("preset var = %q, want it left as from-environment", got)
	}
	if got := os.Getenv("GAMELOG_TEST_UNSET"); got != "from-file" {
		t.Errorf("unset var = %q, want from-file", got)
	}
	os.Unsetenv("GAMELOG_TEST_UNSET")
}

// The tool is usually run from the repo root, where the .env lives two
// directories down beside the tool's source.
func TestFindDotEnv_LocatesToolsGamelogFromRepoRoot(t *testing.T) {
	root := t.TempDir()
	toolDir := filepath.Join(root, "tools", "gamelog")
	if err := os.MkdirAll(toolDir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(toolDir, ".env")
	if err := os.WriteFile(want, []byte("RA_USERNAME=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	got, ok := findDotEnv()
	if !ok {
		t.Fatal("expected to find tools/gamelog/.env")
	}
	if got != want {
		t.Errorf("found %q, want %q", got, want)
	}
}
