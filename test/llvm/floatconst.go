// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

//go:noinline
func testF64() float64 {
	return 1.2345
}

//go:noinline
func testF32() float32 {
	return 5.4321
}

func main() {
	if testF64() != 1.2345 {
		panic("Oops")
	}
	if testF32() != 5.4321 {
		panic("Oops")
	}
}
