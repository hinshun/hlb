package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/logrusorgru/aurora"
	"github.com/muesli/reflow/indent"
	digest "github.com/opencontainers/go-digest"
	"github.com/openllb/hlb/solver/progress"
)

type Task struct {
	t     *TUI
	task  progress.Task
	pager *Pager
}

func (t *TUI) NewTask(task progress.Task) *Task {
	return &Task{
		t:     t,
		task:  task,
		pager: &Pager{},
	}
}

func (t *Task) Update(msg tea.Msg) tea.Cmd {
	return nil
}

func (t *Task) View() string {
	if !t.t.ready {
		return ""
	}

	width := t.t.width - 21
	view := t.task.Description()
	if len(view) == 0 {
		return ""
	}
	if len(view) > width {
		ellipsis := "..."
		view = view[:width-len(ellipsis)] + ellipsis
	}

	var (
		color   func(interface{}) aurora.Value
		content = "\n"
	)
	switch t.task.Status() {
	case progress.TaskCreated:
		color = t.t.color.Faint
	case progress.TaskRunning:
		color = t.t.color.Reset
		if t.task.HasLogs() {
			t.pager.Width = width
			t.pager.Height = 5
			t.pager.SetContent(t.task.String())
			t.pager.GotoBottom()
			content = indent.String(fmt.Sprintf("\n%s\n", t.pager.View()), 3)
		}
	case progress.TaskCanceled:
		color = t.t.color.Yellow
		view = fmt.Sprintf("[CANCELED] %s", view)
	case progress.TaskErrored:
		color = t.t.color.Red
		view = fmt.Sprintf("[ERROR] %s", view)
	case progress.TaskCompleted:
		color = t.t.color.Blue
	}

	id := digest.Digest(t.task.ID())
	return fmt.Sprintf("%s %.1fs %s%s",
		color(fmt.Sprintf("%s", view)),
		t.task.Elapsed().Seconds(),
		t.t.color.Faint(id.Encoded()[:6]),
		t.t.color.Faint(content),
	)
}
