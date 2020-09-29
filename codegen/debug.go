package codegen

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"unicode"

	"github.com/docker/buildx/util/progress"
	shellquote "github.com/kballard/go-shellquote"
	"github.com/logrusorgru/aurora"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/solver/pb"
	digest "github.com/opencontainers/go-digest"
	"github.com/openllb/hlb/parser"
	"github.com/openllb/hlb/pkg/llbutil"
	"github.com/openllb/hlb/report"
	"github.com/openllb/hlb/solver"
	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh/terminal"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sys/unix"
)

var (
	ErrDebugExit = errors.Errorf("exiting debugger")
)

type Debugger func(ctx context.Context, scope *parser.Scope, node parser.Node, ret Value) error

func NewNoopDebugger() Debugger {
	return func(ctx context.Context, _ *parser.Scope, _ parser.Node, _ Value) error {
		return nil
	}
}

type snapshot struct {
	scope *parser.Scope
	node  parser.Node
	ret   Value
}

func NewDebugger(cln *client.Client, w io.Writer, r *bufio.Reader, fbs map[string]*parser.FileBuffer) Debugger {
	color := aurora.NewAurora(true)

	var (
		mod               *parser.Module
		fun               *parser.FuncDecl
		next              *parser.FuncDecl
		history           []*snapshot
		historyIndex      = -1
		reverseStep       bool
		cont              bool
		staticBreakpoints []*Breakpoint
		breakpoints       []*Breakpoint
	)

	return func(ctx context.Context, scope *parser.Scope, node parser.Node, ret Value) error {
		// Store a snapshot of the current debug step so we can backtrack.
		historyIndex++
		history = append(history, &snapshot{scope, node, ret})

		debug := func(s *snapshot) error {
			showList := true

			// Keep track of whether we're in global scope or a lexical scope.
			switch n := s.scope.Node.(type) {
			case *parser.Module:
				// Don't print source code on the first debug section.
				showList = false
				mod = n
				if len(staticBreakpoints) == 0 {
					staticBreakpoints = findStaticBreakpoints(mod)
					breakpoints = append(breakpoints, staticBreakpoints...)
				}
			case *parser.FuncDecl:
				fun = n
			}

			switch n := s.node.(type) {
			case *parser.FuncDecl:
				for _, bp := range breakpoints {
					if bp.Call != nil {
						continue
					}
					if bp.Func == n {
						cont = false
					}
				}
			case *parser.CallStmt:
				for _, bp := range breakpoints {
					if bp.Call == nil {
						continue
					}
					if bp.Call == n {
						cont = false
					}
				}
			}

			if showList && !cont {
				err := printList(color, fbs, w, s.node)
				if err != nil {
					return err
				}
			}

			if next != nil {
				// If nment is not in the same function scope, skip over it.
				if next != fun {
					return nil
				}
				next = nil
			}

			// Continue until we find a breakpoint or end of program.
			if cont {
				return nil
			}

			for {
				fmt.Fprint(w, "(hlb) ")

				command, err := r.ReadString('\n')
				if err != nil {
					return err
				}

				command = strings.Replace(command, "\n", "", -1)
				if command == "" {
					continue
				}

				args, err := shellquote.Split(command)
				if err != nil {
					return err
				}

				switch args[0] {
				case "break", "b":
					var bp *Breakpoint

					if len(args) == 1 {
						switch n := s.node.(type) {
						case *parser.FuncDecl:
							bp = &Breakpoint{
								Func: n,
							}
						case *parser.CallStmt:
							if n.Name.Ident.Text == "breakpoint" {
								fmt.Fprintf(w, "%s cannot break at breakpoint\n", parser.FormatPos(n.Pos))
								continue
							}

							bp = &Breakpoint{
								Func: fun,
								Call: n,
							}
						}
					} else {
						fmt.Fprintf(w, "unimplemented")
						continue
					}
					breakpoints = append(breakpoints, bp)
				case "breakpoints":
					for i, bp := range breakpoints {
						pos := bp.Func.Pos
						if bp.Call != nil {
							pos = bp.Call.Pos
						}

						msg := fmt.Sprintf("Breakpoint %d for %s%s %s",
							i,
							bp.Func.Name,
							bp.Func.Params,
							parser.FormatPos(pos))

						if bp.Call != nil {
							bp.Call.Terminate = nil
							msg = fmt.Sprintf("%s %s", msg, bp.Call)
						}

						fmt.Fprintf(w, "%s\n", msg)
					}
				case "clear":
					if len(args) == 0 {
						breakpoints = append([]*Breakpoint{}, staticBreakpoints...)
					} else {
						fmt.Fprintf(w, "unimplemented\n")
						continue
					}
				case "continue", "c":
					cont = true
					return nil
				case "dir":
					fs, err := s.ret.Filesystem()
					if err != nil {
						fmt.Fprintf(w, "current step is not in a fs scope\n")
						continue
					}

					dir, err := fs.State.GetDir(ctx)
					if err != nil {
						fmt.Fprintf(w, "err: %s\n", err)
						continue
					}

					fmt.Fprintf(w, "Working directory %q\n", dir)
				case "dot":
					fs, err := s.ret.Filesystem()
					if err != nil {
						fmt.Fprintf(w, "current step is not in a fs scope\n")
						continue
					}

					var sh string
					if len(args) == 2 {
						sh = args[1]
					}

					err = printGraph(ctx, fs.State, sh)
					if err != nil {
						fmt.Fprintf(w, "err: %s\n", err)
					}
					continue
				case "env":
					fs, err := s.ret.Filesystem()
					if err != nil {
						fmt.Fprintf(w, "current step is not in a fs scope\n")
						continue
					}

					env, err := fs.State.Env(ctx)
					if err != nil {
						fmt.Fprintf(w, "err: %s\n", err)
						continue
					}

					fmt.Fprintf(w, "Environment %s\n", env)
				case "exec":
					fs, err := s.ret.Filesystem()
					if err != nil {
						fmt.Fprintf(w, "current step is not in a fs scope\n")
						continue
					}

					err = Exec(ctx, cln, fs, r, w, args[1:]...)
					if err != nil {
						fmt.Fprintf(w, "err: %s\n", err)
						continue
					}
				case "exit":
					return ErrDebugExit
				case "funcs":
					for _, obj := range s.scope.Defined(parser.DeclKind) {
						switch n := obj.Node.(type) {
						case *parser.FuncDecl:
							fmt.Fprintf(w, "%s\n", n.Name)
						case *parser.BindClause:
							fmt.Fprintf(w, "%s\n", obj.Ident)
						}
					}
				case "help":
					fmt.Fprintf(w, "# Inspect\n")
					fmt.Fprintf(w, "help - shows this help message\n")
					fmt.Fprintf(w, "list - show source code\n")
					fmt.Fprintf(w, "print - print evaluate an expression\n")
					fmt.Fprintf(w, "funcs - print list of functions\n")
					fmt.Fprintf(w, "locals - print local variables\n")
					fmt.Fprintf(w, "types - print list of types\n")
					fmt.Fprintf(w, "whatis - print type of an expression\n")
					fmt.Fprintf(w, "# Movement\n")
					fmt.Fprintf(w, "exit - exit the debugger\n")
					fmt.Fprintf(w, "break [ <symbol> | <linespec> ] - sets a breakpoint\n")
					fmt.Fprintf(w, "breakpoints - print out info for active breakpoints\n")
					fmt.Fprintf(w, "clear [ <breakpoint-index> ] - deletes breakpoint\n")
					fmt.Fprintf(w, "continue - run until breakpoint or program termination\n")
					fmt.Fprintf(w, "next - step over to next source line\n")
					fmt.Fprintf(w, "step - single step through program\n")
					fmt.Fprintf(w, "stepout - step out of current function\n")
					fmt.Fprintf(w, "reverse-step - single step backwards through program\n")
					fmt.Fprintf(w, "restart - restart program from the start\n")
					fmt.Fprintf(w, "# Filesystem\n")
					fmt.Fprintf(w, "dir - print working directory\n")
					fmt.Fprintf(w, "env - print environment\n")
					fmt.Fprintf(w, "network - print network mode\n")
					fmt.Fprintf(w, "security - print security mode\n")
				case "list", "l":
					if showList {
						err = printList(color, fbs, w, s.node)
						if err != nil {
							return err
						}
					} else {
						fmt.Fprintf(w, "Program has not started yet\n")
					}
				case "locals":
					if fun != nil {
						for _, arg := range fun.Params.Fields() {
							obj := s.scope.Lookup(arg.Name.Text)
							if obj == nil {
								fmt.Fprintf(w, "err: %s\n", errors.WithStack(ErrCodeGen{arg, ErrUndefinedReference}))
								continue
							}
							fmt.Fprintf(w, "%s %s = %#v\n", arg.Type, arg.Name, obj.Data)
						}
					}
				case "next", "n":
					next = fun
					return nil
				case "network":
					fs, err := s.ret.Filesystem()
					if err != nil {
						fmt.Fprintf(w, "current step is not in a fs scope\n")
						continue
					}

					network, err := fs.State.GetNetwork(ctx)
					if err != nil {
						fmt.Fprintf(w, "err: %s\n", err)
						continue
					}

					fmt.Fprintf(w, "Network %s\n", network)
				case "print":
					fmt.Fprintf(w, "print\n")
				case "restart", "r":
					reverseStep = true
					historyIndex = 1
					return nil
				case "reverse-step", "rs":
					if historyIndex == 0 {
						fmt.Fprintf(w, "Already at the start of the program\n")
					} else {
						reverseStep = true
						return nil
					}
				case "security":
					fs, err := s.ret.Filesystem()
					if err != nil {
						fmt.Fprintf(w, "current step is not in a fs scope\n")
						continue
					}

					security, err := fs.State.GetSecurity(ctx)
					if err != nil {
						fmt.Fprintf(w, "err: %s\n", err)
						continue
					}

					fmt.Fprintf(w, "Security %s\n", security)
				case "step", "s":
					return nil
				case "stepout":
					fmt.Fprintf(w, "unimplemented\n")
				case "types":
					for _, kind := range report.Kinds {
						fmt.Fprintf(w, "%s\n", kind)
					}
				case "whatis":
					fmt.Fprintf(w, "unimplemented\n")
				default:
					fmt.Fprintf(w, "unrecognized command %s\n", command)
				}
			}
		}

		err := debug(history[historyIndex])
		if err != nil {
			return err
		}

		if reverseStep {
			historyIndex--
			reverseStep = false

			for historyIndex < len(history) {
				err = debug(history[historyIndex])
				if err != nil {
					return err
				}

				if reverseStep {
					historyIndex--
					reverseStep = false
				} else {
					historyIndex++
				}
			}

			historyIndex--
		}

		return nil
	}
}

