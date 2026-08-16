package commands

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"go.dalton.dog/gamelog/internal/dotenv"
	"go.dalton.dog/gamelog/internal/externalid"
	"go.dalton.dog/gamelog/internal/forms"
	"go.dalton.dog/gamelog/internal/model"
	"go.dalton.dog/gamelog/internal/providers/retroachievements"
	"go.dalton.dog/gamelog/internal/providers/steam"
)

// Credentials holds API credentials for both providers, read once from the
// environment. This is the only os.Getenv call site in the tool, which
// keeps everything downstream testable without touching the environment.
type Credentials struct {
	RAUsername  string
	RAAPIKey    string
	SteamAPIKey string
	SteamID     string

	// ExophaseUser is a public profile name, not a secret. Neither PlayStation
	// nor Ubisoft has a public achievements API, so both PSN trophy history
	// and Ubisoft Connect achievement history come from the same public
	// Exophase account — there is nothing to authenticate with, which is why
	// this is the only "credential" that is safe to be wrong in public.
	ExophaseUser string

	// XBLAPIKey is a personal key from xbl.io, scoped to whichever Microsoft
	// account signed in there to create it. Unlike ExophaseUser this is a
	// real secret — OpenXBL has no public-profile mode, only the caller's
	// own account.
	XBLAPIKey string

	// PSNNpsso is the npsso session value from a logged-in
	// ca.account.sony.com session (~2 month lifetime). A real secret, unlike
	// ExophaseUser: it's exchanged for a bearer token good for Sony's own
	// trophy API, not a public profile name.
	PSNNpsso string

	// NadeoLogin/NadeoPassword are a Trackmania *service account*, created at
	// trackmania.com and bound to a real Ubisoft account whose permissions it
	// inherits. The password is a real secret and is shown exactly once at
	// creation.
	//
	// Because it acts as that player, misusing it risks restrictions on an
	// account someone actually plays on — which is why the nadeo client throttles
	// harder than the others and why `nadeo fetch` defaults to one season.
	NadeoLogin    string
	NadeoPassword string
}

func LoadCredentials() Credentials {
	dotenv.LoadDotEnv()
	return Credentials{
		RAUsername:  os.Getenv("RA_USERNAME"),
		RAAPIKey:    os.Getenv("RA_API_KEY"),
		SteamAPIKey: os.Getenv("STEAM_API_KEY"),
		// STEAM_USER_ID is accepted as an alias: it's the name a Steam
		// profile URL actually suggests, and either may hold a SteamID64
		// or a vanity name (resolved later by SteamClient).
		SteamID: model.FirstNonEmpty(os.Getenv("STEAM_ID"), os.Getenv("STEAM_USER_ID")),
		// EXOPHASE_PSN_USER is accepted as an alias, from when this only
		// covered PSN.
		ExophaseUser:  model.FirstNonEmpty(os.Getenv("EXOPHASE_USER"), os.Getenv("EXOPHASE_PSN_USER")),
		XBLAPIKey:     os.Getenv("XBLIO_API_KEY"),
		PSNNpsso:      os.Getenv("PSN_NPSSO"),
		NadeoLogin:    os.Getenv("NADEO_SERVICE_LOGIN"),
		NadeoPassword: os.Getenv("NADEO_SERVICE_PASSWORD"),
	}
}

// day formats an instant as the calendar date it fell on in the site's
// timezone.
func day(t time.Time) string {
	return t.In(model.SiteLocation).Format("2006-01-02")
}

func (c Credentials) RAConfigured() bool    { return c.RAUsername != "" && c.RAAPIKey != "" }
func (c Credentials) SteamConfigured() bool { return c.SteamAPIKey != "" && c.SteamID != "" }

// ExophaseConfigured needs only a username — the profile is public.
func (c Credentials) ExophaseConfigured() bool { return c.ExophaseUser != "" }

func (c Credentials) XBLConfigured() bool { return c.XBLAPIKey != "" }

// PSNConfigured needs only the npsso value — the OAuth exchange happens at
// request time in the psn client.
func (c Credentials) PSNConfigured() bool { return c.PSNNpsso != "" }

// NadeoConfigured needs both halves of the service account; the token exchange
// happens at request time in the nadeo client.
func (c Credentials) NadeoConfigured() bool { return c.NadeoLogin != "" && c.NadeoPassword != "" }

// ProviderResult is one provider's outcome for a suggestion report: either
// a usable suggestion, or a reason it was skipped/failed. A skipped or
// errored provider never aborts the command — the report just explains why.
type ProviderResult struct {
	Skipped bool
	Reason  string // set when Skipped
	Errored bool
	Err     error // set when Errored

	RA    *retroachievements.RASuggestion
	Steam *steam.SteamSuggestion

	SteamPlaytimeMinutes int
	SteamPlaytimeKnown   bool
}

