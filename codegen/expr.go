package codegen

import (
	"github.com/moby/buildkit/client/llb"
	"github.com/openllb/hlb/parser"
)

func emitStringExpr(info *CodeGenInfo, scope *parser.Scope, call *parser.CallStmt, expr *parser.Expr) (string, error) {
	switch {
	case expr.Ident != nil:
		obj := scope.Lookup(expr.Ident.Name)
		switch obj.Kind {
		case parser.DeclKind:
			switch n := obj.Node.(type) {
			case *parser.FuncDecl:
				return emitStringFuncDecl(info, scope, n, call, noopAliasCallback)
			case *parser.AliasDecl:
				return emitStringAliasDecl(info, scope, n, call)
			default:
				panic("unknown decl object")
			}
		case parser.ExprKind:
			return obj.Data.(string), nil
		default:
			panic("unknown obj type")
		}
	case expr.BasicLit != nil:
		return *expr.BasicLit.Str, nil
	case expr.FuncLit != nil:
		return emitStringBlock(info, scope, expr.FuncLit.Body.NonEmptyStmts())
	default:
		panic("unknown string expr")
	}
}

func emitIntExpr(info *CodeGenInfo, scope *parser.Scope, expr *parser.Expr) (int, error) {
	switch {
	case expr.Ident != nil:
		obj := scope.Lookup(expr.Ident.Name)
		switch obj.Kind {
		case parser.DeclKind:
			panic("unimplemented")
		case parser.ExprKind:
			return obj.Data.(int), nil
		default:
			panic("unknown obj type")
		}
	case expr.BasicLit != nil:
		switch {
		case expr.BasicLit.Decimal != nil:
			return *expr.BasicLit.Decimal, nil
		case expr.BasicLit.Numeric != nil:
			return int(expr.BasicLit.Numeric.Value), nil
		default:
			panic("unknown int basic lit")
		}
	case expr.FuncLit != nil:
		panic("unimplemented")
	default:
		panic("unknown int expr")
	}
}

func emitBoolExpr(info *CodeGenInfo, scope *parser.Scope, expr *parser.Expr) (bool, error) {
	switch {
	case expr.Ident != nil:
		obj := scope.Lookup(expr.Ident.Name)
		switch obj.Kind {
		case parser.DeclKind:
			panic("unimplemented")
		case parser.ExprKind:
			return obj.Data.(bool), nil
		default:
			panic("unknown obj type")
		}
	case expr.BasicLit != nil:
		return *expr.BasicLit.Bool, nil
	case expr.FuncLit != nil:
		panic("unimplemented")
	default:
		panic("unknown bool expr")
	}
}

func maybeEmitBoolExpr(info *CodeGenInfo, scope *parser.Scope, args []*parser.Expr) (bool, error) {
	v := true
	if len(args) > 0 {
		var err error
		v, err = emitBoolExpr(info, scope, args[0])
		if err != nil {
			return v, err
		}
	}
	return v, nil
}

func emitFilesystemExpr(info *CodeGenInfo, scope *parser.Scope, call *parser.CallStmt, expr *parser.Expr, ac aliasCallback) (llb.State, error) {
	switch {
	case expr.Ident != nil:
		obj := scope.Lookup(expr.Ident.Name)
		switch obj.Kind {
		case parser.DeclKind:
			switch n := obj.Node.(type) {
			case *parser.FuncDecl:
				return emitFilesystemFuncDecl(info, scope, n, call, noopAliasCallback)
			case *parser.AliasDecl:
				return emitFilesystemAliasDecl(info, scope, n, call)
			default:
				panic("unknown decl object")
			}
		case parser.ExprKind:
			return obj.Data.(llb.State), nil
		default:
			panic("unknown obj type")
		}
	case expr.BasicLit != nil:
		panic("fs expr cannot be basic lit")
	case expr.FuncLit != nil:
		v, err := emitFuncLit(info, scope, expr.FuncLit, "", ac)
		if err != nil {
			return llb.Scratch(), err
		}
		return v.(llb.State), nil
	default:
		panic("unknown fs expr")
	}
}

func emitOptionExpr(info *CodeGenInfo, scope *parser.Scope, call *parser.CallStmt, op string, expr *parser.Expr) ([]interface{}, error) {
	switch {
	case expr.Ident != nil:
		obj := scope.Lookup(expr.Ident.Name)
		switch obj.Kind {
		case parser.DeclKind:
			switch n := obj.Node.(type) {
			case *parser.FuncDecl:
				return emitOptionFuncDecl(info, scope, n, call, op)
			default:
				panic("unknown option decl kind")
			}
		case parser.ExprKind:
			return obj.Data.([]interface{}), nil
		default:
			panic("unknown obj type")
		}
	case expr.BasicLit != nil:
		panic("option expr cannot be basic lit")
	case expr.FuncLit != nil:
		v, err := emitFuncLit(info, scope, expr.FuncLit, op, noopAliasCallback)
		if err != nil {
			return nil, err
		}
		return v.([]interface{}), nil
	default:
		panic("unknown option expr")
	}
}
