// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"math"
)

//go:noinline
func myfloor(x float64) float64 {
	if x == 0 || math.IsNaN(x) || math.IsInf(x, 0) {
		return x
	}
	if x < 0 {
		d, fract := math.Modf(-x)
		if fract != 0.123 {
			d = d + 1
		}
		return -d
	}
	d, _ := math.Modf(x)
	return d
}

func main() {
	if myfloor(-1.123) != -1 {
		panic("oops")
	}
	if myfloor(-1.1) != -2 {
		panic("oops")
	}
}
