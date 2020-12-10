package codegen

// import (
// 	"bufio"
// 	"context"
// 	"fmt"
// 	"io"
// 	"os"
// 	"os/exec"
// 	"strings"

// 	shellquote "github.com/kballard/go-shellquote"
// 	"github.com/moby/buildkit/client"
// 	"github.com/moby/buildkit/client/llb"
// 	"github.com/moby/buildkit/solver/pb"
// 	digest "github.com/opencontainers/go-digest"
// 	"github.com/openllb/hlb/errdefs"
// 	"github.com/openllb/hlb/parser"
// 	"github.com/pkg/errors"
// )

// func NewDebugger(c *client.Client, w io.Writer, r *bufio.Reader) interface{} {
// 	return func(ctx context.Context, scope *parser.Scope, node parser.Node, ret Value) error {
// 		debug := func(s *snapshot) error {
// 			for {
// 				fmt.Fprint(w, "(hlb) ")

// 				command, err := r.ReadString('\n')
// 				if err != nil {
// 					return err
// 				}

// 				command = strings.Replace(command, "\n", "", -1)

// 				if command == "" {
// 					continue
// 				}

// 				args, err := shellquote.Split(command)
// 				if err != nil {
// 					return err
// 				}

// 				switch args[0] {
// 				case "dir":
// 					fs, err := s.ret.Filesystem()
// 					if err != nil {
// 						fmt.Fprintf(w, "current step is not in a fs scope\n")
// 						continue
// 					}

// 					dir, err := fs.State.GetDir(ctx)
// 					if err != nil {
// 						fmt.Fprintf(w, "err: %s\n", err)
// 						continue
// 					}

// 					fmt.Fprintf(w, "Working directory %q\n", dir)
// 				case "dot":
// 					fs, err := s.ret.Filesystem()
// 					if err != nil {
// 						fmt.Fprintf(w, "current step is not in a fs scope\n")
// 						continue
// 					}

// 					var sh string
// 					if len(args) == 2 {
// 						sh = args[1]
// 					}

// 					err = printGraph(ctx, fs.State, sh)
// 					if err != nil {
// 						fmt.Fprintf(w, "err: %s\n", err)
// 					}
// 					continue
// 				case "env":
// 					fs, err := s.ret.Filesystem()
// 					if err != nil {
// 						fmt.Fprintf(w, "current step is not in a fs scope\n")
// 						continue
// 					}

// 					env, err := fs.State.Env(ctx)
// 					if err != nil {
// 						fmt.Fprintf(w, "err: %s\n", err)
// 						continue
// 					}

// 					fmt.Fprintf(w, "Environment %s\n", env)
// 				case "funcs":
// 					for _, obj := range s.scope.Defined() {
// 						switch obj.Node.(type) {
// 						case *parser.FuncDecl, *parser.BindClause:
// 							fmt.Fprintf(w, "%s\n", obj.Ident)
// 						}
// 					}
// 				case "help":
// 					fmt.Fprintf(w, "# Inspect\n")
// 					fmt.Fprintf(w, "help - shows this help message\n")
// 					fmt.Fprintf(w, "list - show source code\n")
// 					fmt.Fprintf(w, "print - print evaluate an expression\n")
// 					fmt.Fprintf(w, "funcs - print list of functions\n")
// 					fmt.Fprintf(w, "locals - print local variables\n")
// 					fmt.Fprintf(w, "types - print list of types\n")
// 					fmt.Fprintf(w, "whatis - print type of an expression\n")
// 					fmt.Fprintf(w, "# Movement\n")
// 					fmt.Fprintf(w, "exit - exit the debugger\n")
// 					fmt.Fprintf(w, "break [ <symbol> | <linespec> ] - sets a breakpoint\n")
// 					fmt.Fprintf(w, "breakpoints - print out info for active breakpoints\n")
// 					fmt.Fprintf(w, "clear [ <breakpoint-index> ] - deletes breakpoint\n")
// 					fmt.Fprintf(w, "continue - run until breakpoint or program termination\n")
// 					fmt.Fprintf(w, "next - step over to next source line\n")
// 					fmt.Fprintf(w, "step - single step through program\n")
// 					fmt.Fprintf(w, "stepout - step out of current function\n")
// 					fmt.Fprintf(w, "reverse-step - single step backwards through program\n")
// 					fmt.Fprintf(w, "restart - restart program from the start\n")
// 					fmt.Fprintf(w, "# Filesystem\n")
// 					fmt.Fprintf(w, "dir - print working directory\n")
// 					fmt.Fprintf(w, "env - print environment\n")
// 					fmt.Fprintf(w, "network - print network mode\n")
// 					fmt.Fprintf(w, "security - print security mode\n")
// 				case "list", "l":
// 					if showList {
// 						printList(ctx, s.node, w)
// 					} else {
// 						fmt.Fprintf(w, "Program has not started yet\n")
// 					}
// 				case "locals":
// 					if fun != nil {
// 						for _, arg := range fun.Params.Fields() {
// 							obj := s.scope.Lookup(arg.Name.Text)
// 							if obj == nil {
// 								fmt.Fprintf(w, "err: %s\n", errors.WithStack(errdefs.WithUndefinedIdent(arg, nil)))
// 								continue
// 							}
// 							fmt.Fprintf(w, "%s %s = %#v\n", arg.Type, arg.Name, obj.Data)
// 						}
// 					}
// 				case "network":
// 					fs, err := s.ret.Filesystem()
// 					if err != nil {
// 						fmt.Fprintf(w, "current step is not in a fs scope\n")
// 						continue
// 					}

