// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.tangollvm && linux && amd64 && cgo && !compiler_bootstrap

package ssagen

import (
	"cmd/internal/llvm"
	"testing"
)

func TestLLVMPreallocatedContract(t *testing.T) {
	if err := llvm.InitTangoLLVMEnv("libLLVM.so"); err != nil {
		t.Fatal(err)
	}
	ctx := llvm.NewContext()
	defer ctx.Dispose()
	mod := ctx.NewModule("preallocated-contract")
	defer mod.Dispose()
	fn := llvm.AddFunction(mod, "readArgument", llvm.FunctionType(ctx.Int64Type(), nil, false))
	bb := ctx.AddBasicBlock(fn, "entry")
	irBuilder := ctx.NewBuilder()
	defer irBuilder.Dispose()
	irBuilder.SetInsertPointAtEnd(bb)
	b := builder{
		compilerContext: &compilerContext{goPreAllocatedID: llvm.LookupIntrinsicID("llvm.go.preallocated")},
		Builder:         irBuilder,
	}
	addr := b.createGoPreAllocated(ctx.Int64Type(),
		llvm.ConstInt(ctx.Int64Type(), 0, false),
		llvm.ConstInt(ctx.Int64Type(), 8, false),
		llvm.ConstInt(ctx.Int64Type(), 0, false),
		llvm.ConstInt(ctx.Int1Type(), 0, false))
	irBuilder.CreateRet(irBuilder.CreateLoad(ctx.Int64Type(), addr, "argument"))
	if err := llvm.VerifyModule(mod, llvm.ReturnStatusAction); err != nil {
		t.Fatalf("preallocated IR does not match the paired LLVM API: %v\n%s", err, mod.String())
	}
}
