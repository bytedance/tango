// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"math/bits"
)

func main() {
	if a := bits.RotateLeft64(64, 4); a != 1024 {
		fmt.Println(a)
		panic("rotateleft has failed")
	}
}