// 					network, err := fs.State.GetNetwork(ctx)
// 					if err != nil {
// 						fmt.Fprintf(w, "err: %s\n", err)
// 						continue
// 					}

// 					fmt.Fprintf(w, "Network %s\n", network)
// 				case "print":
// 					fmt.Fprintf(w, "print\n")
// 				case "security":
// 					fs, err := s.ret.Filesystem()
// 					if err != nil {
// 						fmt.Fprintf(w, "current step is not in a fs scope\n")
// 						continue
// 					}

// 					security, err := fs.State.GetSecurity(ctx)
// 					if err != nil {
// 						fmt.Fprintf(w, "err: %s\n", err)
// 						continue
// 					}

// 					fmt.Fprintf(w, "Security %s\n", security)
// 				case "types":
// 					for _, kind := range []string{"string", "int", "bool", "fs", "option"} {
// 						fmt.Fprintf(w, "%s\n", kind)
// 					}
// 				case "whatis":
// 					fmt.Fprintf(w, "unimplemented\n")
// 				default:
// 					fmt.Fprintf(w, "unrecognized command %s\n", command)
// 				}
// 			}
// 		}
// 		return nil
// 	}
// }

// func printGraph(ctx context.Context, st llb.State, sh string) error {
// 	def, err := st.Marshal(ctx, llb.LinuxAmd64)
// 	if err != nil {
// 		return err
// 	}

// 	ops, err := loadLLB(def)
// 	if err != nil {
// 		return err
// 	}

// 	r, w := io.Pipe()
// 	defer r.Close()

// 	go func() {
// 		defer w.Close()
// 		writeDot(ops, w)
// 	}()

// 	if sh == "" {
// 		_, err = io.Copy(os.Stderr, r)
// 		return err
// 	}

// 	cmd := exec.CommandContext(ctx, "sh", "-c", sh)
// 	cmd.Stdin = r
// 	cmd.Stdout = os.Stdout
// 	cmd.Stderr = os.Stderr

// 	return cmd.Run()
// }

// type llbOp struct {
// 	Op         pb.Op
// 	Digest     digest.Digest
// 	OpMetadata pb.OpMetadata
// }

// func loadLLB(def *llb.Definition) ([]llbOp, error) {
// 	var ops []llbOp
// 	for _, dt := range def.Def {
// 		var op pb.Op
// 		if err := (&op).Unmarshal(dt); err != nil {
// 			return nil, err
// 		}
// 		dgst := digest.FromBytes(dt)
// 		ent := llbOp{Op: op, Digest: dgst, OpMetadata: def.Metadata[dgst]}
// 		ops = append(ops, ent)
// 	}
// 	return ops, nil
// }

// func writeDot(ops []llbOp, w io.Writer) {
// 	fmt.Fprintln(w, "digraph {")
// 	defer fmt.Fprintln(w, "}")
// 	for _, op := range ops {
// 		name, shape := attr(op.Digest, op.Op)
// 		fmt.Fprintf(w, "  %q [label=%q shape=%q];\n", op.Digest, name, shape)
// 	}
// 	for _, op := range ops {
// 		for i, inp := range op.Op.Inputs {
// 			label := ""
// 			if eo, ok := op.Op.Op.(*pb.Op_Exec); ok {
// 				for _, m := range eo.Exec.Mounts {
// 					if int(m.Input) == i && m.Dest != "/" {
// 						label = m.Dest
// 					}
// 				}
// 			}
// 			fmt.Fprintf(w, "  %q -> %q [label=%q];\n", inp.Digest, op.Digest, label)
// 		}
// 	}
// }

// func attr(dgst digest.Digest, op pb.Op) (string, string) {
// 	switch op := op.Op.(type) {
// 	case *pb.Op_Source:
// 		return op.Source.Identifier, "ellipse"
// 	case *pb.Op_Exec:
// 		return strings.Join(op.Exec.Meta.Args, " "), "box"
// 	case *pb.Op_Build:
// 		return "build", "box3d"
// 	case *pb.Op_File:
// 		names := []string{}

// 		for _, action := range op.File.Actions {
// 			var name string

// 			switch act := action.Action.(type) {
// 			case *pb.FileAction_Copy:
// 				name = fmt.Sprintf("copy{src=%s, dest=%s}", act.Copy.Src, act.Copy.Dest)
// 			case *pb.FileAction_Mkfile:
// 				name = fmt.Sprintf("mkfile{path=%s}", act.Mkfile.Path)
// 			case *pb.FileAction_Mkdir:
// 				name = fmt.Sprintf("mkdir{path=%s}", act.Mkdir.Path)
// 			case *pb.FileAction_Rm:
// 				name = fmt.Sprintf("rm{path=%s}", act.Rm.Path)
// 			}

// 			names = append(names, name)
// 		}
// 		return strings.Join(names, ","), "note"
// 	default:
// 		return dgst.String(), "plaintext"
// 	}
// }
