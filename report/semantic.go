package report

import (
	"fmt"

	"github.com/openllb/hlb/parser"
)

func LinkDocs(file *parser.File) {
	var (
		lastCG *parser.CommentGroup
	)

	parser.Inspect(file, func(node parser.Node) bool {
		switch n := node.(type) {
		case *parser.Decl:
			if n.Doc != nil {
				lastCG = n.Doc
			}
		case *parser.FuncDecl:
			if lastCG != nil && lastCG.End().Line == n.Pos.Line-1 {
				n.Doc = lastCG
			}

			parser.Inspect(n, func(node parser.Node) bool {
				switch n := node.(type) {
				case *parser.CommentGroup:
					lastCG = n
				case *parser.CallStmt:
					if lastCG != nil && lastCG.End().Line == n.Pos.Line-1 {
						n.Doc = lastCG
					}
				}
				return true
			})
		}
		return true
	})
}

func SemanticCheck(file *parser.File) (*parser.File, error) {
	var (
		dupDecls []*parser.Ident
	)

	file.Scope = parser.NewScope(file, nil)
	parser.Inspect(file, func(node parser.Node) bool {
		switch n := node.(type) {
		case *parser.FuncDecl:
			fun := n

			if fun.Name != nil {
				obj := file.Scope.Lookup(fun.Name.Name)
				if obj != nil {
					if len(dupDecls) == 0 {
						dupDecls = append(dupDecls, obj.Ident)
					}
					dupDecls = append(dupDecls, fun.Name)
					return false
				}

				file.Scope.Insert(&parser.Object{
					Kind:  parser.DeclKind,
					Ident: fun.Name,
					Node:  fun,
				})
			}

			fun.Scope = parser.NewScope(fun, file.Scope)

			if fun.Params != nil {
				for _, param := range fun.Params.List {
					fun.Scope.Insert(&parser.Object{
						Kind:  parser.FieldKind,
						Ident: param.Name,
						Node:  param,
					})
				}
			}

			parser.Inspect(fun, func(node parser.Node) bool {
				switch n := node.(type) {
				case *parser.CallStmt:
					if n.Alias == nil {
						return true
					}

					n.Alias.Func = fun
					n.Alias.Call = n

					if n.Alias.Ident != nil {
						file.Scope.Insert(&parser.Object{
							Kind:  parser.DeclKind,
							Ident: n.Alias.Ident,
							Node:  n.Alias,
						})
					}
				}
				return true
			})
		}
		return true
	})
	if len(dupDecls) > 0 {
		return file, ErrDuplicateDecls{dupDecls}
	}

	var errs []error
	parser.Inspect(file, func(n parser.Node) bool {
		fun, ok := n.(*parser.FuncDecl)
		if !ok {
			return true
		}

		if fun.Params != nil {
			err := checkFieldList(fun.Params.List)
			if err != nil {
				errs = append(errs, err)
				return false
			}
		}

		if fun.Type != nil && fun.Body != nil {
			var op string
			if fun.Type.Type() == parser.Option {
				op = string(fun.Type.SubType())
			}

			err := checkBlockStmt(fun.Scope, fun.Type, fun.Body, op)
			if err != nil {
				errs = append(errs, err)
				return false
			}
		}

		return true
	})
	if len(errs) > 0 {
		return file, ErrSemantic{errs}
	}

	return file, nil
}

func checkFieldList(fields []*parser.Field) error {
	var dupFields []*parser.Field

	fieldSet := make(map[string]*parser.Field)
	for _, field := range fields {
		if field.Name == nil {
			continue
		}

		dupField, ok := fieldSet[field.Name.Name]
		if ok {
			if len(dupFields) == 0 {
				dupFields = append(dupFields, dupField)
			}
			dupFields = append(dupFields, field)
			continue
		}

		fieldSet[field.Name.Name] = field
	}

	if len(dupFields) > 0 {
		return ErrDuplicateFields{dupFields}
	}

	return nil
}

func checkBlockStmt(scope *parser.Scope, typ *parser.Type, block *parser.BlockStmt, op string) error {
	if typ.Equals(parser.Option) {
		return checkOptionBlockStmt(scope, typ, block, op)
	}

	if block.NumStmts() == 0 {
		return ErrNoSource{block}
	}

	foundSource := false

	i := -1
	for _, stmt := range block.NonEmptyStmts() {
		call := stmt.Call
		if stmt.Call.Func == nil || Contains(Debugs, call.Func.Name) {
			continue
		}

		i++

		if !foundSource {
			if !Contains(BuiltinSources[typ.Type()], call.Func.Name) {
				obj := scope.Lookup(call.Func.Name)
				if obj == nil {
					return ErrFirstSource{call}
				}

				var callType *parser.Type
				switch obj.Kind {
				case parser.DeclKind:
					switch n := obj.Node.(type) {
					case *parser.FuncDecl:
						callType = n.Type
					case *parser.AliasDecl:
						callType = n.Func.Type
					}
				case parser.FieldKind:
					field, ok := obj.Node.(*parser.Field)
					if ok {
						callType = field.Type
					}
				}

				if !callType.Equals(typ.Type()) {
					return ErrFirstSource{call}
				}
			}
			foundSource = true

			err := checkCallStmt(scope, typ, i, call, op)
			if err != nil {
				return err
			}
			continue
		}

		if Contains(BuiltinSources[typ.Type()], call.Func.Name) {
			return ErrOnlyFirstSource{call}
		}

		err := checkCallStmt(scope, typ, i, call, op)
		if err != nil {
			return err
		}
	}

	return nil
}

