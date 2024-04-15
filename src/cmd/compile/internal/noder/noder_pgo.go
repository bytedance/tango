// Copyright 2024 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package noder

import (
	"cmd/compile/internal/base"
	"cmd/compile/internal/syntax"
)

func optStackSizeEnabled() bool {
	return base.Ctxt.Pkgpath == "runtime" && base.Flag.PgoProfile != "" && base.Debug.PGOStackSize > 0
}

// rewriteStackMin rewrites the value of stackMin constant by editing the AST.
//
// The source code to rewrite is:
// `package runtime
//
// const (
//
//	stackMin = 8192
//	optmode   = "beast"
//
// )`
//
// And AST structure corresponding to source code (some irrelevant parts are omitted) is:
//
//	*syntax.File {
//	.  PkgName: runtime @ dump_source.go:1:9
//	.  DeclList: []syntax.Decl (2 entries) {
//	.  .  0: *syntax.ConstDecl {
//	.  .  .  Group: *syntax.Group {}
//	.  .  .  NameList: []*syntax.Name (1 entries) {
//	.  .  .  .  0: stackMin @ dump_source.go:4:2
//	.  .  .  }
//	.  .  .  Values: *syntax.BasicLit {
//	.  .  .  .  Value: "8192"
//	.  .  .  }
//	.  .  }
//	.  }
//	.  EOF: syntax.Pos {}
//	}
func rewriteStackMin(noders []*noder, newValue string) bool {
	const stackMin = "stackMin"
	for _, n := range noders {
		for _, decl := range n.file.DeclList {
			constDecl, ok := decl.(*syntax.ConstDecl)
			if !ok {
				continue
			}

			foundStackMin := false
			for _, name := range constDecl.NameList {
				if name.Value == stackMin {
					foundStackMin = true
					break
				}
			}

			if !foundStackMin {
				continue
			}

			lit, ok := constDecl.Values.(*syntax.BasicLit)
			if !ok {
				return false
			}

			lit.Value = newValue
			return true
		}
	}

	return false
}
