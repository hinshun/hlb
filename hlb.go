package hlb

import (
	"bufio"
	"context"
	"io"
	"os"

	isatty "github.com/mattn/go-isatty"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	"github.com/openllb/hlb/codegen"
	"github.com/openllb/hlb/parser"
	"github.com/openllb/hlb/report"
)

func Compile(ctx context.Context, cln *client.Client, target string, r io.Reader, debug bool) (llb.State, *codegen.CodeGenInfo, error) {
	st := llb.Scratch()

	file, ib, err := Parse(r, defaultOpts()...)
	if err != nil {
		return st, nil, err
	}

	file, err = report.SemanticCheck(file)
	if err != nil {
		return st, nil, err
	}

	call := &parser.CallStmt{
		Func: &parser.Ident{Name: target},
	}

	ibs := map[string]*report.IndexedBuffer{
		file.Pos.Filename: ib,
	}

	var opts []codegen.CodeGenOption
	if debug {
		r := bufio.NewReader(os.Stdin)

		opts = append(opts, codegen.WithDebugger(codegen.NewDebugger(ctx, cln, os.Stderr, r, ibs)))
	}

	return codegen.Generate(call, file, opts...)
}

func defaultOpts() []ParseOption {
	var opts []ParseOption
	if isatty.IsTerminal(os.Stderr.Fd()) {
		opts = append(opts, WithColor(true))
	}
	return opts
}
