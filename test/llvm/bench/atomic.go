// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"sync/atomic"
	"time"
)

const iter = 10000
const benchSize = 1 << 9

func main() {
	BenchmarkLLVMAtomicInt32()
	BenchmarkAtomicInt32()
}

//go:noinline
//go:llvm
func BenchmarkLLVMAtomicInt32() {

	t1 := time.Now()
	var a int32

	for n := 0; n < iter; n++ {
		a = 0
		for i := int32(0); i < benchSize; i++ {
			atomic.AddInt32(&a, 1)

			if atomic.LoadInt32(&a) != i+1 {
				panic("atomic value is not set correctly")
			}
		}
	}

	t2 := time.Since(t1)
	fmt.Println("LLVM atomic:", t2)
}

//go:noinline
func BenchmarkAtomicInt32() {
	t1 := time.Now()
	var a int32

	for n := 0; n < iter; n++ {
		a = 0
		for i := int32(0); i < benchSize; i++ {
			atomic.AddInt32(&a, 1)

			if atomic.LoadInt32(&a) != i+1 {
				panic("atomic value is not set correctly")
			}
		}
	}
	t2 := time.Since(t1)
	fmt.Println("gc atomic:", t2)
}
