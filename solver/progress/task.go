package progress

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"time"
)

type Task interface {
	io.Writer
	fmt.Stringer

	ID() string

	Status() TaskStatus

	Description() string

	SetDescription(description string)

	HasLogs() bool

	Start(startTime time.Time) error

	Complete(endTime time.Time, err error)

	Elapsed() time.Duration
}

type TaskStatus int

const (
	TaskCreated TaskStatus = iota
	TaskRunning
	TaskCached
	TaskErrored
	TaskCanceled
	TaskCompleted
	TaskUnknown
)

func (ts TaskStatus) String() string {
	switch ts {
	case TaskCreated:
		return "created"
	case TaskRunning:
		return "running"
	case TaskCached:
		return "cached"
	case TaskErrored:
		return "errored"
	case TaskCanceled:
		return "canceled"
	case TaskCompleted:
		return "completed"
	default:
		return "unknown"
	}
}

type task struct {
	m           *manager
	buf         *bytes.Buffer
	id          string
	name        string
	description string
	hasLogs     bool
	status      TaskStatus
	startTime   *time.Time
	endTime     *time.Time
}

func (m *manager) newTask(id string) *task {
	t := &task{
		m:      m,
		id:     id,
		status: TaskCreated,
		buf:    new(bytes.Buffer),
	}
	return t
}

func (t *task) Write(p []byte) (n int, err error) {
	t.hasLogs = true
	return t.buf.Write(p)
}

func (t *task) String() string {
	return t.buf.String()
}

func (t *task) ID() string {
	return t.id
}

func (t *task) Description() string {
	return t.description
}

func (t *task) SetDescription(description string) {
	t.description = description
	log.Printf("[task] set description %q", description)
}

func (t *task) HasLogs() bool {
	return t.hasLogs
}

func (t *task) Start(startTime time.Time) error {
	if t.status != TaskCreated {
		return fmt.Errorf("task already at status: %s", t.status)
	}
	t.status = TaskRunning
	startTime = startTime.Add(t.m.localTimeDiff)
	t.startTime = &startTime
	return nil
}

func (t *task) Complete(endTime time.Time, err error) {
	endTime = endTime.Add(t.m.localTimeDiff)
	t.endTime = &endTime
	if err != nil {
		if errors.Is(err, context.Canceled) {
			t.status = TaskCanceled
			return
		}
		t.status = TaskErrored
		return
	}
	t.status = TaskCompleted
}

func (t *task) Status() TaskStatus {
	return t.status
}

func (t *task) Elapsed() time.Duration {
	if t.startTime == nil {
		return 0
	}
	endTime := time.Now()
	if t.endTime != nil {
		endTime = *t.endTime
	}
	return endTime.Sub(*t.startTime)
}