func printList(color aurora.Aurora, fbs map[string]*parser.FileBuffer, w io.Writer, node parser.Node) error {
	pos := node.Position()
	fb := fbs[pos.Filename]

	var lines []string

	start := pos.Line - 5
	if start < 0 {
		start = 0
	}

	end := start + 10
	if end > fb.Len() {
		end = fb.Len()
	}

	length := 1
	switch n := node.(type) {
	case *parser.FuncDecl:
		length = n.Name.End().Column - n.Pos.Column
	case *parser.CallStmt:
		length = n.Name.End().Column - n.Pos.Column
	}

	maxLn := len(fmt.Sprintf("%d", end))
	gutter := strings.Repeat(" ", maxLn)
	header := fmt.Sprintf(
		"%s %s",
		color.Sprintf(color.Blue("%s-->"), gutter),
		color.Sprintf(color.Bold("%s:%d:%d:"), pos.Filename, pos.Line, pos.Column))
	lines = append(lines, header)

	for i := start; i < end; i++ {
		line, err := fb.Line(i)
		if err != nil {
			return err
		}

		ln := fmt.Sprintf("%d", i+1)
		prefix := color.Sprintf(color.Blue("%s%s | "), ln, strings.Repeat(" ", maxLn-len(ln)))
		lines = append(lines, fmt.Sprintf("%s%s", prefix, string(line)))

		if i == pos.Line-1 {
			prefix = color.Sprintf(color.Blue("%s ⫶ "), gutter)
			padding := bytes.Map(func(r rune) rune {
				if unicode.IsSpace(r) {
					return r
				}
				return ' '
			}, line[:pos.Column-1])

			lines = append(lines, fmt.Sprintf(
				"%s%s",
				prefix,
				color.Sprintf(color.Green("%s%s"), padding, strings.Repeat("^", length)),
			))
		}
	}

	fmt.Fprintln(w, strings.Join(lines, "\n"))
	return nil
}

