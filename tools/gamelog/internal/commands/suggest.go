package commands

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"go.dalton.dog/gamelog/internal/dotenv"
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

	// NinjaKiwiOAK is an "OAK" save-export token for Ninja Kiwi's data API
	// (data.ninjakiwi.com), obtained in-game (BTD6: Settings → Data → Export
	// Save) and bound to one player's save. A real secret, and short-lived —
	// it expires roughly every 90 days and has to be re-exported and pasted
	// back into .env by hand.
	NinjaKiwiOAK string
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
		NinjaKiwiOAK:  os.Getenv("NINJA_KIWI_OAK"),
	}
}

// day formats an instant as the calendar date it fell on in the site's
// timezone.
func day(t time.Time) string {
	return t.In(model.SiteLocation).Format("2006-01-02")
}

// Day is day for callers outside this package — the web UI's pre-filled
// suggestion forms must land on the same calendar date as everything else.
func Day(t time.Time) string { return day(t) }

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

// OAKConfigured needs just the token — it's self-contained, unlike Nadeo's
// login/password exchange.
func (c Credentials) OAKConfigured() bool { return c.NinjaKiwiOAK != "" }

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

	// Coverage is set by WithCoverage; RA/Steam's Started/Finished then span
	// only the activity no logged playthrough accounts for.
	Coverage *Coverage
}

// SuggestionReport is the suggested-dates panel's result for one game.
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

func FetchRAResult(ctx context.Context, raID string, creds Credentials) ProviderResult {
	if raID == "" {
		return ProviderResult{Skipped: true, Reason: "no retroachievements_id set on this game"}
	}
	if !creds.RAConfigured() {
		return ProviderResult{Skipped: true, Reason: "RA_USERNAME/RA_API_KEY not set"}
	}
	client := raClient(creds)
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
	client := steamClient(creds)
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

func FormatHours(minutes int) string {
	return fmt.Sprintf("%.1fh", float64(minutes)/60.0)
}

// CoveredSummary is the suggest panel's "nothing new to log" line.
func CoveredSummary(c *Coverage) string {
	out := fmt.Sprintf("All %d activity day(s) fall inside logged playthroughs.", c.Days)
	if c.LatestOpenIn >= 0 {
		out += fmt.Sprintf(" Latest activity %s is inside #%d, which has no finish date — set one there if that run is done.",
			c.Latest, c.LatestOpenIn+1)
	}
	return out
}
