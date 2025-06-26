// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

func test(i int) int {
	return i + 1
}

//go:noinline
func testFunc(i int, f func(int) int) int {
	return f(i)
}

func main() {
	var f func(int) int
	f = test
	var j int = 10
	fp := func(i int) int {
		j++
		return f(i) + j
	}
	r := testFunc(1, fp)
	if r != 13 || j != 11 {
		panic("Oops")
	}
}
