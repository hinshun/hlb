package main

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/creack/pty"
	"github.com/hinshun/vt10x"
	"github.com/moby/buildkit/client"
	_ "github.com/moby/buildkit/client/connhelper/dockercontainer"
	"github.com/moby/buildkit/client/llb"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
	digest "github.com/opencontainers/go-digest"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

func init() {
	logrus.SetReportCaller(true)
	logrus.SetFormatter(&fmtr{})
	logrus.SetLevel(logrus.DebugLevel)
}

func main() {
	fd, err := os.Create("logrus.log")
	if err != nil {
		panic(err)
	}
	defer fd.Close()

	logrus.SetOutput(fd)

	err = tui(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "err: %s", err)
		os.Exit(1)
	}
}

func tui(ctx context.Context) error {
	ptm, pts, err := pty.Open()
	if err != nil {
		return err
	}

	var state vt10x.State
	term := vt10x.New(&state, pts)

	rows, cols := state.Size()
	vt10x.ResizePty(pts, cols, rows)

	go func() {
		for {
			err := term.Parse()
			if err != nil {
				break
			}
		}
	}()

	ptm.Write([]byte("fetch http://dl-cdn.alpinelinux.org/alpine/v3.12/main/x86_64/APKINDEX.tar.gz\n"))
	ptm.Write([]byte("fetch http://dl-cdn.alpinelinux.org/alpine/v3.12/community/x86_64/APKINDEX.tar.gz\n"))
	ptm.Write([]byte("(1/4) Installing ca-certificates (20191127-r4)\n"))

	time.Sleep(time.Second)

	fmt.Println(state.String())
	return nil
}

type discardReadWriter struct {
	io.Reader
	io.Writer
}

func newDiscardReadWriter() io.ReadWriter {
	return &discardReadWriter{
		Reader: nil,
		Writer: ioutil.Discard,
	}
}

type ProgressModel struct {
	done        chan struct{}
	active      bool
	cursor      int
	vtxByDigest map[digest.Digest]*VertexModel
	vertices    []*VertexModel
}

func NewProgressModel() *ProgressModel {
	return &ProgressModel{
		done:        make(chan struct{}),
		cursor:      -1,
		vtxByDigest: make(map[digest.Digest]*VertexModel),
	}
}

func (p *ProgressModel) Finish() {
	if !p.active {
		close(p.done)
	}
}

func (p *ProgressModel) Init() tea.Cmd {
	return tick()
}

func (p *ProgressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Quit if the UI has been closed externally.
	select {
	case <-p.done:
		return p, tea.Quit
	default:
	}

	for _, vtx := range p.vertices {
		if vtx.mode != "focus" {
			continue
		}
		_, cmd := vtx.Update(msg)
		return p, cmd
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return p, tea.Quit
		case "j", "down":
			p.active = true
			p.cursor += 1
			if p.cursor >= len(p.vertices) {
				p.cursor = len(p.vertices) - 1
			}
			for i, vtx := range p.vertices {
				if i == p.cursor {
					vtx.Hover()
				} else {
					vtx.Blur()
				}
			}
			return p, nil
		case "k", "up":
			p.active = true
			p.cursor -= 1
			if p.cursor < 0 {
				p.cursor = 0
			}
			for i, vtx := range p.vertices {
				if i == p.cursor {
					vtx.Hover()
				} else {
					vtx.Blur()
				}
			}
			return p, nil
		case "enter":
			if p.active {
				for i, vtx := range p.vertices {
					if i == p.cursor {
						vtx.Focus()
					} else {
						vtx.Blur()
					}
				}
			}
		}
	case tickMsg:
		return p, tick()
	}
	return p, nil
}

func (p *ProgressModel) AddVertex(vtx *client.Vertex) {
	model := NewVertexModel(vtx)
	p.vertices = append(p.vertices, model)
	p.vtxByDigest[vtx.Digest] = model
	if !p.active {
		p.cursor = len(p.vertices)
	}
}

func (p *ProgressModel) AddVertexLog(dgst digest.Digest, l *client.VertexLog) {
	model, ok := p.vtxByDigest[dgst]
	if !ok {
		return
	}
	model.logs = append(model.logs, l)
	var content []string
	for _, l := range model.logs {
		content = append(content, string(l.Data))
	}
	model.vp.Height = 4
	model.vp.SetContent(strings.Join(content, ""))
	model.vp.GotoBottom()
}

func (p *ProgressModel) View() string {
	var out string
	for _, vtx := range p.vertices {
		out += vtx.View()
	}
	return out
}

