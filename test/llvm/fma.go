// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"math"
)

//go:noinline
func testFMA(a, b, c float64) float64 {
	return math.FMA(a, b, c)
}

func main() {
	if testFMA(0, 1, 2) != 2.0 {
		panic("Oops")
	}
}
