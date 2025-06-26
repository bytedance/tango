// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package hello

import (
	"fmt"
	_ "unsafe"
)

//go:llvm
//go:noinline
//go:linkname hello linkname/world.World
func hello() int {
	a := 10
	fmt.Println("hello world")
	return a
}
