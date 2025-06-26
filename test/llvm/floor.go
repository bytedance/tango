// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import "math"

func main() {
	if math.Floor(math.NaN()) == math.NaN() {
		panic("floor has failed")
	}
	if math.Floor(math.Inf(1)) != math.Inf(1) {
		panic("floor has failed")
	}
	if math.Floor(math.Inf(-1)) != math.Inf(-1) {
		panic("floor has failed")
	}
	if math.Floor(3.67) != 3.0 {
		panic("floor has failed")
	}
}
