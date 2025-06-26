// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

func mainHelper() [8]int {
	var ret [8]int
	for i := 0; i < 8; i++ {
		ret[i] = i
	}
	return ret
}

func main() {
	if mainHelper()[5] != 5 {
		panic("selectnaddr has failed")
	}
}
