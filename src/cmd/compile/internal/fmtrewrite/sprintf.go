// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fmtrewrite

import (
	"cmd/compile/internal/base"
	"cmd/compile/internal/ir"
	"cmd/compile/internal/reflectdata"
	"cmd/compile/internal/typecheck"
	"cmd/compile/internal/types"
	"go/constant"
)

func RewriteSprintf(fn *ir.Func) {
	if base.Flag.Cfg.BeastOpt.FmtRewrite == 0 {
		return
	}
	ir.CurFunc = fn

	aa := assignAnalysis{fn: fn}
	edit := func(n ir.Node) ir.Node {
		call, ok := n.(*ir.CallExpr)
		if !ok {
			return n
		}

		// Check 1: function is `fmt.Sprintf`.
		if n.Op() != ir.OCALLFUNC || !call.IsDDD || call.Fun.Op() != ir.ONAME {
			return n
		}
		fn := call.Fun.(*ir.Name).Sym()
		if !(fn.Pkg.Path == "fmt" || fn.Pkg == types.LocalPkg && base.Ctxt.Pkgpath == "fmt") || fn.Name != "Sprintf" {
			return n
		}

		// Check 2: format is constant.
		arg0 := ir.StaticValue(call.Args[0])
		if !ir.IsConst(arg0, constant.String) {
			return n
		}

		// Check 3: format is supported.
		format, ok := parse(ir.StringVal(arg0))
		if !ok {
			return n
		}

		v, chain := aa.StaticValue(call.Args[1])
		var defn ir.Node
		if len(chain) > 0 {
			defn = chain[len(chain)-1].Defn
		}
		if v != call.Args[1] && defn == nil {
			return n
		}
		if v.Op() != ir.OSLICELIT {
			return n
		}
		args := v.(*ir.CompLitExpr).List

		// Check 4: arguments are valid.
		c := checker{
			format: format,
			args:   args,
		}
		ir.SetPos(n)
		if !c.check() {
			return n
		}

		if defn != nil {
			if !aa.checkUse(chain) {
				return n
			}
			for _, x := range args {
				// every argument must be implicit conversion to empty interface.
				if x.Op() != ir.OCONVIFACE || !x.Type().IsEmptyInterface() {
					return n
				}
			}

			// In order to keep execution order deterministic, we need to
			// evaluate the arguments and store them in a temporary variable.
			// We generate the following code:
			//
			//	var arg1 T1
			//    	    ...
			//	var argN TN
			// 	arg1, argN = arg1 expr, argN expr
			//
			var lhs, rhs []ir.Node
			for _, x := range args {
				x := x.(*ir.ConvExpr)
				pos := x.X.Pos()
				name := typecheck.TempAt(pos, ir.CurFunc, x.X.Type())
				lhs = append(lhs, name)
				rhs = append(rhs, x.X)
				// replace the argument with the temporary variable.
				x.X = name
			}
			as2 := ir.NewAssignListStmt(defn.Pos(), ir.OAS2, lhs, rhs)
			defn.(ir.InitNode).PtrInit().Append(typecheck.Stmt(as2))
		}

		if base.Debug.FmtRewrite > 0 {
			base.Warn("intrinsic fmt.Sprintf")
		}

		var list []ir.Node
		argIndex := 0
		var s subst
		s.curfn = ir.CurFunc
		for _, token := range format {
			switch token.Kind {
			default:
				base.Fatalf("unexpected fmt kind")
			case Literal:
				list = append(list, ir.NewString(n.Pos(), token.String))
			case Verb:
				list = append(list, s.fmtValue(token.Verb, args[argIndex]))
				argIndex++
			}
		}
		init := ir.TakeInit(call)
		// Special case for single string.
		if len(list) == 1 {
			x := list[0]
			conv := typecheck.Conv(x, types.Types[types.TSTRING])
			conv.SetTypecheck(1)
			conv.(ir.InitNode).PtrInit().Append(init...)

			if len(s.bufs) != 0 {
				// case like: fmt.Sprintf("%d", v)
				// we need copy the string
				return mkcall("fmt_stringcopy", types.Types[types.TSTRING], conv)
			}
			return conv
		}

		addStr := ir.NewAddStringExpr(n.Pos(), list)
		addStr.SetType(types.Types[types.TSTRING])
		addStr.SetTypecheck(1)
		addStr.PtrInit().Append(init...)
		return addStr
	}

	editlist := func(list ir.Nodes) {
		for i, node := range list {
			list[i] = edit(node)
		}
	}

	// We don't use ir.EditChildren here. A function call can be an
	// expression or a statement, we only optimize expressions here
	// to avoid converting a statement to an expression.
	//
	// fmt.Sprintf("%s", "1") => "1"
	//
	// We cannot do this optimization when this function call is
	// statement because "1" is not a statement.
	ir.VisitList(fn.Body, func(n ir.Node) {
		switch n := n.(type) {
		/*--- statements ---*/
		case *ir.CallExpr:
			editlist(n.Args)
		case *ir.AssignStmt:
			n.Y = edit(n.Y)
		case *ir.AssignListStmt:
			editlist(n.Rhs)
		case *ir.AssignOpStmt:
			n.Y = edit(n.Y)
		case *ir.RangeStmt:
			n.X = edit(n.X)
		case *ir.ReturnStmt:
			editlist(n.Results)

		/*--- expressions ---*/
		case *ir.ParenExpr:
			n.X = edit(n.X)
		case *ir.ConvExpr:
			n.X = edit(n.X)
		case *ir.IndexExpr:
			n.X = edit(n.X)
		case *ir.SliceExpr:
			n.X = edit(n.X)
		case *ir.UnaryExpr:
			n.X = edit(n.X)
		case *ir.KeyExpr:
			n.Key = edit(n.Key)
			n.Value = edit(n.Value)
		case *ir.StructKeyExpr:
			n.Value = edit(n.Value)
		case *ir.CaseClause:
			editlist(n.List)
		case *ir.CompLitExpr:
			editlist(n.List)
		case *ir.AddStringExpr:
			editlist(n.List)
		}
	})
}

