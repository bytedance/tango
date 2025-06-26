// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

//go:noinline
func test(i int) int {
	if i == 0 {
		return 0
	}
	ret := 1
	for i > 10 {
		i = i / 10
		ret++
	}
	return ret
}

//go:noinline
func testMulPanic(i int) int {
	if i == 0 {
		return 0
	}
	if i < 0 {
		panic("negative")
	}
	ret := 1
	for i > 10 {
		i = i / 10
		ret++
	}
	if ret > 100 {
		panic("too large")
	}
	return ret
}

func main() {
	ret := test(1024)
	if ret != 3 {
		panic("Oops")
	}
}
