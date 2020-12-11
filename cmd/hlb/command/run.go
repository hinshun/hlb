package command

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/moby/buildkit/client"
	"github.com/openllb/hlb"
	"github.com/openllb/hlb/builtin"
	"github.com/openllb/hlb/codegen"
	"github.com/openllb/hlb/diagnostic"
	"github.com/openllb/hlb/errdefs"
	"github.com/openllb/hlb/local"
	"github.com/openllb/hlb/parser"
	"github.com/openllb/hlb/solver/progress"
	"github.com/openllb/hlb/tui"
	cli "github.com/urfave/cli/v2"
)

var (
	DefaultHLBFilename = "build.hlb"
)

var runCommand = &cli.Command{
	Name:      "run",
	Usage:     "compiles and runs a hlb program",
	ArgsUsage: "<*.hlb>",
	Flags: []cli.Flag{
		&cli.StringSliceFlag{
			Name:    "target",
			Aliases: []string{"t"},
			Usage:   "specify target filesystem to solve",
			Value:   cli.NewStringSlice("default"),
		},
		&cli.BoolFlag{
			Name:  "debug",
			Usage: "jump into a source level debugger for hlb",
		},
		&cli.BoolFlag{
			Name:  "tree",
			Usage: "print out the request tree without solving",
		},
		&cli.StringFlag{
			Name:  "log-output",
			Usage: "set type of log output (auto, tty, plain)",
			Value: "auto",
		},
		&cli.BoolFlag{
			Name:    "backtrace",
			Usage:   "print out the backtrace when encountering an error",
			EnvVars: []string{"HLB_BACKTRACE"},
		},
	},
	Action: func(c *cli.Context) error {
		rc, err := ModuleReadCloser(c.Args().Slice())
		if err != nil {
			return err
		}
		defer rc.Close()

		cln, ctx, err := Client(c)
		if err != nil {
			return err
		}

		// TODO: remove debugging block
		f, err := os.Create("/tmp/hlb.log")
		if err != nil {
			return err
		}
		defer f.Close()
		log.SetOutput(f)
		// TODO: remove debugging block

		return Run(ctx, cln, rc, RunInfo{
			Debug:     c.Bool("debug"),
			Tree:      c.Bool("tree"),
			Targets:   c.StringSlice("target"),
			LLB:       c.Bool("llb"),
			Backtrace: c.Bool("backtrace"),
			LogOutput: c.String("log-output"),
			ErrOutput: os.Stderr,
			Output:    os.Stdout,
		})
	},
}

type RunInfo struct {
	Debug     bool
	Tree      bool
	Backtrace bool
	Targets   []string
	LLB       bool
	LogOutput string
	ErrOutput io.Writer
	Output    io.Writer

	// override defaults sources as necessary
	Environ []string
	Cwd     string
	Os      string
	Arch    string
}

