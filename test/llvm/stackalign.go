// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
)

func main() {
	benchLLVMAtomicInt32()
	fmt.Println("llvm atomic int32 with times cost ")
}

//go:noinline
func benchLLVMAtomicInt32() {

	t2 := 0
	fmt.Println("llvm atomic int32 with times cost ", t2)
}
