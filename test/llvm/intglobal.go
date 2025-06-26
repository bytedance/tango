// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

var i int = 1

//go:noinline
func geti() int {
	i++
	return i
}

//go:noinline
func cmpi() int {
	if i == 1 {
		return 0
	}
	return i
}

func main() {
	if geti() != 2 {
		panic("Oops")
	}
	if i != 2 {
		panic("Oops")
	}
	if cmpi() != 2 {
		panic("Oops")
	}
}