// SuggestionReport is the full result of `gamelog suggest` for one game.
type SuggestionReport struct {
	Title           string
	Slug            string
	NumPlaythroughs int
	RAGameID        string
	RAUsername      string
	RA              ProviderResult
	SteamAppID      string
	Steam           ProviderResult
}

func RunSuggest(args []string) error {
	gamesDir, err := model.FindGamesDir()
	if err != nil {
		return err
	}
	games, err := model.ListGames(gamesDir)
	if err != nil {
		return err
	}

	slug, err := resolveSuggestSlug(args, games)
	if err != nil {
		return err
	}

	var summary model.GameSummary
	found := false
	for _, g := range games {
		if g.Slug == slug {
			summary = g
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("no game %q found. Known games: %s", slug, joinSlugs(games))
	}

	doc, err := model.LoadDoc(summary.Path)
	if err != nil {
		return err
	}
	raID, steamAppID := doc.ExternalIDs()
	// Front matter may have been hand-edited with a pasted URL rather than a
	// bare ID; normalise so both work. A value that parses as neither is left
	// as-is and allowed to fail loudly at the provider.
	if id, err := externalid.ParseExternalID(raID); err == nil {
		raID = id
	}
	if id, err := externalid.ParseExternalID(steamAppID); err == nil {
		steamAppID = id
	}
	creds := LoadCredentials()

	report := SuggestionReport{
		Title:           summary.Title,
		Slug:            slug,
		NumPlaythroughs: summary.NumPlaythroughs,
		RAGameID:        raID,
		RAUsername:      creds.RAUsername,
		SteamAppID:      steamAppID,
	}

	ctx := context.Background()
	report.RA = FetchRAResult(ctx, raID, creds)
	report.Steam = FetchSteamResult(ctx, steamAppID, creds)

	fmt.Print(FormatSuggestionReport(report))
	return nil
}

func resolveSuggestSlug(args []string, games []model.GameSummary) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}
	return forms.SelectExistingGame(games)
}

func joinSlugs(games []model.GameSummary) string {
	slugs := make([]string, len(games))
	for i, g := range games {
		slugs[i] = g.Slug
	}
	return strings.Join(slugs, ", ")
}

func FetchRAResult(ctx context.Context, raID string, creds Credentials) ProviderResult {
	if raID == "" {
		return ProviderResult{Skipped: true, Reason: "no retroachievements_id set on this game"}
	}
	if !creds.RAConfigured() {
		return ProviderResult{Skipped: true, Reason: "RA_USERNAME/RA_API_KEY not set"}
	}
	client := &retroachievements.RAClient{Username: creds.RAUsername, APIKey: creds.RAAPIKey}
	progress, err := client.GetGameProgress(ctx, raID)
	if err != nil {
		return ProviderResult{Errored: true, Err: err}
	}
	suggestion := retroachievements.RASuggestRange(progress)
	if !suggestion.OK {
		return ProviderResult{Skipped: true, Reason: "no achievements earned yet for this game"}
	}
	return ProviderResult{RA: &suggestion}
}

func FetchSteamResult(ctx context.Context, appID string, creds Credentials) ProviderResult {
	if appID == "" {
		return ProviderResult{Skipped: true, Reason: "no steam_appid set on this game"}
	}
	if !creds.SteamConfigured() {
		return ProviderResult{Skipped: true, Reason: "STEAM_API_KEY/STEAM_ID not set"}
	}
	client := &steam.SteamClient{APIKey: creds.SteamAPIKey, SteamID: creds.SteamID}
	result, err := client.GetPlayerAchievements(ctx, appID)
	if err != nil {
		return ProviderResult{Errored: true, Err: err}
	}
	if !result.Success {
		reason := "achievement data unavailable (private profile, or this game has no achievements)"
		if result.Error != "" {
			reason = strings.ToLower(strings.TrimRight(result.Error, "."))
		}
		return ProviderResult{Skipped: true, Reason: reason}
	}

	pr := ProviderResult{}
	if minutes, found, err := client.GetOwnedGamePlaytime(ctx, appID); err == nil && found {
		pr.SteamPlaytimeMinutes = minutes
		pr.SteamPlaytimeKnown = true
	}

	suggestion := steam.SteamSuggestRange(result.Achievements)
	if !suggestion.OK {
		pr.Skipped = true
		if pr.SteamPlaytimeKnown {
			pr.Reason = fmt.Sprintf("no achievements unlocked yet; total playtime %s available but no date signal", FormatHours(pr.SteamPlaytimeMinutes))
		} else {
			pr.Reason = "no achievements unlocked yet"
		}
		return pr
	}
	pr.Steam = &suggestion
	return pr
}

