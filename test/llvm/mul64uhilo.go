// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"math/bits"
)

func main() {
	if hi, lo := bits.Mul64(0xffff_ffff_ffff_fff, 0xffff_ffff_ffff_fff); hi != 0xff_ffff_ffff_ffff || lo != 0xe000_0000_0000_0001 {
		fmt.Println(hi)
		fmt.Println(lo)
		panic("mul64uhilo has failed")
	}
}
