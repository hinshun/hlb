package linter

import (
	"context"
	"fmt"
	"os"

	"github.com/openllb/hlb/checker"
	"github.com/openllb/hlb/codegen"
	"github.com/openllb/hlb/parser"
)

type Linter struct {
	Recursive bool
	errs      []ErrLintModule
}

type LintOption func(*Linter)

func WithRecursive() LintOption {
	return func(l *Linter) {
		l.Recursive = true
	}
}

func Lint(mod *parser.Module, opts ...LintOption) error {
	linter := Linter{}
	for _, opt := range opts {
		opt(&linter)
	}
	err := linter.Lint(mod)
	if err != nil {
		return err
	}
	if len(linter.errs) > 0 {
		return ErrLint{mod.Pos.Filename, linter.errs}
	}
	return nil
}

func (l *Linter) Lint(mod *parser.Module) error {
	var (
		modErr error
		errs   []error
	)
	parser.Match(mod, parser.MatchOpts{},
		func(id *parser.ImportDecl) {
			if id.DeprecatedPath != nil {
				errs = append(errs, ErrDeprecated{id.DeprecatedPath, `import without keyword "from" infront of the expression is deprecated`})
				id.From = &parser.From{Text: "from"}
				id.Expr = &parser.Expr{
					BasicLit: &parser.BasicLit{
						Str: id.DeprecatedPath,
					},
				}
				id.DeprecatedPath = nil
			}

			if !l.Recursive {
				return
			}

			err := l.LintRecursive(mod, id.Expr)
			if err != nil {
				if lintErr, ok := err.(ErrLint); ok {
					l.errs = append(l.errs, lintErr.Errs...)
				} else {
					modErr = err
				}
			}
		},
		func(call *parser.CallStmt) {
			err := l.lintCall(call.Name, call.Args)
			if err != nil {
				errs = append(errs, err)
			}
		},
		func(call *parser.CallExpr) {
			err := l.lintCall(call.Name, call.Args())
			if err != nil {
				errs = append(errs, err)
			}
		},
	)
	if modErr != nil {
		return modErr
	}
	if len(errs) > 0 {
		l.errs = append(l.errs, ErrLintModule{mod, errs})
	}
	return nil
}

func (l *Linter) LintRecursive(mod *parser.Module, expr *parser.Expr) error {
	cg, err := codegen.New(nil)
	if err != nil {
		return err
	}

	ret := codegen.NewRegister()
	err = cg.EmitExpr(context.Background(), mod.Scope, expr, nil, nil, nil, ret)
	if err != nil {
		return err
	}

	if ret.Kind() != parser.String {
		return nil
	}

	path, err := ret.String()
	if err != nil {
		return err
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	imod, _, err := parser.Parse(f)
	if err != nil {
		return err
	}

	err = checker.SemanticPass(imod)
	if err != nil {
		return err
	}

	return l.Lint(imod)
}

func (l *Linter) lintCall(name *parser.IdentExpr, args []*parser.Expr) error {
	if name.Reference != nil {
		return nil
	}
	switch name.Ident.Text {
	case "chmod":
		if len(args) < 1 {
			return nil
		}
		return l.lintFileMode(args[0])
	case "mode":
		if len(args) < 1 {
			return nil
		}
		return l.lintFileMode(args[0])
	case "mkdir":
		if len(args) < 2 {
			return nil
		}
		return l.lintFileMode(args[1])
	case "mkfile":
		if len(args) < 2 {
			return nil
		}
		return l.lintFileMode(args[1])
	}
	return nil
}

func (l *Linter) lintFileMode(expr *parser.Expr) error {
	if expr.BasicLit == nil {
		return nil
	}

	switch {
	case expr.BasicLit.Decimal != nil:
		return ErrNonOctalFileMode{Node: expr, IntegerType: "decimal"}
	case expr.BasicLit.Numeric != nil:
		nl := expr.BasicLit.Numeric
		if nl.Base == 8 {
			return nil
		}

		return ErrNonOctalFileMode{Node: expr, IntegerType: fmt.Sprintf("%s (%s)", nl.PrefixName, string(nl.Prefix))}
	}
	return nil
}