type VertexModel struct {
	*client.Vertex
	logs []*client.VertexLog
	vp   *viewport.Model
	mode string
}

func NewVertexModel(vtx *client.Vertex) *VertexModel {
	return &VertexModel{
		Vertex: vtx,
		vp:     &viewport.Model{Width: 78, Height: 0},
	}
}

func (v *VertexModel) Focus() {
	v.mode = "focus"
}

func (v *VertexModel) Hover() {
	v.mode = "hover"
}

func (v *VertexModel) Blur() {
	v.mode = "blur"
}

func (v *VertexModel) Init() tea.Cmd {
	return nil
}

func (v *VertexModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		v.vp.Width = msg.Width
		return v, nil
	case tea.KeyMsg:
		if msg.String() == "q" {
			v.Blur()
			return v, nil
		}
		if v.mode == "focus" {
			vp, cmd := v.vp.Update(msg)
			v.vp = &vp
			return v, cmd
		}
	}
	return v, nil
}

func (v *VertexModel) View() string {
	var pfx string
	switch v.mode {
	case "hover":
		pfx = "=>"
	case "focus":
		pfx = "=x"
	default:
		pfx = "= "
	}
	return fmt.Sprintf("%s %s\n%s\n", pfx, v.Name, v.vp.View())
}

type tickMsg struct{}

func tick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
		return tickMsg{}
	})
}

func DisplaySolveStatus(ctx context.Context, p *ProgressModel, statusCh chan *client.SolveStatus) error {
	tracker := NewTracker()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case status, ok := <-statusCh:
			if !ok {
				p.Finish()
				return nil
			}
			err := tracker.Update(p, status)
			if err != nil {
				return err
			}
		}
	}
}

type Tracker struct {
	vtxByDigest map[digest.Digest]*client.Vertex
}

func NewTracker() *Tracker {
	return &Tracker{
		vtxByDigest: make(map[digest.Digest]*client.Vertex),
	}
}

func (t *Tracker) Update(p *ProgressModel, status *client.SolveStatus) error {
	for _, vtx := range status.Vertexes {
		_, ok := t.vtxByDigest[vtx.Digest]
		if !ok {
			t.vtxByDigest[vtx.Digest] = vtx
			p.AddVertex(vtx)
		}
	}
	for _, s := range status.Statuses {
		_, ok := t.vtxByDigest[s.Vertex]
		if !ok {
			return fmt.Errorf("received status before vertex %s", s.Vertex)
		}
		// Update status somewhere.
	}
	for _, l := range status.Logs {
		_, ok := t.vtxByDigest[l.Vertex]
		if !ok {
			return fmt.Errorf("received log before vertex %s", l.Vertex)
		}
		// Write log somewhere.
		p.AddVertexLog(l.Vertex, l)
	}
	return nil
}

func solve(ctx context.Context) error {
	cln, err := client.New(ctx, os.Getenv("BUILDKIT_HOST"), client.WithFailFast())
	if err != nil {
		return err
	}

	g, ctx := errgroup.WithContext(ctx)

	statusCh := make(chan *client.SolveStatus)

	p := NewProgressModel()

	g.Go(func() error {
		return tea.NewProgram(p).Start()
	})

	g.Go(func() error {
		return DisplaySolveStatus(context.Background(), p, statusCh)
	})

	g.Go(func() error {
		_, err = cln.Build(ctx, client.SolveOpt{}, "", func(ctx context.Context, c gateway.Client) (*gateway.Result, error) {
			st := llb.Image("alpine").Run(
				llb.Shlex("apk add -U git"),
				llb.IgnoreCache,
			).Root().Run(
				llb.Shlex("apk add -U curl openssh vim"),
				llb.IgnoreCache,
			).Root()

			def, err := st.Marshal(ctx)
			if err != nil {
				return nil, err
			}

			return c.Solve(ctx, gateway.SolveRequest{
				Definition: def.ToPB(),
			})
		}, statusCh)
		return err
	})

	return g.Wait()
}

type fmtr struct{}

func (f *fmtr) Format(e *logrus.Entry) ([]byte, error) {
	// fn := strings.TrimPrefix(e.Caller.File, prefix)
	return []byte(
		fmt.Sprintf(
			"%-8s[%s] %s: %s\n",
			strings.ToUpper(e.Level.String()),
			e.Time.Format("15:04:05.000"),
			e.Caller.File,
			e.Message,
		),
	), nil
}
