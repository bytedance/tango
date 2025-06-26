// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"math"
)

func testFloat(f float64) bool {
	if f != 1 {
		return false
	}
	return true
}

func main() {
	// any number compared to NaN will get false
	if a := math.NaN() + math.NaN(); testFloat(a) {
		fmt.Println(math.IsNaN(a))
		panic("add failed")
	}
	if a := math.Inf(1) + math.Inf(1); a != math.Inf(1) {
		fmt.Println(a)
		panic("add failed")
	}
	if a := math.Inf(-1) + math.Inf(-1); a != math.Inf(-1) {
		fmt.Println(a)
		panic("add failed")
	}
}
