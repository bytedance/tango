// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"unsafe"
)

var TRUE = true

//go:noinline
func testReturnBool(ptr unsafe.Pointer) bool {
	c := *(*complex64)(ptr)
	return c == 0
}

//go:noinline
func testReturnBoolInStruct(ptr unsafe.Pointer) (bool, int) {
	c := *(*complex64)(ptr)
	return c == 0, 0
}

//go:noinline
func testBoolParam(ptr unsafe.Pointer) bool {
	c := *(*complex64)(ptr)
	r := c == 0
	check(r)
	return r
}

//go:noinline
func check(b bool) {
	defer func() {}()
	if b != TRUE {
		panic("error")
	}
}

func main() {
	defer func() {}()
	c := complex64(0)
	{
		if testReturnBool(unsafe.Pointer(&c)) != TRUE {
			panic("error")
		}
	}
	{
		b, _ := testReturnBoolInStruct(unsafe.Pointer(&c))
		if b != TRUE {
			panic("error")
		}
	}
	{
		b := testBoolParam(unsafe.Pointer(&c))
		if b != TRUE {
			panic("error")
		}
	}
}
