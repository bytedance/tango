// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

type Option struct{}

//go:inline
func test(x chan Option, o Option, j Option) (a, b, c, d, e, f, g, h, i int, r Option) {
	x <- o
	x <- j
	r = o
	return
}

type Bulk struct {
	O Option
	_ [10]int
}

//go:inline
func test2(x chan Option, o Option, B Bulk, k int64, j Option) (a, b, c, d, e, f, g, h, i int, r Option) {
	x <- o
	x <- j
	x <- B.O
	r = o
	return
}

// dummy code that simply makes compiler work
func main() {
	var x = make(chan Option)
	var o, j Option
	var B Bulk
	go test(x, o, j)
	go test2(x, o, B, 0, j)
}