func FormatSuggestionReport(r SuggestionReport) string {
	var b strings.Builder
	plural := "s"
	if r.NumPlaythroughs == 1 {
		plural = ""
	}
	fmt.Fprintf(&b, "Suggestions for %q (%s) — %d playthrough%s logged\n\n", r.Title, r.Slug, r.NumPlaythroughs, plural)

	b.WriteString(formatRABlock(r))
	b.WriteString("\n")
	b.WriteString(formatSteamBlock(r))
	b.WriteString("\n")

	if r.NumPlaythroughs > 1 {
		b.WriteString("Note: this game has multiple playthroughs logged. These dates reflect your\n")
		b.WriteString("ENTIRE RetroAchievements/Steam history for this game, not any single\n")
		b.WriteString("playthrough — cross-reference manually before entering them via `gamelog`.\n\n")
	}

	fmt.Fprintf(&b, "Enter these manually: run `gamelog`, pick %s, then \"Log a new session\"\n", r.Title)
	b.WriteString("or \"Update a playthrough\".\n")
	return b.String()
}

func formatRABlock(r SuggestionReport) string {
	var b strings.Builder
	if r.RAGameID == "" {
		b.WriteString("RetroAchievements\n")
	} else {
		fmt.Fprintf(&b, "RetroAchievements (game %s, user %s)\n", r.RAGameID, r.RAUsername)
	}

	switch {
	case r.RA.Errored:
		fmt.Fprintf(&b, "  request failed (%v), skipping.\n", r.RA.Err)
	case r.RA.Skipped:
		fmt.Fprintf(&b, "  %s, skipping.\n", r.RA.Reason)
	case r.RA.RA != nil:
		s := r.RA.RA
		confidenceLabel := "high (award earned)"
		if s.AwardKind != "" {
			confidenceLabel = fmt.Sprintf("high (%s)", s.AwardKind)
		}
		lastLineLabel := "award date                  : "
		if s.Confidence != "high" {
			confidenceLabel = "medium (last known activity, not confirmed complete)"
			lastLineLabel = "latest achievement earned   : "
		}
		fmt.Fprintf(&b, "  confidence: %s\n", confidenceLabel)
		fmt.Fprintf(&b, "  earliest achievement earned : %s\n", day(s.Started))
		fmt.Fprintf(&b, "  %s%s\n", lastLineLabel, day(s.Finished))
		fmt.Fprintf(&b, "  -> suggested started : %s\n", day(s.Started))
		fmt.Fprintf(&b, "  -> suggested finished: %s\n", day(s.Finished))
	}
	return b.String()
}

func formatSteamBlock(r SuggestionReport) string {
	var b strings.Builder
	if r.SteamAppID == "" {
		b.WriteString("Steam\n")
	} else {
		fmt.Fprintf(&b, "Steam (appid %s)\n", r.SteamAppID)
	}

	switch {
	case r.Steam.Errored:
		fmt.Fprintf(&b, "  request failed (%v), skipping.\n", r.Steam.Err)
	case r.Steam.Skipped:
		fmt.Fprintf(&b, "  %s, skipping.\n", r.Steam.Reason)
	case r.Steam.Steam != nil:
		s := r.Steam.Steam
		b.WriteString("  confidence: low (guess from achievement unlock times, not a real playtime timeline)\n")
		fmt.Fprintf(&b, "  achievements unlocked        : %d / %d\n", s.UnlockedCount, s.TotalCount)
		fmt.Fprintf(&b, "  earliest unlock              : %s\n", day(s.Started))
		fmt.Fprintf(&b, "  latest unlock                : %s\n", day(s.Finished))
		if r.Steam.SteamPlaytimeKnown {
			fmt.Fprintf(&b, "  total playtime (all-time)    : %s\n", FormatHours(r.Steam.SteamPlaytimeMinutes))
		}
		fmt.Fprintf(&b, "  -> suggested started : %s\n", day(s.Started))
		weak := ""
		if s.UnlockedCount < s.TotalCount {
			weak = "  (weak — not all achievements earned, may still be playing)"
		}
		fmt.Fprintf(&b, "  -> suggested finished: %s%s\n", day(s.Finished), weak)
	}
	return b.String()
}

func FormatHours(minutes int) string {
	return fmt.Sprintf("%.1fh", float64(minutes)/60.0)
}
