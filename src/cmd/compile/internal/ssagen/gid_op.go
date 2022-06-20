// Copyright 2022 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ssagen

import (
	"cmd/compile/internal/base"
	"cmd/compile/internal/ir"
	"cmd/compile/internal/ssa"
	"cmd/compile/internal/types"
	"cmd/internal/sys"
	"fmt"
	"internal/buildcfg"
)

func initGIdOp(addF func(pkg, fn string, b intrinsicBuilder, archFamilies ...sys.ArchFamily)) {
	// pid support
	if beastIntrinsic() {
		addF("github.com/choleraehyq/pid", "Get",
			func(s *state, n *ir.CallExpr, args []*ssa.Value) *ssa.Value {
				if base.Flag.Cfg.BeastOpt.Verbose != 0 {
					fmt.Println("[Beast] github.com/choleraehyq/pid.Get() is replaced with runtime.GetGId()")
				}
				return s.rtcall(ir.Syms.GetGId, true, []*types.Type{types.Types[types.TINT64]}, args...)[0]
			},
			sys.AMD64, sys.ARM64)
		addF("github.com/choleraehyq/pid", "GetPid",
			func(s *state, n *ir.CallExpr, args []*ssa.Value) *ssa.Value {
				if base.Flag.Cfg.BeastOpt.Verbose != 0 {
					fmt.Println("[Beast] github.com/choleraehyq/pid.GetPid() is replaced with runtime.GetPId()")
				}
				return s.rtcall(ir.Syms.GetPId, true, []*types.Type{types.Types[types.TINT]}, args...)[0]
			},
			sys.AMD64, sys.ARM64)
	}
}

func beastIntrinsic() bool {
	return base.Flag.Cfg.BeastOpt.Intrinsic != 0 &&
		(buildcfg.GOARCH == "amd64" || buildcfg.GOARCH == "arm64")
}
