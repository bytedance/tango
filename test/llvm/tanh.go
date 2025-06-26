// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"math"
)

//go:noinline
func test(x float64) float64 {
	return math.Tanh(x)
}

func main() {
	println(test(100.01))
	println(test(0.7))
	println(test(0.6))
	println(test(2.01))
}
