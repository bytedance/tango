// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"math/bits"
)

func main() {
	if a := bits.Reverse64(64); a != 0x200_0000_0000_0000 {
		fmt.Println(a)
		panic("bitreverse has failed")
	}
}
