// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

//go:noinline
func test(bitSize int) uint64 {
	maxVal := uint64(1)<<uint(bitSize) - 1
	return maxVal
}

//go:noinline
func test2(bitSize int) int64 {
	maxVal := int64(1)<<int(bitSize) - 1
	return maxVal
}

func main() {
	bitSize := 64
	if test(bitSize) != 18446744073709551615 {
		panic("oops")
	}
	if test2(bitSize) != -1 {
		panic("oops")
	}
}