func mkcall(name string, t *types.Type, va ...ir.Node) ir.Node {
	fn := typecheck.LookupRuntime(name)
	return mkcall1(fn, t, va...)
}

func mkcall1(fn *ir.Name, t *types.Type, va ...ir.Node) ir.Node {
	if fn.Type() == nil || fn.Type().Kind() != types.TFUNC {
		base.Fatalf("mkcall %v %v", fn, fn.Type())
	}

	n := fn.Type().NumParams()
	if n != len(va) {
		base.Fatalf("mkcall %v needs %v args got %v", fn, n, len(va))
	}

	call := typecheck.Call(base.Pos, fn, va, false).(*ir.CallExpr)
	call.SetType(t)
	return call
}

func convIface(v ir.Node, iface *types.Type) ir.Node {
	if !iface.IsInterface() || iface.IsEmptyInterface() {
		base.Fatalf("dst is not an interface")
	}

	conv, ok := typecheck.Conv(v, iface).(*ir.ConvExpr)
	if !ok {
		return v
	}

	typ := v.Type()
	pos := v.Pos()
	if !typ.IsInterface() {
		conv.TypeWord = reflectdata.ITabAddrAt(pos, typ, iface)
		conv.SrcRType = reflectdata.TypePtrAt(pos, typ)
	} else {
		conv.TypeWord = reflectdata.TypePtrAt(pos, iface)
	}
	return conv
}

type subst struct {
	curfn *ir.Func
	arg   ir.Node     // current argument
	typ   *types.Type // current argument type

	bufs []*ir.Name // fmt_itoa, fmt_uitoa temporary buffers
}

func (s *subst) fmtValue(verb rune, arg ir.Node) ir.Node {
	ir.SetPos(arg)

	if arg.Op() == ir.OCONVIFACE { // remove conversion to empty interface.
		arg = arg.(*ir.ConvExpr).X
	}
	s.arg = arg
	s.typ = arg.Type()
	if n := s.handleMethods(verb); n != nil {
		return n
	}

	switch typ := s.typ; {
	case typ.IsInteger():
		return s.fmtInteger(verb)
	case typ.IsString():
		return s.fmtString(arg, verb)
	case typ.IsSlice() && typ.Elem().Kind() == types.TUINT8:
		return s.fmtBytes(verb)
	case typ.IsBoolean():
		return s.fmtBool(verb)
	default:
		base.Fatalf("unexpected argument type: %v", typ.String())
		panic("unreachable")
	}
}