type Breakpoint struct {
	Func *parser.FuncDecl
	Call *parser.CallStmt
}

func findStaticBreakpoints(mod *parser.Module) []*Breakpoint {
	var breakpoints []*Breakpoint

	parser.Match(mod, parser.MatchOpts{},
		func(fun *parser.FuncDecl, call *parser.CallStmt) {
			if !call.Breakpoint() {
				return
			}
			bp := &Breakpoint{
				Func: fun,
				Call: call,
			}
			breakpoints = append(breakpoints, bp)
		},
	)

	return breakpoints
}

func printGraph(ctx context.Context, st llb.State, sh string) error {
	def, err := st.Marshal(ctx, llb.LinuxAmd64)
	if err != nil {
		return err
	}

	ops, err := loadLLB(def)
	if err != nil {
		return err
	}

	r, w := io.Pipe()
	defer r.Close()

	go func() {
		defer w.Close()
		writeDot(ops, w)
	}()

	if sh == "" {
		_, err = io.Copy(os.Stderr, r)
		return err
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", sh)
	cmd.Stdin = r
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

type llbOp struct {
	Op         pb.Op
	Digest     digest.Digest
	OpMetadata pb.OpMetadata
}

func loadLLB(def *llb.Definition) ([]llbOp, error) {
	var ops []llbOp
	for _, dt := range def.Def {
		var op pb.Op
		if err := (&op).Unmarshal(dt); err != nil {
			return nil, err
		}
		dgst := digest.FromBytes(dt)
		ent := llbOp{Op: op, Digest: dgst, OpMetadata: def.Metadata[dgst]}
		ops = append(ops, ent)
	}
	return ops, nil
}

func writeDot(ops []llbOp, w io.Writer) {
	fmt.Fprintln(w, "digraph {")
	defer fmt.Fprintln(w, "}")
	for _, op := range ops {
		name, shape := attr(op.Digest, op.Op)
		fmt.Fprintf(w, "  %q [label=%q shape=%q];\n", op.Digest, name, shape)
	}
	for _, op := range ops {
		for i, inp := range op.Op.Inputs {
			label := ""
			if eo, ok := op.Op.Op.(*pb.Op_Exec); ok {
				for _, m := range eo.Exec.Mounts {
					if int(m.Input) == i && m.Dest != "/" {
						label = m.Dest
					}
				}
			}
			fmt.Fprintf(w, "  %q -> %q [label=%q];\n", inp.Digest, op.Digest, label)
		}
	}
}

func attr(dgst digest.Digest, op pb.Op) (string, string) {
	switch op := op.Op.(type) {
	case *pb.Op_Source:
		return op.Source.Identifier, "ellipse"
	case *pb.Op_Exec:
		return strings.Join(op.Exec.Meta.Args, " "), "box"
	case *pb.Op_Build:
		return "build", "box3d"
	case *pb.Op_File:
		names := []string{}

		for _, action := range op.File.Actions {
			var name string

			switch act := action.Action.(type) {
			case *pb.FileAction_Copy:
				name = fmt.Sprintf("copy{src=%s, dest=%s}", act.Copy.Src, act.Copy.Dest)
			case *pb.FileAction_Mkfile:
				name = fmt.Sprintf("mkfile{path=%s}", act.Mkfile.Path)
			case *pb.FileAction_Mkdir:
				name = fmt.Sprintf("mkdir{path=%s}", act.Mkdir.Path)
			case *pb.FileAction_Rm:
				name = fmt.Sprintf("rm{path=%s}", act.Rm.Path)
			}

			names = append(names, name)
		}
		return strings.Join(names, ","), "note"
	default:
		return dgst.String(), "plaintext"
	}
}

func Exec(ctx context.Context, cln *client.Client, fs Filesystem, r *bufio.Reader, w io.Writer, args ...string) error {
	if len(args) == 0 {
		args = []string{"/bin/sh"}
	}

	s, err := llbutil.NewSession(ctx, fs.SessionOpts...)
	if err != nil {
		return err
	}

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return s.Run(ctx, cln.Dialer())
	})

	g.Go(func() error {
		var pw progress.Writer

		mw := MultiWriter(ctx)
		if mw != nil {
			pw = mw.WithPrefix("", false)
		}

		return solver.Build(ctx, cln, s, pw, func(ctx context.Context, c gateway.Client) (res *gateway.Result, err error) {
			def, err := fs.State.Marshal(ctx)
			if err != nil {
				return
			}

			res, err = c.Solve(ctx, gateway.SolveRequest{
				Definition: def.ToPB(),
			})
			if err != nil {
				return
			}

			ctr, err := c.NewContainer(ctx, gateway.NewContainerRequest{
				Mounts: []gateway.Mount{{
					Dest:      "/",
					MountType: pb.MountType_BIND,
					Ref:       res.Ref,
				}},
			})
			if err != nil {
				return
			}
			defer ctr.Release(ctx)

			ir := InterruptibleReader(r)
			defer func() {
				ir.Close()
			}()

			proc, err := ctr.Start(ctx, gateway.StartRequest{
				Args:   args,
				Cwd:    "/",
				Tty:    true,
				Stdin:  ir,
				Stdout: NopWriteCloser(w),
			})
			if err != nil {
				return
			}

			oldState, err := terminal.MakeRaw(int(os.Stdin.Fd()))
			if err != nil {
				return nil, err
			}
			defer terminal.Restore(int(os.Stdin.Fd()), oldState)

			ch := make(chan os.Signal, 1)
			ch <- syscall.SIGWINCH // Initial resize.

			go forwardResize(ctx, ch, proc, int(os.Stdin.Fd()))

			signal.Notify(ch, syscall.SIGWINCH)
			defer signal.Stop(ch)

			return res, proc.Wait()
		}, fs.SolveOpts...)
	})

	err = g.Wait()
	if err != nil {
		return err
	}

	return nil
}

func forwardResize(ctx context.Context, ch chan os.Signal, proc gateway.ContainerProcess, fd int) {
	for {
		select {
		case <-ctx.Done():
			close(ch)
			return
		case <-ch:
			ws, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
			if err != nil {
				return
			}

			err = proc.Resize(ctx, gateway.WinSize{
				Cols: uint32(ws.Col),
				Rows: uint32(ws.Row),
			})
			if err != nil {
				return
			}
		}
	}
}

func InterruptibleReader(r io.Reader) io.ReadCloser {
	return &interruptibleReader{
		Reader: r,
		done:   make(chan struct{}),
	}
}

type interruptibleReader struct {
	io.Reader
	done chan struct{}
}

func (r *interruptibleReader) Read(p []byte) (n int, err error) {
	select {
	case <-r.done:
		return 0, nil
	default:
		n, err := r.Reader.Read(p)
		return n, err
	}
}

func (r *interruptibleReader) Close() error {
	close(r.done)
	return nil
}

func NopWriteCloser(w io.Writer) io.WriteCloser {
	return &nopWriteCloser{w}
}

type nopWriteCloser struct {
	io.Writer
}

func (w *nopWriteCloser) Close() error {
	return nil
}
