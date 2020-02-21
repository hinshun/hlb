package command

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/openllb/hlb"
	"github.com/openllb/hlb/codegen"
	"github.com/openllb/hlb/parser"
	"github.com/openllb/hlb/report"
	"github.com/openllb/hlb/solver"
	cli "github.com/urfave/cli/v2"
)

var publishCommand = &cli.Command{
	Name:      "publish",
	Usage:     "compiles a HLB program and publishes it as a HLB frontend",
	ArgsUsage: "<*.hlb>",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "target",
			Aliases: []string{"t"},
			Usage:   "target filesystem to compile",
			Value:   "default",
		},
		&cli.StringFlag{
			Name:  "ref",
			Usage: "frontend image reference",
		},
	},
	Action: func(c *cli.Context) error {
		if !c.IsSet("ref") {
			return fmt.Errorf("--ref must be specified")
		}

		var r io.Reader
		if c.NArg() == 0 {
			fi, err := os.Stdin.Stat()
			if err != nil {
				return err
			}

			if fi.Mode()&os.ModeNamedPipe == 0 {
				return fmt.Errorf("must provided hlb file or pipe to stdin")
			}

			r = os.Stdin
		} else {
			f, err := os.Open(c.Args().First())
			if err != nil {
				return err
			}
			r = f
		}

		file, _, err := hlb.Parse(r, defaultOpts()...)
		if err != nil {
			return err
		}

		file, err = report.SemanticCheck(file)
		if err != nil {
			return err
		}

		var params []*parser.Field
		parser.Inspect(file, func(node parser.Node) bool {
			switch n := node.(type) {
			case *parser.FuncDecl:
				if n.Name.Name == c.String("target") {
					params = n.Params.List
					return false
				}
			case *parser.AliasDecl:
				if n.Ident.Name == c.String("target") {
					params = n.Func.Params.List
					return false
				}
			}
			return true
		})

		frontendStmts := []*parser.Stmt{
			parser.NewCallStmt("frontendOpt", []*parser.Expr{
				parser.NewStringExpr("hlb-target"),
				parser.NewStringExpr(c.String("target")),
			}, nil, nil),
		}
		for _, param := range params {
			fun := "frontendOpt"
			if param.Type.Type() == parser.Filesystem {
				fun = "frontendInput"
			}
			frontendStmts = append(frontendStmts, parser.NewCallStmt(fun, []*parser.Expr{
				parser.NewStringExpr(param.Name.Name),
				parser.NewIdentExpr(param.Name.Name),
			}, nil, nil))
		}

		signatureHLB := &parser.File{
			Decls: []*parser.Decl{
				{
					Func: &parser.FuncDecl{
						Type: parser.NewType(parser.Filesystem),
						Name: parser.NewIdent(c.String("target")),
						Params: &parser.FieldList{
							List: params,
						},
						Body: &parser.BlockStmt{
							List: []*parser.Stmt{
								parser.NewCallStmt("generate", []*parser.Expr{
									parser.NewFuncLitExpr(parser.Filesystem,
										parser.NewCallStmt("image", []*parser.Expr{
											parser.NewStringExpr(c.String("ref")),
										}, nil, nil),
									),
								}, parser.NewWithFuncLit(frontendStmts...), nil),
							},
						},
					},
				},
			},
		}

		entryName := "publish_hlb"
		publishHLB := &parser.File{
			Decls: []*parser.Decl{
				{
					Func: &parser.FuncDecl{
						Type:   parser.NewType(parser.Filesystem),
						Name:   parser.NewIdent(entryName),
						Params: &parser.FieldList{},
						Body: &parser.BlockStmt{
							List: []*parser.Stmt{
								parser.NewCallStmt("image", []*parser.Expr{
									parser.NewStringExpr("openllb/hlb"),
								}, nil, nil),
								parser.NewCallStmt("mkfile", []*parser.Expr{
									parser.NewStringExpr(hlb.SourceHLB),
									parser.NewNumericExpr(int64(hlb.HLBFileMode), 8),
									parser.NewStringExpr(file.String()),
								}, nil, nil),
								parser.NewCallStmt("mkfile", []*parser.Expr{
									parser.NewStringExpr(hlb.SignatureHLB),
									parser.NewNumericExpr(int64(hlb.HLBFileMode), 8),
									parser.NewStringExpr(signatureHLB.String()),
								}, nil, nil),
							},
						},
					},
				},
			},
		}

		root, err := report.SemanticCheck(publishHLB)
		if err != nil {
			return err
		}

		st, _, err := codegen.Generate(parser.NewCallStmt(entryName, nil, nil, nil).Call, root)
		if err != nil {
			return err
		}

		ctx := context.Background()
		cln, err := solver.BuildkitClient(ctx, c.String("addr"))
		if err != nil {
			return err
		}

		return solver.Solve(ctx, cln, st, solver.WithPushImage(c.String("ref")))
	},
}
