// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"math/bits"
)

func main() {
	if quo, rem := bits.Div64(0xffff_ffff_ffff_fff, 0xffff_ffff_ffff_ffff, 0xffff_ffff_ffff_ffff); quo != 0x1000000000000000 || rem != 0xffff_ffff_ffff_fff {
		fmt.Println("quo is ", quo)
		fmt.Println("rem is ", rem)
		panic("div64 has failed")
	}
}
