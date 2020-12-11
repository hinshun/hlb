package progress

import (
	"log"
	"sync"
	"time"

	"github.com/moby/buildkit/client"
)

type Job interface {
	Name() string

	NewChannel() chan *client.SolveStatus

	NewTask(id string) Task

	Depends(id string) bool

	Status() TaskStatus

	Elapsed() time.Duration
}

type job struct {
	m        *manager
	name     string
	taskByID map[string]*task
	taskMu   sync.RWMutex
}

func (m *manager) newJob(name string) *job {
	return &job{
		m:        m,
		name:     name,
		taskByID: make(map[string]*task),
	}
}

func (j *job) Name() string {
	return j.name
}

func (j *job) NewChannel() chan *client.SolveStatus {
	statusCh := make(chan *client.SolveStatus)

	log.Printf("[job %s] new channel", j.name)
	j.m.Go(func() error {
		for {
			select {
			case <-j.m.ctx.Done():
				log.Printf("[job %s] channel canceled", j.name)
				return j.m.ctx.Err()
			case s, ok := <-statusCh:
				if !ok {
					return nil
				}
				j.m.statusCh <- SolveStatus{
					SolveStatus: s,
					job:         j,
				}
			}
		}
	})

	return statusCh
}

func (j *job) NewTask(id string) Task {
	j.taskMu.Lock()
	defer j.taskMu.Unlock()

	log.Printf("[%s] added t %q\n", j.name, id)
	t, ok := j.m.taskByID[id]
	if !ok {
		t = j.m.newTask(id)
		j.m.taskByID[id] = t
		j.m.Go(func() error {
			select {
			case <-j.m.ctx.Done():
				log.Printf("[%s] t %q send canceled\n", j.name, id)
				return j.m.ctx.Err()
			case <-j.m.interrupt:
			case j.m.taskCh <- t:
			}
			return nil
		})
	}
	j.taskByID[id] = t.(*task)
	return t
}

func (j *job) Status() TaskStatus {
	j.taskMu.RLock()
	defer j.taskMu.RUnlock()

	if len(j.taskByID) == 0 {
		return TaskCreated
	}
	status := TaskUnknown
	for _, task := range j.taskByID {
		if task.Status() < status {
			status = task.Status()
		}
	}
	return status
}

func (j *job) Depends(id string) bool {
	j.taskMu.RLock()
	_, ok := j.taskByID[id]
	j.taskMu.RUnlock()
	return ok
}

func (j *job) Elapsed() time.Duration {
	j.taskMu.RLock()
	defer j.taskMu.RUnlock()

	now := time.Now()
	earliest := now
	latest := time.Time{}
	for _, task := range j.taskByID {
		if task.startTime != nil && (*task.startTime).Before(earliest) {
			earliest = *task.startTime
		}
		if task.endTime == nil {
			latest = now
		} else if (*task.endTime).After(latest) {
			latest = *task.endTime
		}
	}
	return latest.Sub(earliest)
}
