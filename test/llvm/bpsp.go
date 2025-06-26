// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

type LargeStruct struct {
	A int
	B byte
	F float64
	P [10]int
}

type FitStruct struct {
	P1 int
	P2 int
	P3 int
	P4 int
	P5 int
	P6 int
	P7 int
	P8 int
	P9 int
}

//go:noinline
func test(s LargeStruct, f FitStruct, b byte) int {
	if s.B == 1 || s.F == 0.1 || b == 123 {
		return 2
	}
	return s.A
}

func main() {
	var s LargeStruct
	var f FitStruct
	s.A = 100
	if test(s, f, 1) != s.A {
		panic("Oops")
	}
	if test(s, f, 123) != 2 {
		panic("Oops")
	}
}
