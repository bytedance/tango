// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

//go:noinline
func test(s string, b []byte) bool {
	return s == "" && b == nil
}

func main() {
	var s string
	var b []byte
	if !test(s, b) {
		panic("Oops")
	}
}
