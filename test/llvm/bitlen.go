// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"math/bits"
)

func main() {
	if bits.Len64(0) != 0 {
		panic("bitlen has failed")
	}
	if bits.Len64(64) != 7 {
		panic("bitlen has failed")
	}
}
