// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"math"
)

func main() {
	if math.Trunc(0) != 0 {
		panic("trunc has failed")
	}
	if math.Trunc(math.Inf(1)) != math.Inf(1) {
		panic("trunc has failed")
	}
	if math.Trunc(math.Inf(-1)) != math.Inf(-1) {
		panic("trunc has failed")
	}
	if math.Trunc(3.627) != 3.0 {
		panic("trunc has failed")
	}
	if math.Trunc(-3.627) != -3.0 {
		panic("trunc has failed")
	}
}
