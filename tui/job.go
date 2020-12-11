package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/muesli/reflow/indent"
	"github.com/openllb/hlb/solver/progress"
)

type Job struct {
	t     *TUI
	job   progress.Job
	tasks []*Task
}

func (t *TUI) NewJob(job progress.Job) *Job {
	return &Job{
		t:   t,
		job: job,
	}
}

func (j *Job) Update(msg tea.Msg) tea.Cmd {
	return nil
}

func (j *Job) View() string {
	var view []string

	icon := ""
	switch j.job.Status() {
	case progress.TaskCreated, progress.TaskRunning:
		icon = j.t.color.Index(205, j.t.spinner.View()).String()
	case progress.TaskCanceled:
		icon = j.t.color.Yellow("✗ ").String()
	case progress.TaskErrored:
		icon = j.t.color.Red("✗ ").String()
	case progress.TaskCompleted:
		icon = j.t.color.Green("✔ ").String()
	}
	view = append(view, fmt.Sprintf("   %s%s %.1fs\n", icon, j.job.Name(), j.job.Elapsed().Seconds()))

	for i, t := range j.tasks {
		pfx := "├─"
		tview := t.View()
		if i == len(j.tasks)-1 {
			pfx = "└─"
		} else {
			lines := strings.Split(tview, "\n")
			for k, line := range lines {
				if k == 0 || k == len(lines)-1 {
					continue
				}
				lines[k] = fmt.Sprintf("│%s", line[1:])
			}
			tview = strings.Join(lines, "\n")
		}

		tview = fmt.Sprintf("%s %s", pfx, tview)
		tview = indent.String(tview, 5)
		view = append(view, tview)
	}

	return strings.Join(view, "")
}
