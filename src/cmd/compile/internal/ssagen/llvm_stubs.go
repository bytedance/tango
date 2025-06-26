// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !goexperiment.tangollvm || !linux || !amd64 || !cgo || compiler_bootstrap

package ssagen

import (
	"cmd/compile/internal/ssa"
	"fmt"
	"os"
	"strconv"
)

func InitLLVM() {
	var verbose int
	if i, err := strconv.ParseInt(os.Getenv("GOLLVMVERBOSE"), 10, 32); err == nil {
		verbose = int(i)
	}
	if verbose != 0 {
		fmt.Printf("TangoLLVM only support Linux/amd64 now")
	}
}

func newLLVMBuilder(s *state) ssa.LLVMBuilder {
	return nil
}

func CompleteLLVM() {}
