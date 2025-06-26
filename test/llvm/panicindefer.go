// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

func doPanic() {
	panic("do panic!")
}

func doRecovery() {
	if r := recover(); r != nil {
		println("do recovery")
	}
}

//go:noinline
func test(i int) int {
	defer doRecovery()
	defer doPanic()
	return i + 123
}

func main() {
	if test(123) != 246 {
		panic("Oops")
	}
}
