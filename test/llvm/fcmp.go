// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"math"
)

//go:noinline
func FEq(x, y float64) bool {
	return x == y
}

//go:noinline
func FNeq(x, y float64) bool {
	return x != y
}

//go:noinline
func FLt(x, y float64) bool {
	return x < y
}

//go:noinline
func FLe(x, y float64) bool {
	return x <= y
}

type testData struct {
	x  float64
	y  float64
	eq bool
	ne bool
	lt bool
	le bool
}

func main() {
	nan := math.NaN()
	tests := []testData{
		{0.0, 0.1, false, true, true, true},
		{0.1, 0.1, true, false, false, true},
		{0.1, 0.0, false, true, false, false},
		{nan, 0.1, false, true, false, false},
		{0.1, nan, false, true, false, false},
		{nan, nan, false, true, false, false},
	}
	for _, d := range tests {
		println(d.x, d.y)
		x := d.x
		y := d.y
		eq, ne, lt, le := FEq(x, y), FNeq(x, y), FLt(x, y), FLe(x, y)
		if d.eq != eq {
			panic("oops: bad eq")
		}
		if d.ne != ne {
			panic("oops: bad ne")
		}
		if d.lt != lt {
			panic("oops: bad lt")
		}
		if d.le != le {
			panic("oops: bad le")
		}
	}
}