func Run(ctx context.Context, cln *client.Client, rc io.ReadCloser, info RunInfo) (err error) {
	if len(info.Targets) == 0 {
		info.Targets = []string{"default"}
	}
	if info.Output == nil {
		info.Output = os.Stdout
	}

	ctx = local.WithEnviron(ctx, info.Environ)
	ctx, err = local.WithCwd(ctx, info.Cwd)
	if err != nil {
		return err
	}
	ctx = local.WithOs(ctx, info.Os)
	ctx = local.WithArch(ctx, info.Arch)

	// var progressOpts []solver.ProgressOption
	// if info.LogOutput == "" || info.LogOutput == "auto" {
	// 	// assume plain output, will upgrade if we detect tty
	// 	info.LogOutput = "plain"
	// 	if fdAble, ok := info.Output.(interface{ Fd() uintptr }); ok {
	// 		if isatty.IsTerminal(fdAble.Fd()) {
	// 			info.LogOutput = "tty"
	// 		}
	// 	}
	// }

	// switch info.LogOutput {
	// case "tty":
	// 	progressOpts = append(progressOpts, solver.WithLogOutput(solver.LogOutputTTY))
	// case "plain":
	// 	progressOpts = append(progressOpts, solver.WithLogOutput(solver.LogOutputPlain))
	// default:
	// 	return fmt.Errorf("unrecognized log-output %q", info.LogOutput)
	// }

	pm := progress.NewManager(ctx)
	ctx = progress.WithManager(ctx, pm)
	ctx = diagnostic.WithSources(ctx, builtin.Sources())

	defer func() {
		if err == nil {
			return
		}

		// Handle backtrace.
		backtrace := diagnostic.Backtrace(ctx, err)
		if len(backtrace) > 0 {
			color := diagnostic.Color(ctx)
			fmt.Fprintf(info.ErrOutput, color.Sprintf(
				"\n%s: %s\n",
				color.Bold(color.Red("error")),
				color.Bold(diagnostic.Cause(err)),
			))
		}
		for i, span := range backtrace {
			if !info.Backtrace && i != len(backtrace)-1 {
				if i == 0 {
					color := diagnostic.Color(ctx)
					frame := "frame"
					if len(backtrace) > 2 {
						frame = "frames"
					}
					fmt.Fprintf(info.ErrOutput, color.Sprintf(color.Cyan(" ⫶ %d %s hidden ⫶\n"), len(backtrace)-1, frame))
				}
				continue
			}

			pretty := span.Pretty(ctx, diagnostic.WithNumContext(2))
			lines := strings.Split(pretty, "\n")
			for j, line := range lines {
				if j == 0 {
					lines[j] = fmt.Sprintf(" %d: %s", i+1, line)
				} else {
					lines[j] = fmt.Sprintf("    %s", line)
				}
			}
			fmt.Fprintf(info.ErrOutput, "%s\n", strings.Join(lines, "\n"))
		}

		var numErrs int
		if len(backtrace) == 0 {
			// Handle diagnostic errors.
			spans := diagnostic.Spans(err)
			for _, span := range spans {
				fmt.Fprintf(info.ErrOutput, "\n%s\n", span.Pretty(ctx))
			}
			numErrs = len(spans)
		} else {
			numErrs = 1
		}

		err = errdefs.WithAbort(err, numErrs)
	}()

	mod, err := parser.Parse(ctx, rc)
	if err != nil {
		return err
	}

	var targets []codegen.Target
	for _, target := range info.Targets {
		targets = append(targets, codegen.Target{Name: target})
	}

	done := make(chan struct{})
	defer func() { <-done }()

	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(ctx)

	go func() {
		defer close(done)

		t, err := tui.New(ctx, cancel, pm)
		if err != nil {
			return
			panic(err)
		}

		p := tea.NewProgram(t)
		p.EnableMouseCellMotion()
		defer p.DisableMouseCellMotion()

		err = p.Start()
		if err != nil {
			panic(err)
		}
	}()

	ctx = codegen.WithImageResolver(ctx, codegen.NewCachedImageResolver(cln))

	pm.Go(func() error {
		defer pm.Release()

		solveReq, err := hlb.Compile(ctx, cln, mod, targets)
		if err != nil {
			// Ignore early exits from the debugger.
			if err == codegen.ErrDebugExit {
				return nil
			}
			return err
		}

		if solveReq == nil {
			return nil
		}

		log.Println("[solver] solving")
		defer func() {
			log.Println("[solver] finished solving")
		}()
		return solveReq.Solve(ctx, cln)
	})

	return pm.Wait()
}

func ModuleReadCloser(args []string) (io.ReadCloser, error) {
	if len(args) == 0 {
		return os.Open(DefaultHLBFilename)
	} else if args[0] == "-" {
		fi, err := os.Stdin.Stat()
		if err != nil {
			return nil, err
		}

		if fi.Mode()&os.ModeNamedPipe == 0 {
			return nil, fmt.Errorf("must provide path to hlb module or pipe to stdin")
		}

		return os.Stdin, nil
	}

	return os.Open(args[0])
}
