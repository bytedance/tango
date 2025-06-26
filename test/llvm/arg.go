// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import "fmt"

func testArg(a1, a2, a3, a4, a5, a6, a7, a8, a9, a10 int) bool {
	if a10 != 0 {
		fmt.Println(a10)
		return true
	}
	return false
}

func main() {
	if testArg(0, 0, 0, 0, 0, 0, 0, 0, 0, 0) {
		panic("oparg has failed")
	}
}
