// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"math/bits"
)

//go:noinline
func test(x, y uint64) (uint64, uint64) {
	return bits.Mul64(x, y)
}

func main() {
	x, y := test(1, 3)
	if x != 0 || y != 3 {
		panic("oops")
	}
}
