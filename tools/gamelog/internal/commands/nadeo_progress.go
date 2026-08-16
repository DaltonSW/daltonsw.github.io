package commands

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"

	"go.dalton.dog/gamelog/internal/providers/nadeo"
)

// interruptContext cancels on SIGINT so a fetch stops between requests rather
// than being killed mid-write. Whatever was captured up to that point is still
// saved — the merge makes a partial capture additive, never destructive.
//
// In the TTY path the terminal is in raw mode and Ctrl-C arrives as a keypress
// rather than a signal, so the Bubble Tea model calls the same cancel; this
// covers the piped/non-interactive case.
func interruptContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt)
}

// nadeoProgress reports fetch progress. A backfill is deliberately slow
// (throttled at ~2 req/sec against a real player's account), so the command
// shows what it's doing rather than sitting on a blank line for half a minute.
//
// Two implementations behind one interface: a Bubble Tea view when stderr is a
// terminal, and one plain line per stage otherwise — which is what a piped or
// scheduled run wants, and what every other command in this tool prints.
type nadeoProgress struct {
	prog  *tea.Program
	plain *plainProgress
	done  sync.WaitGroup
}

func newNadeoProgress(title string, forcePlain bool, cancel context.CancelFunc) *nadeoProgress {
	if forcePlain || !isatty.IsTerminal(os.Stderr.Fd()) {
		return &nadeoProgress{plain: &plainProgress{}}
	}

	p := &nadeoProgress{}
	// Rendered to stderr so stdout stays a clean "Saved <path>" for piping.
	p.prog = tea.NewProgram(newProgressModel(title, cancel), tea.WithOutput(os.Stderr))
	p.done.Go(func() {
		// A UI failure must never fail the capture; the fetch is already
		// running and its result is what matters.
		_, _ = p.prog.Run()
	})
	return p
}

// Handle receives one progress event from the fetch. Safe to call from the
// fetch goroutine.
func (p *nadeoProgress) Handle(e nadeo.Event) {
	if p.prog != nil {
		p.prog.Send(e)
		return
	}
	p.plain.handle(e)
}

// Close tears the UI down and waits for the terminal to be restored, so the
// command's own output doesn't land in the middle of a repaint.
func (p *nadeoProgress) Close() {
	if p.prog != nil {
		p.prog.Quit()
		p.done.Wait()
	}
}

// ── Plain output ────────────────────────────────────────────────────────────

type plainProgress struct {
	lastStage nadeo.Stage
}

func (p *plainProgress) handle(e nadeo.Event) {
	// One line per stage transition, plus one per season, so an unattended log
	// still shows what happened without a line per batch.
	if e.Stage == nadeo.StageSeason {
		fmt.Fprintf(os.Stderr, "  %s\n", e.Detail)
		return
	}
	if e.Stage == p.lastStage {
		return
	}
	p.lastStage = e.Stage
	switch e.Stage {
	case nadeo.StageAuth:
		fmt.Fprintln(os.Stderr, "authenticating")
	case nadeo.StageCampaigns:
		fmt.Fprintln(os.Stderr, "enumerating campaigns")
	case nadeo.StageMaps:
		fmt.Fprintf(os.Stderr, "fetching map metadata (%d maps)\n", e.Total)
	case nadeo.StageRecords:
		fmt.Fprintf(os.Stderr, "fetching personal bests (%d records)\n", e.Total)
	}
}

// ── Bubble Tea view ─────────────────────────────────────────────────────────

var (
	styleTitle   = lipgloss.NewStyle().Bold(true)
	styleDim     = lipgloss.NewStyle().Faint(true)
	styleDone    = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	styleSeason  = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	styleWarning = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	styleBar     = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
)

const barWidth = 24

// renderBar draws a fixed-width meter. Deliberately hand-rolled: bubbles'
// progress component would drag in a whole animation-physics module for what is
// two runes and a ratio, and this tool keeps its dependency surface small on
// purpose (see internal/dotenv).
func renderBar(done, total int) string {
	if total <= 0 {
		return ""
	}
	filled := done * barWidth / total
	filled = max(0, min(filled, barWidth))
	return styleBar.Render(strings.Repeat("█", filled)) +
		styleDim.Render(strings.Repeat("░", barWidth-filled))
}

type stageState struct {
	label string
	done  int
	total int
	// started is what distinguishes "not reached yet" from "reached, nothing
	// to do" — a season with no maps left to fetch is finished, not pending.
	started bool
}

type progressModel struct {
	title   string
	cancel  context.CancelFunc
	spin    spinner.Model
	stage   nadeo.Stage
	stages  map[nadeo.Stage]*stageState
	seasons []string
	aborted bool
}

func newProgressModel(title string, cancel context.CancelFunc) progressModel {
	s := spinner.New()
	s.Spinner = spinner.Dot

	return progressModel{
		title:  title,
		cancel: cancel,
		spin:   s,
		stages: map[nadeo.Stage]*stageState{
			nadeo.StageCampaigns: {label: "Campaigns"},
			nadeo.StageMaps:      {label: "Map metadata"},
			nadeo.StageRecords:   {label: "Personal bests"},
		},
	}
}

func (m progressModel) Init() tea.Cmd { return m.spin.Tick }

func (m progressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Raw mode swallows the signal, so this is the only place Ctrl-C can be
		// turned back into a cancellation.
		if msg.Type == tea.KeyCtrlC {
			m.aborted = true
			if m.cancel != nil {
				m.cancel()
			}
			return m, nil
		}

	case nadeo.Event:
		if msg.Stage == nadeo.StageSeason {
			m.seasons = append(m.seasons, msg.Detail)
			return m, nil
		}
		m.stage = msg.Stage
		if st, ok := m.stages[msg.Stage]; ok {
			st.started = true
			st.done, st.total = msg.Done, msg.Total
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	}
	return m, nil
}

// stageOrder fixes the display order; the map above has no order of its own.
var stageOrder = []nadeo.Stage{nadeo.StageCampaigns, nadeo.StageMaps, nadeo.StageRecords}

func (m progressModel) View() string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n  %s\n\n", styleTitle.Render(m.title))

	for _, stage := range stageOrder {
		st := m.stages[stage]
		complete := st.started && (st.total == 0 || st.done >= st.total) && stage != m.stage

		marker := "  "
		switch {
		case complete:
			marker = styleDone.Render("✓ ")
		case stage == m.stage:
			marker = m.spin.View() + " "
		}

		fmt.Fprintf(&b, "  %s%-15s", marker, st.label)
		switch {
		case st.total > 0:
			fmt.Fprintf(&b, " %s %s", renderBar(st.done, st.total),
				styleDim.Render(fmt.Sprintf("%d/%d", st.done, st.total)))
		case st.started && stage == nadeo.StageCampaigns && st.done > 0:
			fmt.Fprint(&b, " "+styleDim.Render(fmt.Sprintf("%d seasons", st.done)))
		}
		b.WriteString("\n")
	}

	if len(m.seasons) > 0 {
		b.WriteString("\n")
		for _, s := range m.seasons {
			fmt.Fprintf(&b, "    %s %s\n", styleDone.Render("✓"), styleSeason.Render(s))
		}
	}

	if m.aborted {
		fmt.Fprintf(&b, "\n  %s\n", styleWarning.Render("cancelled — saving what was captured"))
	}
	b.WriteString("\n")
	return b.String()
}
