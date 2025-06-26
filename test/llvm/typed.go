// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

//go:noinline
func testllvm(e interface{}) {
	_ = e.(int)
}

func main() {
	testllvm(1)
}
