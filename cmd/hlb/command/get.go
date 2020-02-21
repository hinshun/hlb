package command

import (
	"context"
	"fmt"
	"path"

	"github.com/openllb/hlb"
	"github.com/openllb/hlb/codegen"
	"github.com/openllb/hlb/parser"
	"github.com/openllb/hlb/report"
	"github.com/openllb/hlb/solver"
	cli "github.com/urfave/cli/v2"
)

var getCommand = &cli.Command{
	Name:      "get",
	Usage:     "retrieves the HLB signatures from a HLB frontend",
	ArgsUsage: "<image ref>",
	Action: func(c *cli.Context) error {
		if c.NArg() != 1 {
			return fmt.Errorf("must have exactly one argument")
		}

		ref := c.Args().First()
		frontendFile := fmt.Sprintf("%s.hlb", path.Base(ref))

		entryName := "get"
		getHLB := &parser.File{
			Decls: []*parser.Decl{
				{
					Func: &parser.FuncDecl{
						Type:   parser.NewType(parser.Filesystem),
						Name:   parser.NewIdent(entryName),
						Params: &parser.FieldList{},
						Body: &parser.BlockStmt{
							List: []*parser.Stmt{
								parser.NewCallStmt("scratch", nil, nil, nil),
								parser.NewCallStmt("copy", []*parser.Expr{
									parser.NewFuncLitExpr(parser.Filesystem,
										parser.NewCallStmt("image", []*parser.Expr{
											parser.NewStringExpr(ref),
										}, nil, nil),
									),
									parser.NewStringExpr(hlb.SignatureHLB),
									parser.NewStringExpr(frontendFile),
								}, nil, nil),
							},
						},
					},
				},
			},
		}

		root, err := report.SemanticCheck(getHLB)
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

		return solver.Solve(ctx, cln, st, solver.WithDownload("."))
	},
}
