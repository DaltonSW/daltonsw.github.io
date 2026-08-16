package nadeo

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.dalton.dog/gamelog/internal/model"
)

// The same three time helpers every provider package carries. Duplicated
// rather than shared so a provider can't accidentally change how another one
// reports dates.
func today() string          { return time.Now().In(model.SiteLocation).Format("2006-01-02") }
func day(t time.Time) string { return t.In(model.SiteLocation).Format("2006-01-02") }

// Stage names the phase a fetch is in, for progress reporting.
type Stage string

const (
	StageAuth      Stage = "auth"
	StageCampaigns Stage = "campaigns"
	StageMaps      Stage = "maps"
	StageRecords   Stage = "records"
	StageSeason    Stage = "season"
)

// Event is one progress update. Total is 0 when the stage has no known size
// yet. Detail carries a season name for StageSeason.
type Event struct {
	Stage  Stage
	Done   int
	Total  int
	Detail string
}

// FetchOptions selects what a run covers.
//
// The zero value means "the current season only" — deliberately the cheapest
// option, because that's the only season whose times change once a campaign has
// closed, and because every request here is made against a real player's
// account. A full backfill has to be asked for.
type FetchOptions struct {
	// Seasons names campaigns to fetch, by name ("Fall 2024") or season UID.
	Seasons []string
	// All fetches every official campaign.
	All bool
	// Since fetches every campaign starting in this year or later. Zero is off.
	Since int

	OnEvent func(Event)
}

func (o FetchOptions) emit(e Event) {
	if o.OnEvent != nil {
		o.OnEvent(e)
	}
}

// selectCampaigns narrows the enumerated campaigns to what the options ask for.
// Split out from FetchRecord so the selection rules are testable without a
// server: getting this wrong means either a needless 40-request backfill or
// silently fetching the wrong season.
func selectCampaigns(all []Campaign, opts FetchOptions) ([]Campaign, error) {
	switch {
	case opts.All:
		return all, nil

	case len(opts.Seasons) > 0:
		// Match on UID first, then case-insensitively on name, so both
		// `--season "Fall 2024"` and a pasted UID work.
		byUID := make(map[string]Campaign, len(all))
		byName := make(map[string]Campaign, len(all))
		for _, c := range all {
			byUID[c.SeasonUID] = c
			byName[strings.ToLower(c.Name)] = c
		}
		var out []Campaign
		for _, want := range opts.Seasons {
			if c, ok := byUID[want]; ok {
				out = append(out, c)
				continue
			}
			if c, ok := byName[strings.ToLower(strings.TrimSpace(want))]; ok {
				out = append(out, c)
				continue
			}
			return nil, fmt.Errorf("no official campaign named %q — run `gamelog nadeo seasons` to list them", want)
		}
		return out, nil

	case opts.Since > 0:
		var out []Campaign
		for _, c := range all {
			if c.StartTimestamp > 0 && time.Unix(c.StartTimestamp, 0).In(model.SiteLocation).Year() >= opts.Since {
				out = append(out, c)
			}
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("no official campaign starts in %d or later", opts.Since)
		}
		return out, nil

	default:
		// Campaigns comes back oldest-first, so the current season is the last.
		if len(all) == 0 {
			return nil, fmt.Errorf("nadeo returned no official campaigns")
		}
		return all[len(all)-1:], nil
	}
}

