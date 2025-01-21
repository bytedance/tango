// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fmtrewrite

import (
	"cmd/compile/internal/base"
	"cmd/compile/internal/ir"
	"cmd/compile/internal/typecheck"
	"cmd/compile/internal/types"
	"cmd/internal/src"
	"strconv"
	"unicode/utf8"
)

var stringer *types.Type

func Init() {
	if base.Flag.Cfg.BeastOpt.FmtRewrite == 0 {
		return
	}
	sig := types.NewSignature(types.FakeRecv(), nil, []*types.Field{
		types.NewField(src.NoXPos, nil, types.Types[types.TSTRING]),
	})
	method := types.NewField(src.NoXPos, types.LocalPkg.Lookup("String"), sig)
	stringer = types.NewInterface([]*types.Field{method})
}

func parse(format string) ([]token, bool) {
	// special case for empty format.
	if format == "" {
		empty := []token{literal("")}
		return empty, true
	}

	var start, i int
	var field []token
	for i < len(format) {
		v, size1 := utf8.DecodeRuneInString(format[i:])
		i += size1
		if v != '%' {
			continue
		}

		if i >= len(format) {
			return nil, false
		}

		v, size2 := utf8.DecodeRuneInString(format[i:])
		i += size2
		switch v {
		default:
			return nil, false
		case '%', 's', 'd', 'v', 't', 'x': // TODO: support more verbs.
			lstart := i - size1 - size2
			if lstart > start {
				field = append(field, literal(format[start:lstart]))
			}
			if v == '%' { // special case for "%%"
				field = append(field, literal("%"))
			} else {
				field = append(field, verb(v))
			}
			start = i
		}
	}
	if i > start {
		field = append(field, literal(format[start:i]))
	}
	return field, true
}

type token struct {
	Kind   int
	Verb   rune
	String string
}

const (
	Literal = iota
	Verb
)

func literal(s string) token {
	return token{
		Kind:   Literal,
		String: s,
	}
}

func verb(v rune) token {
	return token{
		Kind: Verb,
		Verb: v,
	}
}

func ImplementsFormatterType(typ *types.Type) bool {
	// fmt.Formatter is hard to construct. So just check the method name.
	// TODO: also check signature
	var methods []*types.Field
	if typ.IsInterface() {
		methods = typ.AllMethods()
	} else {
		typ = types.ReceiverBaseType(typ)
		if typ == nil {
			// typ cannot have methods.
			return false
		}
		typecheck.CalcMethods(typ)
		methods = typ.AllMethods()
	}
	for _, method := range methods {
		if method.Sym.Name == "Format" {
			return true
		}
	}
	return false
}

type checker struct {
	format []token
	args   []ir.Node
}

func (c *checker) check() bool {
	argIndex := 0
	for _, token := range c.format {
		switch token.Kind {
		case Literal:
		case Verb:
			if argIndex >= len(c.args) {
				return false
			}
			if !c.match(c.args[argIndex], token.Verb) {
				return false
			}
			argIndex++
		default:
			panic("unexpected kind: " + strconv.FormatInt(int64(token.Kind), 10))
		}
	}
	if argIndex != len(c.args) {
		return false
	}
	return true
}

// match report whether arg can use verb v.
func (c *checker) match(arg ir.Node, v rune) bool {
	if arg.Op() == ir.OCONVIFACE { // remove conversion to interface{}.
		arg = arg.(*ir.ConvExpr).X
	}
	typ := arg.Type()
	if ImplementsFormatterType(typ) {
		return false
	}
	if typ.LinkString() == "reflect.Value" {
		// reflect.Value print the underlying value, we cannot support it.
		return false
	}
	if typecheck.Implements(typ, stringer) || typecheck.Implements(typ, types.ErrorType) {
		switch v {
		case 's', 'v':
			return true
		case 'x':
			// not implemented
			return false
		}
	}
	switch v {
	case 'd':
		return typ.IsInteger()
	case 's':
		if typ.IsSlice() && typ.Elem().Kind() == types.TUINT8 {
			return true
		}
		return typ.IsString()
	case 't':
		return typ.IsBoolean()
	case 'v':
		return typ.IsInteger() || typ.IsBoolean() || typ.IsString()
	case 'x':
		return typ.IsInteger()
	}
	return false
}

type assignAnalysis struct {
	fn     *ir.Func
	oracle *ir.ReassignOracle
	uses   map[*ir.Name]int
}

func (a *assignAnalysis) StaticValue(n ir.Node) (ir.Node, []*ir.Name) {
	var chain []*ir.Name
	for {
		switch n1 := n.(type) {
		case *ir.ConvExpr:
			if n1.Op() == ir.OCONVNOP {
				n = n1.X
				continue
			}
		case *ir.InlinedCallExpr:
			if n1.Op() == ir.OINLCALL {
				n = n1.SingleResult()
				continue
			}
		case *ir.ParenExpr:
			n = n1.X
			continue
		}

		if n.Op() == ir.ONAME {
			nn := n.(*ir.Name).Canonical()
			if nn.Class == ir.PAUTO {
				chain = append(chain, nn)
			}
		}

		if a.oracle == nil {
			a.oracle = new(ir.ReassignOracle)
			a.oracle.Init(a.fn)
		}
		n1 := a.oracle.StaticValue1(n)
		if n1 == nil {
			return n, chain
		}
		n = n1
	}
}

func (a *assignAnalysis) initUses() {
	if a.uses != nil {
		return
	}
	uses := make(map[*ir.Name]int)
	var walk func(n ir.Node)
	walkAll := func(ns ir.Nodes) {
		for _, c := range ns {
			walk(c)
		}
	}
	walk = func(n ir.Node) {
		if n == nil {
			return
		}
		switch x := n.(type) {
		case *ir.Decl:
			// skip declaration of *ir.Name.
			return
		case *ir.Name:
			uses[x.Canonical()]++
			return
		case *ir.AssignStmt:
			walkAll(x.Init())
			// Skip direct assign to *ir.Name.
			if _, ok := x.X.(*ir.Name); !ok {
				walk(x.X)
			}
			walk(x.Y)
			return
		case *ir.AssignListStmt:
			walkAll(x.Init())
			for _, lhs := range x.Lhs {
				// Skip direct assign to *ir.Name.
				if _, ok := lhs.(*ir.Name); ok {
					continue
				}
				walk(lhs)
			}
			for _, rhs := range x.Rhs {
				walk(rhs)
			}
			return
		case *ir.ClosureExpr:
			walkAll(x.Func.Body)
			return
		}
		ir.DoChildren(n, func(c ir.Node) bool {
			walk(c)
			return false
		})
	}
	walkAll(a.fn.Body)
	a.uses = uses
}

func (a *assignAnalysis) checkUse(chain []*ir.Name) bool {
	a.initUses()
	for _, n := range chain {
		if a.uses[n] > 1 {
			return false
		}
	}
	return true
}
