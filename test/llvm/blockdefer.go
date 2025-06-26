// Copyright 2019 The Go Authors. All rights reserved.
// Copyright (c) 2025 ByteDance Ltd. and/or its affiliates.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// This file has been modified by ByteDance Ltd. and/or its affiliates on 2025-06-26.

package main

//go:noinline
func testDefer1() (c int) {
	defer func() { c = 0 }()
	defer func() { c = 3 }()
	c = 2
	return
}

var list []int

//go:noinline
func testConditionalDefers(n int) int {
	doappend := func(i int) {
		list = append(list, i)
	}

	defer doappend(1)
	if n > 5 {
		defer doappend(2)
		if n > 8 {
			defer doappend(3)
		} else {
			defer doappend(4)
		}
	}
	return 0
}

func main() {
	if testDefer1() != 0 {
		panic("blockdefer is not correct")
	}

	if testConditionalDefers(8) != 0 {
		panic("not execute correct")
	}

	if list[0] != 4 || list[1] != 2 || list[2] != 1 {
		panic("defer is not correct")
	}
}
