package progress

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/moby/buildkit/client"
	digest "github.com/opencontainers/go-digest"
	"golang.org/x/sync/errgroup"
)

type Manager interface {
	Go(func() error)
	Release()
	Done() <-chan struct{}
	Wait() error

	NewJob(name string) Job

	Status() BuildStatus
	Jobs() <-chan Job
	Tasks() <-chan Task
	Current() int
	Total() int
	Elapsed() time.Duration
}

type BuildStatus int

const (
	BuildUnknown BuildStatus = iota
	BuildBuilding
	BuildFinished
	BuildFailed
	BuildCanceled
)

func (bs BuildStatus) String() string {
	switch bs {
	case BuildBuilding:
		return "Building"
	case BuildFinished:
		return "Finished"
	case BuildFailed:
		return "Failed"
	case BuildCanceled:
		return "Canceled"
	default:
		return "Unknown"
	}
}

type SolveStatus struct {
	*client.SolveStatus
	job Job
}

func NewManager(ctx context.Context) Manager {
	m := &manager{
		done:        make(chan struct{}),
		status:      BuildBuilding,
		statusCh:    make(chan SolveStatus),
		interrupt:   make(chan struct{}),
		errCh:       make(chan error),
		jobCh:       make(chan Job),
		taskCh:      make(chan Task),
		taskByID:    make(map[string]Task),
		vtxByDigest: make(map[digest.Digest]*client.Vertex),
	}

	go func() {
		m.errCh <- m.handleStatus()
	}()

	m.g, m.ctx = errgroup.WithContext(ctx)
	return m
}

type manager struct {
	ctx       context.Context
	g         *errgroup.Group
	interrupt chan struct{}
	done      chan struct{}

	errCh    chan error
	statusCh chan SolveStatus

	status BuildStatus

	jobCh    chan Job
	taskCh   chan Task
	taskByID map[string]Task

	localTimeDiff time.Duration
	startTime     *time.Time
	endTime       *time.Time
	vtxByDigest   map[digest.Digest]*client.Vertex
	vtxMu         sync.RWMutex
}

func (m *manager) Go(fn func() error) {
	m.g.Go(fn)
}

func (m *manager) Release() {
	log.Println("[manager] interrupted")
	close(m.interrupt)
	endTime := time.Now()
	m.endTime = &endTime
}

func (m *manager) Done() <-chan struct{} {
	return m.done
}

func (m *manager) Wait() error {
	defer close(m.done)

	err := m.g.Wait()
	close(m.statusCh)

	handleErr := <-m.errCh
	if err == nil {
		err = handleErr
	}
	if err != nil {
		if !strings.Contains(err.Error(), "context canceled") {
			m.status = BuildFailed
		} else {
			m.status = BuildCanceled
		}
		return err
	}

	m.status = BuildFinished
	return nil
}

func (m *manager) Status() BuildStatus {
	return m.status
}

func (m *manager) NewJob(name string) Job {
	job := m.newJob(name)

	log.Printf("[manager] added job %q\n", name)
	m.Go(func() error {
		select {
		case <-m.ctx.Done():
			log.Printf("[manager] job %q send canceled\n", name)
			return m.ctx.Err()
		case <-m.interrupt:
		case m.jobCh <- job:
		}
		return nil
	})

	return job
}

func (m *manager) Jobs() <-chan Job {
	return m.jobCh
}

func (m *manager) Tasks() <-chan Task {
	return m.taskCh
}

func (m *manager) Current() int {
	var sum int
	m.vtxMu.RLock()
	for _, vtx := range m.vtxByDigest {
		if vtx.Completed == nil || vtx.Error != "" {
			continue
		}
		sum += 1
	}
	m.vtxMu.RUnlock()
	return sum
}

func (m *manager) Total() int {
	min := len(m.taskByID)
	m.vtxMu.RLock()
	if len(m.vtxByDigest) > min {
		min = len(m.vtxByDigest)
	}
	m.vtxMu.RUnlock()
	return min
}

func (m *manager) Elapsed() time.Duration {
	endTime := time.Now()
	if m.endTime != nil {
		endTime = *m.endTime
	}
	if m.startTime != nil {
		return endTime.Sub(*m.startTime)
	}
	return 0
}

func (m *manager) handleStatus() error {
	for {
		solveStatus, ok := <-m.statusCh
		if !ok {
			return nil
		}

		_, err := json.MarshalIndent(solveStatus, "", "    ")
		if err != nil {
			return err
		}
		// log.Printf("[handler] received solve status:\n%s\n", string(dt))

		for _, vtx := range solveStatus.Vertexes {
			m.vtxMu.Lock()
			prev := m.vtxByDigest[vtx.Digest]
			m.vtxByDigest[vtx.Digest] = vtx
			m.vtxMu.Unlock()

			task, ok := m.taskByID[vtx.Digest.String()]
			if !ok {
				task = solveStatus.job.NewTask(vtx.Digest.String())
				m.taskByID[vtx.Digest.String()] = task
			}

			if vtx.Name != "" {
				task.SetDescription(vtx.Name)
			}

			// Handle vtx.Error
			// Handle vtx already completed

			if vtx.Started != nil && (prev == nil || prev.Started == nil) {
				err = task.Start(*vtx.Started)
				if err != nil {
					return err
				}
				if m.localTimeDiff == 0 {
					m.localTimeDiff = time.Since(*vtx.Started)
					startTime := vtx.Started.Add(m.localTimeDiff)
					m.startTime = &startTime
				}
			}
			if vtx.Completed != nil {
				if vtx.Error == "" {
					task.Complete(*vtx.Completed, nil)
				} else if strings.Contains(vtx.Error, context.Canceled.Error()) {
					task.Complete(*vtx.Completed, context.Canceled)
				} else {
					task.Complete(*vtx.Completed, fmt.Errorf(vtx.Error))
				}
			}
		}
		for _, status := range solveStatus.Statuses {
			_, ok := m.taskByID[status.Vertex.String()]
			if !ok {
				log.Printf("[handler] unregistered status %q\n", status.Vertex)
				continue
			}

			// Update task timestamp
			// Handle task total/current nonzero
			// Handle task complete
		}
		for _, l := range solveStatus.Logs {
			task, ok := m.taskByID[l.Vertex.String()]
			if !ok {
				log.Printf("[handler] unregistered log %q\n", l.Vertex)
				continue
			}

			// Do we need timestamp?
			// Do we need stream ID?

			_, err := task.Write(l.Data)
			if err != nil {
				return err
			}
		}
		log.Printf("[manager] Building (%d/%d)\n", m.Current(), m.Total())
	}
}
