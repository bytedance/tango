// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"unsafe"
)

//go:noinline
func def(p *int) int {
	return *p
}

//go:linkname def2  main.def
func def2(p unsafe.Pointer)

//go:linkname def3  main.def
func def3(p unsafe.Pointer) int
func main() {
	var i int
	def(&i)
	def2(unsafe.Pointer(&i))
	def3(unsafe.Pointer(&i))
}
