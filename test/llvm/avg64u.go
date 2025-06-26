// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
)

//go:noinline
func mainHelper() uint64 {
	return 19
}

func main() {
	a := mainHelper() / 19 * 19
	if a != 19 {
		fmt.Println(a)
		panic("avg64u has failed")
	}
}