func checkCallStmt(scope *parser.Scope, typ *parser.Type, index int, call *parser.CallStmt, op string) error {
	var (
		funcs  []string
		params []*parser.Field
	)

	switch typ.Type() {
	case parser.Filesystem, parser.Str:
		if index == 0 {
			funcs = flatMap(BuiltinSources[typ.Type()], Debugs)
		} else {
			funcs = flatMap(Ops, Debugs)
		}
		builtins := Builtins[typ.Type()][call.Func.Name]
		params = handleVariadicParams(builtins, call.Args)
	case parser.Option:
		optionType := parser.ObjType(fmt.Sprintf("%s::%s", parser.Option, op))
		funcs = KeywordsByName[op]
		builtins := Builtins[optionType][call.Func.Name]
		params = handleVariadicParams(builtins, call.Args)
	}

	if !Contains(funcs, call.Func.Name) {
		obj := scope.Lookup(call.Func.Name)
		if obj == nil {
			return ErrInvalidFunc{call}
		}

		var fields []*parser.Field
		if obj.Kind == parser.DeclKind {
			switch n := obj.Node.(type) {
			case *parser.FuncDecl:
				fields = n.Params.List
			case *parser.AliasDecl:
				fields = n.Func.Params.List
			default:
				panic("unknown decl object")
			}
		}
		params = handleVariadicParams(fields, call.Args)
	}

	if len(params) != len(call.Args) {
		return ErrNumArgs{len(params), call}
	}

	for i, arg := range call.Args {
		typ := params[i].Type

		var err error
		switch {
		case arg.Ident != nil:
			err = checkIdentArg(scope, typ.Type(), arg.Ident)
		case arg.BasicLit != nil:
			err = checkBasicLitArg(typ.Type(), arg.BasicLit)

		case arg.FuncLit != nil:
			err = checkFuncLitArg(scope, typ.Type(), arg.FuncLit, call.Func.Name)
		default:
			panic("unknown field type")
		}
		if err != nil {
			return err
		}
	}

	if call.WithOpt != nil {
		var err error
		switch {
		case call.WithOpt.Ident != nil:
			err = checkIdentArg(scope, parser.Option, call.WithOpt.Ident)
		case call.WithOpt.FuncLit != nil:
			err = checkFuncLitArg(scope, parser.Option, call.WithOpt.FuncLit, call.Func.Name)
		default:
			panic("unknown with opt type")
		}
		if err != nil {
			return err
		}
	}

	return nil
}

func checkIdentArg(scope *parser.Scope, typ parser.ObjType, ident *parser.Ident) error {
	obj := scope.Lookup(ident.Name)
	if obj == nil {
		return ErrIdentNotDefined{ident}
	}

	switch obj.Kind {
	case parser.DeclKind:
		switch n := obj.Node.(type) {
		case *parser.FuncDecl:
			if n.Params.NumFields() > 0 {
				return ErrFuncArg{ident}
			}
		case *parser.AliasDecl:
			if n.Func.Params.NumFields() > 0 {
				return ErrFuncArg{ident}
			}
		default:
			panic("unknown arg type")
		}
	case parser.FieldKind:
		var err error
		switch d := obj.Node.(type) {
		case *parser.Field:
			if !d.Type.Equals(typ) {
				return ErrWrongArgType{ident.Pos, typ, d.Type.Type()}
			}
		default:
			panic("unknown arg type")
		}
		if err != nil {
			return err
		}
	default:
		panic("unknown ident type")
	}
	return nil
}

func checkBasicLitArg(typ parser.ObjType, lit *parser.BasicLit) error {
	switch typ {
	case parser.Str:
		if lit.Str == nil {
			return ErrWrongArgType{lit.Pos, typ, lit.ObjType()}
		}
	case parser.Int:
		if lit.Decimal == nil && lit.Numeric == nil {
			return ErrWrongArgType{lit.Pos, typ, lit.ObjType()}
		}
	case parser.Bool:
		if lit.Bool == nil {
			return ErrWrongArgType{lit.Pos, typ, lit.ObjType()}
		}
	default:
		return ErrWrongArgType{lit.Pos, typ, lit.ObjType()}
	}
	return nil
}

func checkFuncLitArg(scope *parser.Scope, typ parser.ObjType, lit *parser.FuncLit, op string) error {
	if !lit.Type.Equals(typ) {
		return ErrWrongArgType{lit.Pos, typ, lit.Type.ObjType}
	}

	return checkBlockStmt(scope, lit.Type, lit.Body, op)
}

func checkOptionBlockStmt(scope *parser.Scope, typ *parser.Type, block *parser.BlockStmt, op string) error {
	i := -1
	for _, stmt := range block.List {
		call := stmt.Call
		if call == nil || call.Func == nil {
			continue
		}
		i++

		callType := parser.NewType(parser.ObjType(fmt.Sprintf("%s::%s", parser.Option, op)))
		err := checkCallStmt(scope, callType, i, call, op)
		if err != nil {
			return err
		}
	}
	return nil
}

func handleVariadicParams(fields []*parser.Field, args []*parser.Expr) []*parser.Field {
	params := make([]*parser.Field, len(fields))
	copy(params, fields)

	if len(params) > 0 && params[len(params)-1].Variadic != nil {
		variadicParam := params[len(params)-1]
		params = params[:len(params)-1]
		for i, _ := range args[len(params):] {
			params = append(params, parser.NewField(variadicParam.Type.Type(), fmt.Sprintf("%s[%d]", variadicParam.Name, i), false))
		}
	}

	return params
}
