// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"math"
)

func main() {
	if math.RoundToEven(0) != 0 {
		panic("roundeven has panic")
	}
	if math.RoundToEven(math.Inf(1)) != math.Inf(1) {
		panic("roundeven has panic")
	}
	if math.RoundToEven(math.Inf(-1)) != math.Inf(-1) {
		panic("roundeven has panic")
	}
	if math.RoundToEven(math.NaN()) == math.NaN() {
		panic("roundeven has panic")
	}
	if a := math.RoundToEven(-2.5); a != -2 {
		fmt.Println(a)
		panic("roundeven has panic")
	}
}
