package server

import (
	"net/url"
	"slices"
	"testing"

	"go.dalton.dog/gamelog/internal/commands"
)

func TestNadeoOptionsFromForm(t *testing.T) {
	rows := []commands.NadeoSeason{{SeasonUID: "s-fall"}, {SeasonUID: "s-summer"}}
	tests := []struct {
		name        string
		form        url.Values
		wantErr     bool
		wantSeasons []string
		wantCurrent bool
		wantN       int
	}{
		{name: "nothing ticked", form: url.Values{}, wantErr: true},
		{name: "unknown season", form: url.Values{"season": {"s-made-up"}, "current": {"1"}}, wantErr: true},
		{name: "current only is the zero value", form: url.Values{"current": {"1"}}, wantN: 1},
		{name: "seasons without current", form: url.Values{"season": {"s-summer"}}, wantSeasons: []string{"s-summer"}, wantN: 1},
		{
			name:        "current plus seasons, duplicates dropped",
			form:        url.Values{"current": {"1"}, "season": {"s-fall", "s-summer", "s-fall"}},
			wantSeasons: []string{"s-fall", "s-summer"}, wantCurrent: true, wantN: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, n, err := nadeoOptionsFromForm(tt.form, rows)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want an error, got %+v", opts)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(opts.Seasons, tt.wantSeasons) || opts.Current != tt.wantCurrent || opts.All || n != tt.wantN {
				t.Errorf("got seasons=%v current=%v all=%v n=%d", opts.Seasons, opts.Current, opts.All, n)
			}
		})
	}
}
