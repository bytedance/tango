// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

//go:noescape
func Add(x, y int64) int64

//go:llvm
func main() {
	if got := Add(100, 100); got != 200 {
		panic("ABI0 addition returned an incorrect result")
	}
}