// FetchRecord captures campaign history for the authenticated account.
//
// Following this tool's contract for every provider: a fetch that fails is not
// an error. It records LastError on the returned record and returns (rec, nil),
// leaving SaveNadeoRecord to keep whatever is already archived. A non-nil error
// is reserved for things that should abort the run — bad credentials, or a
// --season that names no real campaign.
func FetchRecord(ctx context.Context, c *Client, opts FetchOptions) (*model.NadeoRecord, error) {
	rec := &model.NadeoRecord{
		Provider:    model.ProviderNadeo,
		Title:       "Trackmania",
		LastAttempt: today(),
	}

	opts.emit(Event{Stage: StageAuth})
	accountID, err := c.AccountID(ctx)
	if err != nil {
		// Credentials are wrong or missing — nothing else in this run can work,
		// and it isn't a transient provider failure.
		return nil, err
	}
	rec.ID = accountID

	opts.emit(Event{Stage: StageCampaigns})
	all, campaignRaw, err := c.Campaigns(ctx)
	if err != nil {
		rec.LastError = err.Error()
		return rec, nil
	}
	rec.Raw = &model.NadeoRaw{Campaigns: campaignRaw}
	opts.emit(Event{Stage: StageCampaigns, Done: len(all), Total: len(all)})

	selected, err := selectCampaigns(all, opts)
	if err != nil {
		return nil, err
	}

	// One flat list of map UIDs, and one flat list of (map, season) pairs. Both
	// are batched across season boundaries so a 25-track season doesn't cost a
	// request of its own in a full backfill.
	var uids []string
	var refs []MapRef
	seen := map[string]bool{}
	for _, c := range selected {
		for _, p := range c.Playlist {
			if !seen[p.MapUID] {
				seen[p.MapUID] = true
				uids = append(uids, p.MapUID)
			}
			refs = append(refs, MapRef{MapUID: p.MapUID, GroupUID: c.SeasonUID})
		}
	}

	opts.emit(Event{Stage: StageMaps, Total: len(uids)})
	maps, mapRaw, err := c.MapInfos(ctx, uids, func(done int) {
		opts.emit(Event{Stage: StageMaps, Done: done, Total: len(uids)})
	})
	if err != nil {
		rec.LastError = err.Error()
		return rec, nil
	}
	rec.Raw.Maps = mapRaw

	// Records are captured at both scopes, because neither can be derived from
	// the other: the season-scoped time is what was achieved while a campaign
	// was open and freezes when it closes, while the all-time best keeps moving
	// whenever an old track gets revisited to clean up a medal. The gap between
	// them *is* the record of having gone back.
	//
	// They have to be two passes: mapIdList and seasonIdList both filter every
	// result, so sending them together intersects rather than unions.
	//
	// The Live leaderboard is the fallback for the season pass — it needs a
	// request per 50 maps and carries no dates, but it returns zone rankings,
	// which the Core endpoint omits.
	opts.emit(Event{Stage: StageRecords, Total: len(refs)})
	seasonUIDs := make([]string, len(selected))
	for i, camp := range selected {
		seasonUIDs[i] = camp.SeasonUID
	}
	mapIDs := make([]string, 0, len(uids))
	for _, uid := range uids {
		if m, ok := maps[uid]; ok && m.MapID != "" {
			mapIDs = append(mapIDs, m.MapID)
		}
	}

	bySeason, seasonRaw, err := c.SeasonRecords(ctx, accountID, seasonUIDs, nil)
	if err != nil {
		// Not fatal on its own: fall through to the leaderboard rather than
		// abandoning a capture that can still be completed a slower way.
		opts.emit(Event{Stage: StageRecords, Detail: "season records unavailable, using leaderboard"})
		bySeason = nil
	}
	recordRaw := seasonRaw

	var bests map[string]Record
	if len(bySeason) == 0 {
		var fallbackRaw []json.RawMessage
		bests, fallbackRaw, err = c.PersonalBests(ctx, refs, func(done int) {
			opts.emit(Event{Stage: StageRecords, Done: done / 2, Total: len(refs)})
		})
		if err != nil {
			rec.LastError = err.Error()
			return rec, nil
		}
		recordRaw = append(recordRaw, fallbackRaw...)
	}
	opts.emit(Event{Stage: StageRecords, Done: len(refs) / 2, Total: len(refs)})

	// The all-time pass has no fallback: the Live leaderboard is season-scoped
	// by construction, so if this fails the archive simply keeps whatever
	// all-time bests it already had and the season figures stand in for
	// display. Better a slightly stale number than a wrong one.
	byAllTime, allTimeRaw, err := c.AllTimeRecords(ctx, accountID, mapIDs, func(done int) {
		opts.emit(Event{Stage: StageRecords, Done: len(refs)/2 + done/2, Total: len(refs)})
	})
	if err != nil {
		opts.emit(Event{Stage: StageRecords, Detail: "all-time records unavailable, keeping season scope"})
		byAllTime = nil
	}
	recordRaw = append(recordRaw, allTimeRaw...)

	rec.Raw.Records = recordRaw
	opts.emit(Event{Stage: StageRecords, Done: len(refs), Total: len(refs)})

	now := today()
	for _, camp := range selected {
		out := model.NadeoCampaign{
			SeasonUID:  camp.SeasonUID,
			CampaignID: camp.ID,
			Name:       camp.Name,
			Started:    unixDay(camp.StartTimestamp),
			Ended:      unixDay(camp.EndTimestamp),
		}
		for _, p := range camp.Playlist {
			track := model.NadeoTrack{
				Position:  p.Position,
				MapUID:    p.MapUID,
				FirstSeen: now,
			}
			if m, ok := maps[p.MapUID]; ok {
				track.MapID = m.MapID
				track.Name = m.Name
				track.AuthorID = m.Author
				track.ThumbnailURL = m.ThumbnailURL
				track.AuthorMs = m.AuthorScore
				track.GoldMs = m.GoldScore
				track.SilverMs = m.SilverScore
				track.BronzeMs = m.BronzeScore
			}
			// The record paths key differently — the Core endpoints by mapId,
			// the Live one by (mapUid, groupUid) — which is why map metadata is
			// fetched before records either way: mapId comes from there.
			if r, ok := bySeason[track.MapID]; ok && track.MapID != "" {
				track.SeasonBestMs = r.RecordScore.Time
				track.SeasonDrivenAt = recordDay(r.Timestamp)
			} else if r, ok := bests[recordKey(p.MapUID, camp.SeasonUID)]; ok {
				track.SeasonBestMs = r.Score
				track.WorldRank = r.WorldRank()
			}

			if r, ok := byAllTime[track.MapID]; ok && track.MapID != "" {
				track.PersonalBestMs = r.RecordScore.Time
				track.DrivenAt = recordDay(r.Timestamp)
				track.RecordID = r.MapRecordID
				track.ReplayURL = r.URL
				track.NadeoMedal = r.Medal
			} else {
				// No all-time figure this run. The season best is still a
				// personal best — just possibly not the latest one — so it
				// stands in rather than leaving the grid blank. The archive's
				// minimum-wins merge corrects it the moment a real all-time
				// value arrives.
				track.PersonalBestMs = track.SeasonBestMs
				track.DrivenAt = track.SeasonDrivenAt
			}
			out.Tracks = append(out.Tracks, track)
		}
		sort.SliceStable(out.Tracks, func(i, j int) bool { return out.Tracks[i].Position < out.Tracks[j].Position })
		rec.Campaigns = append(rec.Campaigns, out)
		opts.emit(Event{Stage: StageSeason, Detail: camp.Name, Done: 1})
	}

	rec.Fetched = now
	return rec, nil
}

// recordDay renders a record's RFC3339 timestamp as the calendar date it fell
// on in the site's timezone, matching every other date in the archive. An
// unparseable value yields "" rather than a wrong date.
func recordDay(ts string) string {
	if ts == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ""
	}
	return day(t)
}

// unixDay renders a campaign timestamp as YYYY-MM-DD, or "" for the zero/absent
// value the API uses for an unscheduled boundary.
func unixDay(ts int64) string {
	if ts <= 0 {
		return ""
	}
	return day(time.Unix(ts, 0))
}

// SeasonLabel is how a campaign is identified on the command line and in
// listings. Kept here so the command and the client can't drift on it.
func SeasonLabel(c Campaign) string {
	if c.Name != "" {
		return c.Name
	}
	return c.SeasonUID
}

// DescribeCampaign renders one campaign for `gamelog nadeo seasons`.
func DescribeCampaign(c Campaign) string {
	span := ""
	if s, e := unixDay(c.StartTimestamp), unixDay(c.EndTimestamp); s != "" {
		span = s
		if e != "" {
			span += " – " + e
		}
	}
	return strings.TrimSpace(fmt.Sprintf("%-16s %-24s %s tracks",
		SeasonLabel(c), span, strconv.Itoa(len(c.Playlist))))
}
