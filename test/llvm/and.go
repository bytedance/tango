// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"math"
)

func main() {
	// we use function call to avoid constant propagation
	t := int(math.Pow(2.0, 4.0))
	a1 := int8(t)
	b1 := int8(t - 1)
	if a1&b1 != 0 {
		panic("and failed")
	}
	a2 := int16(t)
	b2 := int16(t - 1)
	if a2&b2 != 0 {
		panic("and failed")
	}
}
