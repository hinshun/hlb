package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/logrusorgru/aurora"
	"github.com/muesli/reflow/padding"
	"github.com/openllb/hlb/diagnostic"
	"github.com/openllb/hlb/solver/progress"
)

type TUI struct {
	pm      progress.Manager
	cancel  context.CancelFunc
	color   aurora.Aurora
	spinner spinner.Model

	ready  bool
	width  int
	height int

	viewMu sync.RWMutex
	jobs   []*Job
	tasks  []*Task
}

func New(ctx context.Context, cancel context.CancelFunc, pm progress.Manager) (*TUI, error) {
	t := &TUI{
		pm:      pm,
		cancel:  cancel,
		color:   diagnostic.Color(ctx),
		spinner: spinner.NewModel(),
	}
	t.spinner.Spinner = spinner.Dot

	select {
	case <-ctx.Done():
		return t, ctx.Err()
	case j := <-t.pm.Jobs():
		jmodel := t.NewJob(j)
		t.jobs = append(t.jobs, jmodel)
	}

	go func() {
		for job := range t.pm.Jobs() {
			jmodel := t.NewJob(job)
			t.viewMu.Lock()
			t.jobs = append(t.jobs, jmodel)
			t.viewMu.Unlock()
		}
	}()

	go func() {
		for task := range t.pm.Tasks() {
			t.viewMu.Lock()
			tmodel := t.NewTask(task)
			for _, j := range t.jobs {
				if !j.job.Depends(task.ID()) {
					continue
				}
				j.tasks = append(j.tasks, tmodel)
			}
			t.tasks = append(t.tasks, tmodel)
			t.viewMu.Unlock()
		}
	}()

	return t, nil
}

func (t *TUI) Init() tea.Cmd {
	return tea.Batch(
		spinner.Tick,
		t.done(),
	)
}

func (t *TUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m, ok := msg.(tea.KeyMsg); ok && m.String() == "ctrl+c" {
		t.cancel()
		return t, nil
	}

	var cmds []tea.Cmd
	for _, j := range t.jobs {
		cmd := j.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if len(cmds) > 0 {
		return t, tea.Batch(cmds...)
	}

	switch m := msg.(type) {
	case doneMsg:
		return t, tea.Quit
	case tea.WindowSizeMsg:
		t.width = m.Width
		t.height = m.Height
		t.ready = true
	case tea.KeyMsg:
		switch m.String() {
		case "q":
			return t, tea.Quit
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		t.spinner, cmd = t.spinner.Update(msg)
		return t, cmd
	}

	return t, nil
}

func (t *TUI) View() string {
	if !t.ready {
		return ""
	}

	view := []string{"\n"}

	completionStart := false
	t.viewMu.RLock()
	for i := len(t.jobs) - 1; i >= 0; i-- {
		if len(t.jobs[i].tasks) == 0 {
			continue
		}
		for _, t := range t.jobs[i].tasks {
			if t.task.Status() == progress.TaskCompleted {
				completionStart = true
				break
			}
		}
		view = append(view, t.jobs[i].View())
	}
	t.viewMu.RUnlock()

	if t.pm.Status() < progress.BuildFinished {
		view = append(view, t.color.Faint("\n   i: interact • ctrl+c: cancel\n").String())
	}

	var completion int
	if completionStart && t.pm.Total() > 0 {
		completion = t.pm.Current() * 100.0 / t.pm.Total()
	}

	view = append(view, fmt.Sprintf("\n   %s", padding.String(t.pm.Status().String(), 9)))
	view = append(view, padding.String(fmt.Sprintf("%d%%", completion), 5))

	bar := make([]rune, 10)
	for i := 0; i < 10; i++ {
		if i < completion/10 {
			bar[i] = '█'
		} else {
			bar[i] = ' '
		}
	}
	view = append(view, fmt.Sprintf("│%s│ %.1fs", string(bar), t.pm.Elapsed().Seconds()))

	return fmt.Sprintf("%s\n", strings.Join(view, ""))
}

type doneMsg struct{}

func (t *TUI) done() tea.Cmd {
	return func() tea.Msg {
		<-t.pm.Done()
		return doneMsg{}
	}
}
