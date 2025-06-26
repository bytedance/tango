// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import "math"

func mainHelper() float32 {
	return 4.0
}

func main() {
	var c float32 = 2.0
	if float32(math.Sqrt(float64(mainHelper()))) != c {
		panic("sqrt32 has failed")
	}
	if math.Sqrt(math.Inf(1)) != math.Inf(1) {
		panic("sqrt64 has failed")
	}
	if math.Sqrt(0) != 0 {
		panic("sqrt64 has failed")
	}
	if math.Sqrt(-1) == math.NaN() {
		panic("sqrt64 has failed")
	}
	if math.Sqrt(math.NaN()) == math.NaN() {
		panic("sqrt64 has failed")
	}
}
