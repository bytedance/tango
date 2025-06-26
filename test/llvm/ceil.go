// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"math"
)

func main() {
	if a := math.Ceil(math.NaN()); a == math.NaN() {
		fmt.Println(a)
		panic("ceil has failed")
	}
	if a := math.Ceil(math.Inf(1)); a != math.Inf(1) {
		fmt.Println(a)
		panic("ceil has failed")
	}
	if a := math.Ceil(math.Inf(-1)); a != math.Inf(-1) {
		fmt.Println(a)
		panic("ceil has failed")
	}
	if a := math.Ceil(3.67); a != 4.0 {
		fmt.Println(a)
		panic("ceil has failed")
	}
}