func (s *subst) handleMethods(verb rune) ir.Node {
	if ImplementsFormatterType(s.typ) {
		base.Fatalf("cannot handle type implements fmt.Formatter")
	}
	typ := s.arg.Type()
	if s.typ.LinkString() == "reflect.Value" {
		base.Fatalf("cannot handle argument type: reflect.Value")
	}
	switch verb {
	case 'v', 's':
		var call ir.Node
		ifaceCall := func(fname string, iface *types.Type) ir.Node {
			fn := typecheck.LookupRuntime(fname, iface)
			conv := convIface(s.arg, iface)
			return mkcall1(fn, types.Types[types.TSTRING], conv)
		}
		if typecheck.Implements(typ, types.ErrorType) {
			call = ifaceCall("fmt_error", types.ErrorType)
			return s.fmtString(call, verb)
		} else if typecheck.Implements(typ, stringer) {
			switch typ.LinkString() {
			case "time.Duration", "time.Month", "time.Weekday":
				// Special case for std package time.
				// These methods won't panic, we can remove the wrapper function.
				x := typecheck.Callee(ir.NewSelectorExpr(s.arg.Pos(), ir.OXDOT, s.arg, types.LocalPkg.Lookup("String")))
				call = typecheck.Call(s.arg.Pos(), x, nil, false)
			default:
				call = ifaceCall("fmt_stringer", stringer)
			}
			return s.fmtString(call, verb)
		}
	case 'x':
		if typecheck.Implements(typ, types.ErrorType) || typecheck.Implements(typ, stringer) {
			base.Fatalf("%%x: unexpected argument type: %v", typ)
		}
	}
	return nil
}

func (s *subst) tempBuffer() ir.Node {
	// A string of length 20 is sufficient to represent the maximum value of int64 and uint64.
	// We use 24 here because 24 is a multiple of 8 (register size for 64-bit platform).
	buf := typecheck.TempAt(s.arg.Pos(), s.curfn, types.NewArray(types.ByteType, 24))
	s.bufs = append(s.bufs, buf)
	return buf
}

func (s *subst) fmtInteger(verb rune) ir.Node {
	var fn string
	var arg ir.Node

	switch verb {
	case 'd', 'v':
		switch {
		case s.typ.IsSigned():
			fn = "fmt_itoa"
			arg = typecheck.Conv(s.arg, types.Types[types.TINT64])
		case s.typ.IsUnsigned():
			fn = "fmt_uitoa"
			arg = typecheck.Conv(s.arg, types.Types[types.TUINT64])
		default:
			base.Fatalf("not integer type")
		}
		return mkcall(fn, types.Types[types.TSTRING], typecheck.NodAddr(s.tempBuffer()), arg)
	case 'x':
		switch {
		case s.typ.IsSigned():
			fn = "fmt_hex"
			arg = typecheck.Conv(s.arg, types.Types[types.TINT64])
		case s.typ.IsUnsigned():
			fn = "fmt_uhex"
			arg = typecheck.Conv(s.arg, types.Types[types.TUINT64])
		default:
			base.Fatalf("not integer type")
		}
		return mkcall(fn, types.Types[types.TSTRING], typecheck.NodAddr(s.tempBuffer()), arg)
	default:
		panic("bad verb: " + string(verb))
	}
}

func (s *subst) fmtString(arg ir.Node, verb rune) ir.Node {
	switch verb {
	case 's', 'v':
		return arg
	default:
		panic("bad verb: " + string(verb))
	}
}

func (s *subst) fmtBytes(verb rune) ir.Node {
	switch verb {
	case 's':
		return typecheck.Conv(s.arg, types.Types[types.TSTRING])
	default:
		panic("bad verb: " + string(verb))
	}
}

func (s *subst) fmtBool(verb rune) ir.Node {
	switch verb {
	case 't', 'v':
		arg := typecheck.Conv(s.arg, types.Types[types.TBOOL])
		return mkcall("fmt_bool", types.Types[types.TSTRING], arg)
	default:
		panic("bad verb: " + string(verb))
	}
}
