// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

// Maps aren't actually copied on assignment.
//
//go:noinline
func testAlias() {
	m := make(map[int]int, 0)
	m[0] = 5
	n := m
	n[0] = 6
	if m[0] != 6 {
		panic("alias didn't work")
	}
}

//go:noinline
func mapassign() {
	a := make(map[int32]int, 100)

	for i := 0; i < 100; i++ {
		a[int32(i)] = i
	}

	if len(a) != 100 {
		panic("length wrong")
	}

	for k, v := range a {
		if k != int32(v) {
			panic("value not correct")
		}
	}
}

func main() {
	testAlias()
	mapassign()
}
