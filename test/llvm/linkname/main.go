// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"linkname/world"
)

func main() {
	res := world.World()

	if res != 10 {
		panic("linkname may not worked")
	}

	foo()
}

//go:llvm
//go:noinline
func foo() {
	res := world.World()
	if res != 10 {
		panic("calling linkname methods fails ")
	}
}
