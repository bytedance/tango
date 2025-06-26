// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

//go:noinline
func getTestNumber() uint64 {
	return 64
}

func main() {
	if ^getTestNumber() != 0xFFFF_FFFF_FFFF_FFBF {
		panic("com has failed")
	}
}
