// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

//go:noinline
func test(a *int) int {
	return *a
}

func main() {
	var recovered = false
	defer func() {
		if r := recover(); r != nil {
			recovered = true
			return
		}
	}()
	test(nil)
	if !recovered {
		panic("not recovered")
	}
}
