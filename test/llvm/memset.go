// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

type LargeStruct struct {
	A [128]int
}

//go:noinline
func test() (a LargeStruct) {
	a.A[123] = 321
	return
}

func main() {
	a := test().A
	if a[123] != 321 {
		panic("Oops")
	}

	for i := 0; i < len(a); i++ {
		if i == 123 {
			continue
		}
		if a[i] != 0 {
			panic("Oops")
		}
	}
}
