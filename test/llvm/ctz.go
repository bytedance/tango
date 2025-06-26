// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"math"
	"math/bits"
)

func mainHelper(f1 float64, f2 float64) uint64 {
	return uint64(math.Pow(f1, f2))
}

func main() {
	if bits.TrailingZeros64(mainHelper(0, 1)) != 64 {
		panic("ctz has failed")
	}
	if a := bits.TrailingZeros64(mainHelper(2, 6)); a != 6 {
		fmt.Println(a)
		panic("ctz has failed")
	}
}
