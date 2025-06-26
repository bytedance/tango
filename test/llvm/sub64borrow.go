// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"math/bits"
)

func main() {
	if diff, borrowOut := bits.Sub64(0xfff, 0xff, 1); diff != 0xeff || borrowOut != 0 {
		fmt.Println(diff)
		fmt.Println(borrowOut)
		panic("sub64borrow has failed")
	}
}
